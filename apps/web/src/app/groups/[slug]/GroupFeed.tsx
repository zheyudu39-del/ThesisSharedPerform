"use client";

import Link from "next/link";
import { useCallback, useEffect, useRef, useState } from "react";
import { useGroupMembership } from "@/components/GroupMembership";
import { PostCard } from "@/components/PostCard";
import { AuthError } from "@/lib/auth";
import { getGroupFeed, type GroupFeedPage } from "@/lib/groups";
import type { PostDTO } from "@/lib/posts";

/**
 * 群内动态（客户端）。
 *
 * 首屏帖子由 Server Component 直出（成员才可见），这里只负责：
 * - 「加载更多」按游标翻页；
 * - 用户在本页加入群组后自动拉取首屏（服务端渲染时还不是成员）；
 * - 点赞 / 收藏等互动沿用 PostCard 的乐观更新。
 */

export type GroupFeedProps = {
  slug: string;
  initialPosts: PostDTO[];
  initialNextCursor: string | null;
  /** 首屏是否成功获取；false 时展示「暂时无法加载」而不是「还没有动态」。 */
  feedAvailable: boolean;
};

export function GroupFeed({
  slug,
  initialPosts,
  initialNextCursor,
  feedAvailable,
}: GroupFeedProps) {
  const [posts, setPosts] = useState<PostDTO[]>(initialPosts);
  const [nextCursor, setNextCursor] = useState<string | null>(initialNextCursor);
  const [loading, setLoading] = useState(false);
  const [loadingMore, setLoadingMore] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [hasLoaded, setHasLoaded] = useState(feedAvailable);

  const mounted = useRef(true);
  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
    };
  }, []);

  const loadFirstPage = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const page: GroupFeedPage = await getGroupFeed(slug, null, 20);
      if (!mounted.current) return;
      setPosts(page.posts);
      setNextCursor(page.nextCursor);
      setHasLoaded(true);
    } catch (err) {
      if (!mounted.current) return;
      setError(err instanceof AuthError ? err.message : "群内动态加载失败。");
    } finally {
      if (mounted.current) setLoading(false);
    }
  }, [slug]);

  // 加入群组后自动加载动态（此时服务端首屏还是「非成员」状态）。
  useGroupMembership(slug, (detail) => {
    if (detail.myRole && !hasLoaded && !loading) void loadFirstPage();
  });

  async function loadMore() {
    if (!nextCursor || loadingMore) return;
    setLoadingMore(true);
    setError(null);
    try {
      const page = await getGroupFeed(slug, nextCursor, 20);
      if (!mounted.current) return;
      setPosts((prev) => {
        const seen = new Set(prev.map((p) => p.id));
        return [...prev, ...page.posts.filter((p) => !seen.has(p.id))];
      });
      setNextCursor(page.nextCursor);
    } catch (err) {
      if (!mounted.current) return;
      setError(err instanceof AuthError ? err.message : "加载更多失败。");
    } finally {
      if (mounted.current) setLoadingMore(false);
    }
  }

  function patchPost(next: PostDTO) {
    setPosts((prev) => prev.map((p) => (p.id === next.id ? next : p)));
  }

  if (loading) {
    return (
      <div className="flex flex-col gap-3" aria-hidden>
        {[0, 1].map((i) => (
          <div key={i} className="h-40 animate-pulse rounded-[12px] bg-surface-2" />
        ))}
      </div>
    );
  }

  if (posts.length === 0) {
    return (
      <div className="rounded-[12px] border border-dashed border-line-strong bg-surface px-5 py-10 text-center">
        <p className="text-[0.88rem] leading-relaxed text-ink-2">
          {hasLoaded
            ? "群里还没有动态。群内成员发布的群组可见帖子会出现在这里。"
            : "群内动态暂时无法加载，请稍后重试。"}
        </p>
        {!hasLoaded && (
          <button
            type="button"
            onClick={() => void loadFirstPage()}
            className="mt-4 rounded-[6px] border border-line-strong px-3 py-1.5 text-[0.82rem] font-semibold text-ink hover:border-ink-3"
          >
            重试
          </button>
        )}
        {hasLoaded && (
          <Link
            href="/"
            className="mt-4 inline-flex rounded-[6px] bg-seal px-4 py-1.5 text-[0.82rem] font-semibold text-white hover:bg-seal-deep"
          >
            去发帖
          </Link>
        )}
      </div>
    );
  }

  return (
    <div>
      {error && (
        <p role="alert" className="mb-3 rounded-[6px] border border-danger/40 bg-danger-wash px-3 py-2 text-[0.8rem] text-danger">
          {error}
        </p>
      )}

      <ul className="flex flex-col gap-3">
        {posts.map((post) => (
          <li key={post.id}>
            <PostCard post={post} onPatch={patchPost} />
          </li>
        ))}
      </ul>

      {nextCursor && (
        <button
          type="button"
          onClick={loadMore}
          disabled={loadingMore}
          className="mt-4 w-full rounded-[6px] border border-line-strong py-2.5 text-[0.85rem] font-semibold text-ink transition-colors hover:border-ink-3 disabled:cursor-not-allowed disabled:opacity-60"
        >
          {loadingMore ? "加载中…" : "加载更多动态"}
        </button>
      )}
    </div>
  );
}
