"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { PostCard } from "@/components/PostCard";
import { getAuthorPosts, type PostDTO } from "@/lib/posts";

/**
 * 个人主页的「发帖 / 转发」列表（PRD A-03）。
 *
 * 为什么拆成客户端组件：分页要按需加载，切 Tab 时要丢弃上一份列表。
 * 主页其余部分（姓名、简介、统计）留在 Server Component 里直接 SSR，
 * 爬虫不需要这段 JS 就能拿到资料。
 *
 * 两个 Tab 各自持有游标与列表状态，切回来时不需要重新拉第一页。
 */

type TabKey = "posts" | "reposts";

const TAB_LABEL: Record<TabKey, string> = {
  posts: "发帖",
  reposts: "转发",
};

const EMPTY_TEXT: Record<TabKey, string> = {
  posts: "还没有公开的原创帖。",
  reposts: "还没有公开的转发。",
};

type TabState = {
  posts: PostDTO[];
  cursor: string | null;
  loaded: boolean;
  loading: boolean;
  error: string | null;
};

const emptyState = (): TabState => ({
  posts: [],
  cursor: null,
  loaded: false,
  loading: false,
  error: null,
});

export function ProfilePosts({ handle }: { handle: string }) {
  const [active, setActive] = useState<TabKey>("posts");
  const [tabs, setTabs] = useState<Record<TabKey, TabState>>({
    posts: emptyState(),
    reposts: emptyState(),
  });
  // 防止切走之后旧的请求回来覆盖新 Tab 的状态。
  const requestRef = useRef<Record<TabKey, number>>({ posts: 0, reposts: 0 });

  const load = useCallback(
    async (kind: TabKey, cursor: string | null, append: boolean) => {
      const token = ++requestRef.current[kind];
      setTabs((prev) => ({
        ...prev,
        [kind]: { ...prev[kind], loading: true, error: null },
      }));
      try {
        const page = await getAuthorPosts(handle, kind, cursor);
        // 期间用户可能切了 Tab 又切回来并触发了新请求，旧响应直接丢弃。
        if (token !== requestRef.current[kind]) return;
        setTabs((prev) => ({
          ...prev,
          [kind]: {
            posts: append ? [...prev[kind].posts, ...page.posts] : page.posts,
            cursor: page.nextCursor,
            loaded: true,
            loading: false,
            error: null,
          },
        }));
      } catch (err) {
        if (token !== requestRef.current[kind]) return;
        setTabs((prev) => ({
          ...prev,
          [kind]: {
            ...prev[kind],
            loaded: true,
            loading: false,
            error: err instanceof Error ? err.message : "加载失败，请稍后重试。",
          },
        }));
      }
    },
    [handle],
  );

  // 首次进入或切到未加载过的 Tab 时拉第一页。
  useEffect(() => {
    if (!tabs[active].loaded && !tabs[active].loading) {
      void load(active, null, false);
    }
  }, [active, tabs, load]);

  const state = tabs[active];

  return (
    <section className="mt-6">
      <div role="tablist" aria-label="用户内容" className="mb-4 flex gap-1 border-b border-line">
        {(Object.keys(TAB_LABEL) as TabKey[]).map((kind) => {
          const selected = kind === active;
          return (
            <button
              key={kind}
              type="button"
              role="tab"
              aria-selected={selected}
              onClick={() => setActive(kind)}
              className={`-mb-px border-b-2 px-3.5 py-2 text-[0.85rem] transition-colors ${
                selected
                  ? "border-seal font-semibold text-ink"
                  : "border-transparent text-ink-3 hover:text-ink-2"
              }`}
            >
              {TAB_LABEL[kind]}
            </button>
          );
        })}
      </div>

      {state.loading && !state.posts.length && (
        <div className="flex flex-col gap-3" aria-hidden>
          {[0, 1, 2].map((i) => (
            <div
              key={i}
              className="h-28 animate-pulse rounded-[12px] border border-line bg-surface-2"
            />
          ))}
        </div>
      )}

      {state.error && (
        <div className="rounded-[10px] border border-danger/40 bg-danger-wash px-4 py-3">
          <p className="text-[0.84rem] text-danger">{state.error}</p>
          <button
            type="button"
            onClick={() => void load(active, null, false)}
            className="mt-2 text-[0.8rem] font-semibold text-danger underline"
          >
            重试
          </button>
        </div>
      )}

      {!state.loading && !state.error && state.loaded && state.posts.length === 0 && (
        <p className="rounded-[12px] border border-dashed border-line-strong bg-surface px-5 py-6 text-center text-[0.85rem] text-ink-2">
          {EMPTY_TEXT[active]}
        </p>
      )}

      {state.posts.length > 0 && (
        <div className="flex flex-col gap-4">
          {state.posts.map((post, i) => (
            <PostCard key={post.id} post={post} entryIndex={i} />
          ))}
          <div className="flex flex-col items-center gap-2 py-2">
            {state.cursor ? (
              <button
                type="button"
                disabled={state.loading}
                onClick={() => void load(active, state.cursor, true)}
                className="rounded-[6px] border border-line-strong px-4 py-1.5 text-[0.8rem] text-ink-2 hover:border-ink-3 hover:text-ink disabled:opacity-50"
              >
                {state.loading ? "加载中…" : "加载更多"}
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
