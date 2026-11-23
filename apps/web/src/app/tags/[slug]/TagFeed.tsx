"use client";

import { useCallback, useEffect, useState } from "react";
import { PostCard } from "@/components/PostCard";
import { getTagPosts, type PostDTO } from "@/lib/posts";

/**
 * 话题页的帖子列表（PRD F-07）。
 *
 * 页面骨架与标题由 Server Component 直出（便于 SEO），列表按需分页，
 * 因此拆成客户端组件。
 */
export function TagFeed({ slug }: { slug: string }) {
  const [posts, setPosts] = useState<PostDTO[]>([]);
  const [cursor, setCursor] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const [loadingMore, setLoadingMore] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(
    async (nextCursor: string | null, append: boolean) => {
      if (append) setLoadingMore(true);
      else setLoading(true);
      setError(null);
      try {
        const page = await getTagPosts(slug, nextCursor);
        setPosts((prev) => (append ? [...prev, ...page.posts] : page.posts));
        setCursor(page.nextCursor);
      } catch (err) {
        setError(err instanceof Error ? err.message : "加载失败，请稍后重试。");
      } finally {
        setLoading(false);
        setLoadingMore(false);
      }
    },
    [slug],
  );

  useEffect(() => {
    void load(null, false);
  }, [load]);

  if (loading) {
    return (
      <div className="flex flex-col gap-3" aria-hidden>
        {[0, 1, 2].map((i) => (
          <div key={i} className="h-32 animate-pulse rounded-[12px] border border-line bg-surface-2" />
        ))}
      </div>
    );
  }

  if (error && posts.length === 0) {
    return (
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
    );
  }

  if (posts.length === 0) {
    return (
      <p className="rounded-[12px] border border-dashed border-line-strong bg-surface px-5 py-8 text-center text-[0.86rem] leading-relaxed text-ink-2">
        这个话题下还没有公开帖子。发帖时带上这个标签，就会出现在这里。
      </p>
    );
  }

  return (
    <div className="flex flex-col gap-4">
      {posts.map((post, i) => (
        <PostCard key={post.id} post={post} entryIndex={i} />
      ))}
      <div className="flex flex-col items-center gap-2 py-2">
        {error && (
          <p role="alert" className="text-[0.82rem] text-danger">
            {error}
          </p>
        )}
        {cursor ? (
          <button
            type="button"
            disabled={loadingMore}
            onClick={() => void load(cursor, true)}
            className="rounded-[6px] border border-line-strong px-4 py-1.5 text-[0.8rem] text-ink-2 hover:border-ink-3 hover:text-ink disabled:opacity-50"
          >
            {loadingMore ? "加载中…" : "加载更多"}
          </button>
        ) : (
          <p className="font-mono text-[0.7rem] tracking-[0.08em] text-ink-3 uppercase">
            已经到底了
          </p>
        )}
      </div>
    </div>
  );
}
