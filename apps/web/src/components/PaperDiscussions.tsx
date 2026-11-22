"use client";

import { useCallback, useEffect, useState } from "react";
import { PostCard } from "@/components/PostCard";
import { getPaperPosts, type PostDTO } from "@/lib/posts";

/**
 * 论文详情页的「相关讨论」（PRD P-03）。
 *
 * 展示所有公开挂在当前论文下的帖子。群内讨论不出现——它属于群组上下文，
 * 服务端已经在查询里排除（见 ListPaperPosts 的 WHERE）。
 *
 * 为什么是客户端组件：列表要按需分页，且首屏不该因为讨论区拖慢论文页的
 * SSR。论文元信息仍由 Server Component 直出。
 */

export function PaperDiscussions({ paperId }: { paperId: string }) {
  const [posts, setPosts] = useState<PostDTO[]>([]);
  const [total, setTotal] = useState<number | null>(null);
  const [cursor, setCursor] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(
    async (nextCursor: string | null, append: boolean) => {
      setLoading(true);
      setError(null);
      try {
        const page = await getPaperPosts(paperId, nextCursor);
        setPosts((prev) => (append ? [...prev, ...page.posts] : page.posts));
        setCursor(page.nextCursor);
        if (page.total !== null) setTotal(page.total);
      } catch (err) {
        setError(err instanceof Error ? err.message : "加载讨论失败，请稍后重试。");
      } finally {
        setLoading(false);
      }
    },
    [paperId],
  );

  useEffect(() => {
    void load(null, false);
  }, [load]);

  return (
    <section id="discussions" className="mb-8">
      <h2 className="mb-2.5 font-mono text-[0.7rem] font-semibold tracking-[0.1em] text-ink-3 uppercase">
        相关讨论{total !== null && total > 0 ? ` · ${total}` : ""}
      </h2>

      {loading && posts.length === 0 && (
        <div className="flex flex-col gap-3" aria-hidden>
          {[0, 1].map((i) => (
            <div
              key={i}
              className="h-28 animate-pulse rounded-[12px] border border-line bg-surface-2"
            />
          ))}
        </div>
      )}

      {error && (
        <div className="rounded-[10px] border border-danger/40 bg-danger-wash px-4 py-3">
          <p className="text-[0.84rem] text-danger">{error}</p>
          <button
            type="button"
            onClick={() => void load(null, false)}
            className="mt-2 text-[0.8rem] font-semibold text-danger underline"
          >
            重试
          </button>
        </div>
      )}

      {!loading && !error && posts.length === 0 && (
        <p className="rounded-[12px] border border-dashed border-line-strong bg-surface px-5 py-6 text-center text-[0.85rem] leading-relaxed text-ink-2">
          还没有人讨论这篇论文。可以回到信息流，引用它写下你的判断。
        </p>
      )}

      {posts.length > 0 && (
        <div className="flex flex-col gap-4">
          {posts.map((post) => (
            <PostCard key={post.id} post={post} />
          ))}
          <div className="flex flex-col items-center gap-2 py-2">
            {cursor ? (
              <button
                type="button"
                disabled={loading}
                onClick={() => void load(cursor, true)}
                className="rounded-[6px] border border-line-strong px-4 py-1.5 text-[0.8rem] text-ink-2 hover:border-ink-3 hover:text-ink disabled:opacity-50"
              >
                {loading ? "加载中…" : "加载更多"}
              </button>
            ) : (
              <p className="font-mono text-[0.7rem] tracking-[0.08em] text-ink-3 uppercase">
                已经到底了
              </p>
            )}
          </div>
        </div>
      )}
    </section>
  );
}
