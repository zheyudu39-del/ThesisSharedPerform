"use client";

import Link from "next/link";
import { useCallback, useEffect, useRef, useState } from "react";
import { useAuth } from "@/components/AuthProvider";
import { PostCard } from "@/components/PostCard";
import { PostComposer } from "@/components/PostComposer";
import { SiteHeader } from "@/components/SiteHeader";
import { AuthError } from "@/lib/auth";
import { getHealth, type HealthResponse } from "@/lib/api";
import { getFeed, type FeedStream, type PostDTO } from "@/lib/posts";

/**
 * 首页信息流。
 *
 * 三档流：关注流（following）/ 全站流（all）/ 热门流（hot）。
 * 分页使用服务端不透明游标 nextCursor：触底自动加载，同时保留「加载更多」按钮，
 * 便于键盘用户与触底观察器不可用的环境。
 *
 * 未登录策略（见 README / 任务说明）：
 * - 关注流需要身份，未登录时直接给出「登录后查看关注流」引导，不发请求；
 * - 全站流 / 热门流先匿名请求，若服务端要求鉴权（401）则整块切换为统一登录引导。
 */

const STREAM_LABEL: Record<FeedStream, string> = {
  following: "关注流",
  all: "全站流",
  hot: "热门流",
};

const STREAM_HINT: Record<FeedStream, string> = {
  following: "时间倒序 · 无加权",
  all: "全站公开帖子 · 时间倒序",
  hot: "按热度排序 · 规则公开",
};

const STREAM_EMPTY: Record<FeedStream, string> = {
  following: "你关注的人还没有发帖。去全站流看看，或关注几位学者。",
  all: "全站还没有公开帖子。成为第一个发帖的人。",
  hot: "热门流暂时为空。",
};

const STREAM_STORAGE_KEY = "thesis_feed_stream";

const STREAMS: FeedStream[] = ["following", "all", "hot"];

export default function HomePage() {
  const { status, user } = useAuth();
  const [stream, setStream] = useState<FeedStream>("following");
  const [posts, setPosts] = useState<PostDTO[]>([]);
  const [nextCursor, setNextCursor] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);
  const [loadingMore, setLoadingMore] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [authRequired, setAuthRequired] = useState(false);
  const [health, setHealth] = useState<HealthResponse | null>(null);

  // 请求序号：切换流时丢弃上一档的迟到响应，避免列表被旧数据覆盖。
  const requestId = useRef(0);

  // 会话内保持上次选择的流（仅体验优化，失败不影响功能）。
  useEffect(() => {
    try {
      const saved = window.sessionStorage.getItem(STREAM_STORAGE_KEY);
      if (saved === "following" || saved === "all" || saved === "hot") setStream(saved);
    } catch {
      // 隐私模式 / 禁用站点数据：保持默认关注流。
    }
  }, []);

  function changeStream(next: FeedStream) {
    if (next === stream) return;
    setStream(next);
    try {
      window.sessionStorage.setItem(STREAM_STORAGE_KEY, next);
    } catch {
      // 忽略：仅影响下次打开的默认标签。
    }
  }

  // 服务状态只用于侧栏展示，失败静默。
  useEffect(() => {
    let cancelled = false;
    void getHealth().then((data) => {
      if (!cancelled) setHealth(data);
    });
    return () => {
      cancelled = true;
    };
  }, []);

  const loadFirstPage = useCallback(
    async (target: FeedStream) => {
      const id = ++requestId.current;
      setLoading(true);
      setError(null);
      setAuthRequired(false);
      setPosts([]);
      setNextCursor(null);

      try {
        const page = await getFeed(target, null, 20);
        if (id !== requestId.current) return;
        setPosts(page.posts);
        setNextCursor(page.nextCursor);
      } catch (err) {
        if (id !== requestId.current) return;
        if (err instanceof AuthError && err.status === 401) {
          setAuthRequired(true);
        } else {
          setError(err instanceof AuthError ? err.message : "信息流加载失败，请稍后重试。");
        }
      } finally {
        if (id === requestId.current) setLoading(false);
      }
    },
    [],
  );

  // 后端所有信息流接口都要求登录：未登录统一展示登录引导，不发请求。
  // 登录态尚未确定（loading）时也不请求，避免用错误身份拿一页数据。
  useEffect(() => {
    if (status === "loading") return;
    if (status === "anonymous") {
      requestId.current += 1; // 作废可能仍在途的请求
      setPosts([]);
      setNextCursor(null);
      setLoading(false);
      setError(null);
      setAuthRequired(false);
      return;
    }
    void loadFirstPage(stream);
  }, [stream, status, loadFirstPage]);

  async function loadMore() {
    if (!nextCursor || loadingMore || loading) return;
    const id = requestId.current;
    setLoadingMore(true);
    setError(null);
    try {
      const page = await getFeed(stream, nextCursor, 20);
      if (id !== requestId.current) return;
      // 游标稳定保证不重不漏；这里仍按 id 去重，防止并发产生重复渲染键。
      setPosts((prev) => {
        const seen = new Set(prev.map((p) => p.id));
        return [...prev, ...page.posts.filter((p) => !seen.has(p.id))];
      });
      // 游标未推进视为到底，避免触底观察器原地反复请求同一页。
      setNextCursor(page.nextCursor === nextCursor ? null : page.nextCursor);
    } catch (err) {
      if (id !== requestId.current) return;
      setError(err instanceof AuthError ? err.message : "加载更多失败，请重试。");
    } finally {
      if (id === requestId.current) setLoadingMore(false);
    }
  }

  function patchPost(next: PostDTO) {
    setPosts((prev) => prev.map((p) => (p.id === next.id ? next : p)));
  }

  function insertPost(created: PostDTO) {
    // 只有可能出现在当前流里的帖子才插入，避免「发了却看不到」的困惑。
    const isOwn = user !== null && created.author.id === user.id;
    const visibleInStream =
      stream === "following"
        ? // 关注流含自己的全部帖子 + 他人公开帖
          isOwn || (created.visibility === "public" && created.groupId === null)
        : created.visibility === "public" && created.groupId === null;
    if (!visibleInStream) return;

    setPosts((prev) => {
      if (prev.some((p) => p.id === created.id)) return prev;
      return [created, ...prev];
    });
  }

  // 未登录：所有流都统一给登录引导（后端 feed 全部鉴权）。
  // 已登录但服务端仍返回 401（令牌过期）：同样引导重新登录。
  const showAnonymousGate = status === "anonymous" || (status === "authenticated" && authRequired);
  const showFeed = !showAnonymousGate;

  return (
    <>
      <SiteHeader />

      <main className="mx-auto max-w-6xl px-5 py-10">
        {/* 页面标题区 */}
        <div className="mb-8 border-b border-line pb-7">
          <p className="mb-3 font-mono text-[0.72rem] tracking-[0.16em] text-ink-3 uppercase">
            信息流 · Feed
          </p>
          <h1 className="font-display text-[clamp(1.8rem,4vw,2.6rem)] leading-[1.05] font-semibold tracking-[-0.03em] text-balance">
            以人的判断，筛选前沿论文
          </h1>
          <p className="mt-3 max-w-[62ch] text-[1rem] text-ink-2">
            不做个性化推荐算法。你看到的内容，来自你关注的人的选择。
          </p>
        </div>

        <div className="grid gap-8 lg:grid-cols-[minmax(0,1fr)_280px]">
          {/* 信息流 */}
          <div className="flex min-w-0 flex-col gap-4">
            {/* 三档流切换 + 精选入口 */}
            <div className="flex flex-wrap items-center gap-1.5">
              {STREAMS.map((s) => (
                <Tab key={s} active={stream === s} onClick={() => changeStream(s)}>
                  {STREAM_LABEL[s]}
                </Tab>
              ))}
              {/*
                精选是「策展集」而不是帖子流，数据结构与分页方式都不同，
                因此做成跳转到 /collections 的入口，而不是第四个 feed stream。
                混进来会让这里的游标、去重与插入逻辑全部需要分支处理。
              */}
              <Link
                href="/collections"
                className="rounded-[6px] px-3 py-1.5 text-[0.84rem] font-medium text-ink-2 transition-colors hover:bg-surface-2 hover:text-ink"
              >
                精选
              </Link>
              <span className="ml-2 font-mono text-[0.7rem] text-ink-3">{STREAM_HINT[stream]}</span>
            </div>

            {/* 发帖框：登录后显示 */}
            {status === "authenticated" && !authRequired && (
              <PostComposer onCreated={insertPost} />
            )}

            {showAnonymousGate && (
              <LoginGate
                title={
                  status === "anonymous"
                    ? stream === "following"
                      ? "登录后查看关注流"
                      : "登录后浏览信息流"
                    : "登录状态已失效"
                }
                body={
                  status === "anonymous"
                    ? "关注流展示你关注的人的公开帖子与你自己发的内容。全站流与热门流同样需要登录后访问。"
                    : "令牌可能已过期，重新登录后即可继续阅读与互动。"
                }
              />
            )}

            {showFeed && (status === "loading" || loading) && <FeedSkeleton />}

            {showFeed && status !== "loading" && !loading && error && !posts.length && (
              <ErrorState message={error} onRetry={() => void loadFirstPage(stream)} />
            )}

            {showFeed && status !== "loading" && !loading && !error && posts.length === 0 && (
              <EmptyState stream={stream} onSwitch={changeStream} />
            )}

            {showFeed && posts.length > 0 && (
              <div className="flex flex-col gap-4">
                {posts.map((post) => (
                  <PostCard key={post.id} post={post} onPatch={patchPost} />
                ))}

                {/* 分页尾部 */}
                <div className="flex flex-col items-center gap-2 py-2">
                  {error && (
                    <p role="alert" className="text-[0.82rem] text-danger">
                      {error}
                    </p>
                  )}
                  {nextCursor ? (
                    <LoadMoreButton
                      cursor={nextCursor}
                      loading={loadingMore}
                      onClick={() => void loadMore()}
                    />
                  ) : (
                    <p className="font-mono text-[0.7rem] tracking-[0.08em] text-ink-3 uppercase">
                      已经到底了
                    </p>
                  )}
                </div>
              </div>
            )}
          </div>

          {/* 侧栏 */}
          <aside className="hidden flex-col gap-4 lg:flex">
            <SidePanel title="排序规则">
              <p className="text-[0.82rem] leading-relaxed text-ink-2">
                关注流按发布时间倒序，不做任何加权。热门流使用公开的热度公式，规则对所有人一致。
              </p>
              <pre className="mt-3 overflow-x-auto rounded-[6px] border border-line bg-surface-2 p-3 font-mono text-[0.72rem] leading-relaxed text-ink-2">
{`raw = 1.0·likes + 2.0·comments
    + 3.0·reposts
hot = raw / (hours + 2)^1.5`}
              </pre>
            </SidePanel>

            <SidePanel title="服务状态">
              <div className="flex flex-col gap-2 text-[0.82rem]">
                <StatusRow label="API" ok={health?.status === "ok"} value={health?.status ?? "不可达"} />
                <StatusRow label="数据库" ok={health?.database === "up"} value={health?.database ?? "—"} />
                <StatusRow label="版本" ok value={health?.version ?? "—"} mono />
              </div>
            </SidePanel>
          </aside>
        </div>
      </main>
    </>
  );
}

/* -------------------------------------------------------------------------- */
/* 分页                                                                        */
/* -------------------------------------------------------------------------- */

function LoadMoreButton({
  cursor,
  loading,
  onClick,
}: {
  /** 当前游标：变化后重新建立观察，保证「加载更多」后能继续触底加载。 */
  cursor: string;
  loading: boolean;
  onClick: () => void;
}) {
  const ref = useRef<HTMLButtonElement>(null);
  const onClickRef = useRef(onClick);

  // 用 ref 持有最新回调，避免父级每次渲染都重建观察器。
  useEffect(() => {
    onClickRef.current = onClick;
  }, [onClick]);

  // 触底自动加载：观察按钮进入视口即触发；按钮本身仍可点击。
  useEffect(() => {
    const node = ref.current;
    if (!node || loading || typeof IntersectionObserver === "undefined") return;

    const observer = new IntersectionObserver(
      (entries) => {
        if (entries.some((e) => e.isIntersecting)) onClickRef.current();
      },
      { rootMargin: "200px 0px" },
    );
    observer.observe(node);
    return () => observer.disconnect();
    // cursor 变化即翻页成功，需要重新观察新位置的按钮。
  }, [cursor, loading]);

  return (
    <button
      ref={ref}
      type="button"
      onClick={onClick}
      disabled={loading}
      aria-busy={loading}
      className="inline-flex items-center gap-2 rounded-[6px] border border-line-strong bg-surface px-5 py-2 text-[0.86rem] font-semibold text-ink hover:border-ink-3 disabled:cursor-not-allowed disabled:opacity-60"
    >
      {loading && (
        <span
          aria-hidden
          className="size-3.5 animate-spin rounded-full border-2 border-ink-3/40 border-t-ink"
        />
      )}
      {loading ? "加载中…" : "加载更多"}
    </button>
  );
}

/* -------------------------------------------------------------------------- */
/* 状态视图                                                                    */
/* -------------------------------------------------------------------------- */

function FeedSkeleton() {
  return (
    <div className="flex flex-col gap-4" aria-hidden>
      {[0, 1, 2].map((i) => (
        <div
          key={i}
          className="rounded-[12px] border border-line bg-surface p-4 shadow-[var(--shadow-1)]"
        >
          <div className="mb-3 flex items-center gap-2.5">
            <div className="size-[34px] shrink-0 animate-pulse rounded-full bg-surface-2" />
            <div className="flex-1">
              <div className="mb-1.5 h-3 w-28 animate-pulse rounded-[3px] bg-surface-2" />
              <div className="h-2.5 w-40 animate-pulse rounded-[3px] bg-surface-2" />
            </div>
          </div>
          <div className="mb-2 h-3 w-full animate-pulse rounded-[3px] bg-surface-2" />
          <div className="mb-2 h-3 w-11/12 animate-pulse rounded-[3px] bg-surface-2" />
          <div className="mb-3 h-3 w-2/3 animate-pulse rounded-[3px] bg-surface-2" />
          <div className="h-24 w-full animate-pulse rounded-[6px] bg-surface-2" />
        </div>
      ))}
    </div>
  );
}

function LoginGate({ title, body }: { title: string; body: string }) {
  return (
    <div className="rounded-[12px] border border-line bg-surface p-6 shadow-[var(--shadow-1)]">
      <p className="mb-1.5 font-mono text-[0.68rem] tracking-[0.12em] text-ink-3 uppercase">
        需要登录 · Sign in
      </p>
      <h2 className="font-display text-[1.15rem] font-semibold">{title}</h2>
      <p className="mt-2 max-w-[52ch] text-[0.88rem] leading-relaxed text-ink-2">{body}</p>
      <div className="mt-4 flex flex-wrap items-center gap-2">
        <Link
          href="/login"
          className="rounded-[6px] bg-seal px-4 py-1.5 text-[0.85rem] font-semibold text-white hover:bg-seal-deep"
        >
          登录
        </Link>
        <Link
          href="/register"
          className="rounded-[6px] border border-line-strong px-4 py-1.5 text-[0.85rem] font-semibold text-ink hover:border-ink-3"
        >
          注册
        </Link>
      </div>
    </div>
  );
}

function ErrorState({ message, onRetry }: { message: string; onRetry: () => void }) {
  return (
    <div className="rounded-[12px] border border-line bg-surface p-6 text-center">
      <p className="font-mono text-[0.68rem] tracking-[0.12em] text-danger uppercase">加载失败</p>
      <p className="mt-2 text-[0.88rem] leading-relaxed text-ink-2">{message}</p>
      <button
        type="button"
        onClick={onRetry}
        className="mt-4 rounded-[6px] border border-line-strong bg-surface px-4 py-1.5 text-[0.85rem] font-semibold text-ink hover:border-ink-3"
      >
        重试
      </button>
    </div>
  );
}

function EmptyState({
  stream,
  onSwitch,
}: {
  stream: FeedStream;
  onSwitch: (next: FeedStream) => void;
}) {
  return (
    <div className="rounded-[12px] border border-dashed border-line-strong bg-surface px-6 py-10 text-center">
      <p className="font-mono text-[0.68rem] tracking-[0.12em] text-ink-3 uppercase">
        {STREAM_LABEL[stream]} · 暂无内容
      </p>
      <p className="mx-auto mt-2 max-w-[44ch] text-[0.88rem] leading-relaxed text-ink-2">
        {STREAM_EMPTY[stream]}
      </p>
      {stream !== "all" && (
        <button
          type="button"
          onClick={() => onSwitch("all")}
          className="mt-4 rounded-[6px] border border-line-strong px-4 py-1.5 text-[0.85rem] font-semibold text-ink hover:border-ink-3"
        >
          去全站流看看
        </button>
      )}
    </div>
  );
}

/* -------------------------------------------------------------------------- */
/* 基础构件                                                                    */
/* -------------------------------------------------------------------------- */

function Tab({
  children,
  active = false,
  onClick,
}: {
  children: React.ReactNode;
  active?: boolean;
  onClick?: () => void;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      aria-pressed={active}
      className={`rounded-[6px] px-3.5 py-1.5 text-[0.9rem] transition-colors ${
        active
          ? "border border-line bg-surface font-semibold text-ink shadow-[var(--shadow-1)]"
          : "text-ink-2 hover:bg-surface-2 hover:text-ink"
      }`}
    >
      {children}
    </button>
  );
}

function SidePanel({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <section className="rounded-[12px] border border-line bg-surface p-4">
      <h2 className="mb-3 font-mono text-[0.7rem] font-semibold tracking-[0.1em] text-ink-3 uppercase">
        {title}
      </h2>
      {children}
    </section>
  );
}

function StatusRow({
  label,
  ok,
  value,
  mono = false,
}: {
  label: string;
  ok: boolean;
  value: string;
  mono?: boolean;
}) {
  return (
    <div className="flex items-center justify-between gap-3">
      <span className="text-ink-2">{label}</span>
      <span className={`flex items-center gap-1.5 ${mono ? "font-mono" : ""}`}>
        <span className={`size-1.5 rounded-full ${ok ? "bg-good" : "bg-danger"}`} />
        {value}
      </span>
    </div>
  );
}
