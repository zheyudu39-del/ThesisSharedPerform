"use client";

import Link from "next/link";
import { useCallback, useEffect, useRef, useState } from "react";
import { RequireAuth } from "@/components/RequireAuth";
import { PostCard } from "@/components/PostCard";
import { AuthError } from "@/lib/auth";
import { getBookmarks, type PostDTO } from "@/lib/posts";

/**
 * 收藏列表工作区（客户端）。
 *
 * - 整页要求登录：收藏是个人数据，未登录直接跳到登录页并回跳；
 * - 按收藏时间倒序（服务端排序），游标翻页；
 * - 取消收藏后把卡片从本地列表移除：留在页面上会让「已收藏」按钮立刻变灰，
 *   但用户已经用这个动作表达了「不想再看到它」，即时移除更符合预期。
 */
export function BookmarksWorkspace() {
  return (
    <RequireAuth>
      <BookmarksList />
    </RequireAuth>
  );
}

function BookmarksList() {
  const [posts, setPosts] = useState<PostDTO[]>([]);
  const [nextCursor, setNextCursor] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const [loadingMore, setLoadingMore] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const mounted = useRef(true);
  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
    };
  }, []);

  const load = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const page = await getBookmarks(null, 20);
      if (!mounted.current) return;
      setPosts(page.posts);
      setNextCursor(page.nextCursor);
    } catch (err) {
      if (!mounted.current) return;
      setError(err instanceof AuthError ? err.message : "收藏列表加载失败，请稍后重试。");
    } finally {
      if (mounted.current) setLoading(false);
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  async function loadMore() {
    if (!nextCursor || loadingMore) return;
    setLoadingMore(true);
    try {
      const page = await getBookmarks(nextCursor, 20);
      if (!mounted.current) return;
      // 翻页期间可能有帖子被取消收藏，按 ID 去重避免同一张卡片出现两次。
      setPosts((prev) => {
        const seen = new Set(prev.map((p) => p.id));
        return [...prev, ...page.posts.filter((p) => !seen.has(p.id))];
      });
      setNextCursor(page.nextCursor);
    } catch (err) {
      if (!mounted.current) return;
      setError(err instanceof AuthError ? err.message : "加载更多失败，请稍后重试。");
    } finally {
      if (mounted.current) setLoadingMore(false);
    }
  }

  /**
   * 卡片状态变化时同步列表。
   *
   * 取消收藏（bookmarked=false）意味着它不该再出现在收藏列表里，直接移除；
   * 点赞、编辑等其它变化只更新对应字段。
   */
  function handlePatch(next: PostDTO) {
    if (!next.bookmarked) {
      setPosts((prev) => prev.filter((p) => p.id !== next.id));
      return;
    }
    setPosts((prev) => prev.map((p) => (p.id === next.id ? next : p)));
  }

  /** 帖子被删除：与取消收藏同理，它不应再留在任何列表里。 */
  function handleDeleted(id: string) {
    setPosts((prev) => prev.filter((p) => p.id !== id));
  }

  return (
    <main className="mx-auto w-full max-w-3xl px-5 py-10">
      <div className="mb-8 border-b border-line pb-7">
        <p className="mb-3 font-mono text-[0.72rem] tracking-[0.16em] text-ink-3 uppercase">
          收藏 · Bookmarks
        </p>
        <h1 className="font-display text-[clamp(1.7rem,4vw,2.3rem)] leading-[1.08] font-semibold tracking-[-0.03em] text-balance">
          你收藏的帖子
        </h1>
        <p className="mt-3 max-w-[58ch] text-[0.95rem] leading-relaxed text-ink-2">
          按收藏时间倒序排列。取消收藏后帖子会从这里移除。
        </p>
      </div>

      {error && (
        <div
          role="alert"
          className="mb-6 flex items-start gap-2.5 rounded-[6px] border border-danger/45 bg-danger-wash px-3.5 py-3 text-[0.85rem] leading-relaxed text-danger"
        >
          <span aria-hidden className="mt-0.5 font-mono font-semibold">
            !
          </span>
          <span className="flex-1">{error}</span>
          <button
            type="button"
            onClick={() => void load()}
            className="shrink-0 font-semibold underline underline-offset-2"
          >
            重试
          </button>
        </div>
      )}

      {loading ? (
        <div className="flex flex-col gap-3" aria-hidden>
          {[0, 1, 2].map((i) => (
            <div key={i} className="h-[132px] animate-pulse rounded-[12px] bg-surface-2" />
          ))}
        </div>
      ) : posts.length === 0 ? (
        <div className="rounded-[12px] border border-dashed border-line-strong bg-surface px-5 py-12 text-center">
          <p className="font-display text-[1.1rem] font-semibold">还没有收藏</p>
          <p className="mx-auto mt-2 max-w-[40ch] text-[0.88rem] leading-relaxed text-ink-2">
            在帖子上点书签图标即可保存。
          </p>
          <Link
            href="/"
            className="mt-5 inline-flex rounded-[6px] bg-seal px-4 py-1.5 text-[0.85rem] font-semibold text-white hover:bg-seal-deep"
          >
            去看看帖子
          </Link>
        </div>
      ) : (
        <>
          <ul className="flex flex-col gap-3">
            {posts.map((post) => (
              <li key={post.id}>
                <PostCard post={post} onPatch={handlePatch} onDeleted={handleDeleted} />
              </li>
            ))}
          </ul>

          {nextCursor && (
            <button
              type="button"
              onClick={loadMore}
              disabled={loadingMore}
              className="mt-5 w-full rounded-[6px] border border-line-strong py-2.5 text-[0.85rem] font-semibold text-ink transition-colors hover:border-ink-3 disabled:cursor-not-allowed disabled:opacity-60"
            >
              {loadingMore ? "加载中…" : "加载更多"}
            </button>
          )}
        </>
      )}
    </main>
  );
}
