"use client";

import Link from "next/link";
import { usePathname, useRouter } from "next/navigation";
import { useEffect, useRef, useState } from "react";
import { useAuth } from "@/components/AuthProvider";
import { PaperPreviewCard } from "./PaperPreviewCard";
import { SealBadge } from "./SealBadge";
import { AuthError } from "@/lib/auth";
import {
  bookmarkPost,
  formatRelativeTime,
  likePost,
  rememberPost,
  unbookmarkPost,
  unlikePost,
  VISIBILITY_LABEL,
  type PostDTO,
} from "@/lib/posts";

/**
 * 信息流卡片（PRD §5.4 卡片形态）。
 * 结构：作者行 → 转发说明 → 正文 → 论文预览 / 引用原帖 → 标签 → 互动栏。
 *
 * - 数据来自真实 PostDTO；
 * - 点赞 / 收藏点击后立即反馈（乐观更新），请求失败回滚并提示；
 * - 引用转发只展示一层快照（repostOf 不含更深层级），因此转发最多两层。
 */

export type PostCardProps = {
  post: PostDTO;
  /** 帖子状态变化时回调，父级据此同步列表（点赞、收藏、评论数）。 */
  onPatch?: (next: PostDTO) => void;
  /** 详情页：不再引导跳转到自身详情。 */
  detail?: boolean;
};

type PendingAction = "like" | "bookmark" | null;

export function PostCard({ post, onPatch, detail = false }: PostCardProps) {
  const { status } = useAuth();
  const router = useRouter();
  const pathname = usePathname();

  const [liked, setLiked] = useState(post.liked);
  const [likeCount, setLikeCount] = useState(post.likeCount);
  const [bookmarked, setBookmarked] = useState(post.bookmarked);
  const [pending, setPending] = useState<PendingAction>(null);
  const [error, setError] = useState<string | null>(null);

  const mounted = useRef(true);
  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
    };
  }, []);

  // 父级刷新帖子数据时同步本地状态（例如详情页评论数更新）。
  useEffect(() => {
    setLiked(post.liked);
    setLikeCount(post.likeCount);
    setBookmarked(post.bookmarked);
  }, [post.id, post.liked, post.likeCount, post.bookmarked]);

  // 记住帖子快照，详情页可直接渲染，不必额外请求。
  useEffect(() => {
    rememberPost(post);
  }, [post]);

  // 错误提示不打断阅读，几秒后自动收起。
  useEffect(() => {
    if (!error) return;
    const timer = window.setTimeout(() => setError(null), 5000);
    return () => window.clearTimeout(timer);
  }, [error]);

  function requireLogin(): boolean {
    if (status === "authenticated") return false;
    const next = encodeURIComponent(pathname || "/");
    router.push(`/login?next=${next}`);
    return true;
  }

  function patch(partial: Partial<PostDTO>) {
    onPatch?.({ ...post, ...partial });
  }

  async function toggleLike() {
    if (pending) return;
    if (requireLogin()) return;

    const wasLiked = liked;
    const previousCount = likeCount;
    const nextLiked = !wasLiked;
    const nextCount = Math.max(0, previousCount + (nextLiked ? 1 : -1));

    // 乐观更新：立即变色。
    setLiked(nextLiked);
    setLikeCount(nextCount);
    patch({ liked: nextLiked, likeCount: nextCount });
    setPending("like");
    setError(null);

    try {
      const result = nextLiked ? await likePost(post.id) : await unlikePost(post.id);
      if (!mounted.current) return;
      // 以服务端计数为准，修正并发或幂等带来的偏差。
      setLiked(result.liked);
      setLikeCount(result.likeCount);
      patch({ liked: result.liked, likeCount: result.likeCount });
    } catch (err) {
      if (!mounted.current) return;
      setLiked(wasLiked);
      setLikeCount(previousCount);
      patch({ liked: wasLiked, likeCount: previousCount });
      setError(err instanceof AuthError ? err.message : "操作失败，请稍后重试。");
    } finally {
      if (mounted.current) setPending(null);
    }
  }

  async function toggleBookmark() {
    if (pending) return;
    if (requireLogin()) return;

    const wasBookmarked = bookmarked;
    const nextBookmarked = !wasBookmarked;

    setBookmarked(nextBookmarked);
    patch({ bookmarked: nextBookmarked });
    setPending("bookmark");
    setError(null);

    try {
      const result = nextBookmarked
        ? await bookmarkPost(post.id)
        : await unbookmarkPost(post.id);
      if (!mounted.current) return;
      setBookmarked(result.bookmarked);
      patch({ bookmarked: result.bookmarked });
    } catch (err) {
      if (!mounted.current) return;
      setBookmarked(wasBookmarked);
      patch({ bookmarked: wasBookmarked });
      setError(err instanceof AuthError ? err.message : "操作失败，请稍后重试。");
    } finally {
      if (mounted.current) setPending(null);
    }
  }

  /**
   * 转发：跳转到帖子详情并带 `repost=1`，由详情页打开转发框。
   * 未登录也允许跳转——详情页会展示带 next 的登录引导，保留转发意图。
   */
  function startRepost() {
    router.push(`${detailHref}?repost=1`);
  }

  const isRepost = post.type === "repost" && post.repostOf !== null;
  const initial = [...post.author.name][0] ?? "?";
  const detailHref = `/posts/${post.id}`;
  const hasContent = post.contentHtml.trim().length > 0 || post.contentText.trim().length > 0;

  return (
    <article className="rounded-[12px] border border-line bg-surface p-4.5 shadow-[var(--shadow-1)]">
      {/* 作者行 */}
      <div className="mb-3 flex items-center gap-2.5">
        <Link
          href={`/u/${encodeURIComponent(post.author.handle)}`}
          className="grid size-[34px] shrink-0 place-items-center rounded-full bg-ink font-display text-[15px] text-paper"
          aria-label={`查看 ${post.author.name} 的主页`}
        >
          {initial}
        </Link>
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-2 text-[0.9rem] font-semibold">
            <Link
              href={`/u/${encodeURIComponent(post.author.handle)}`}
              className="truncate hover:underline"
            >
              {post.author.name}
            </Link>
            {post.author.verifyType === "scholar" && <SealBadge variant="scholar" />}
            {post.author.verifyType === "influencer" && <SealBadge variant="influencer" />}
          </div>
          <div className="flex flex-wrap items-center gap-x-1.5 text-[0.78rem] text-ink-3">
            <span className="font-mono">@{post.author.handle}</span>
            <span aria-hidden>·</span>
            {detail ? (
              <time dateTime={post.createdAt}>{formatRelativeTime(post.createdAt)}</time>
            ) : (
              <Link href={detailHref} className="hover:text-ink-2">
                <time dateTime={post.createdAt}>{formatRelativeTime(post.createdAt)}</time>
              </Link>
            )}
            {post.visibility !== "public" && (
              <span className="ml-1 rounded-[3px] bg-surface-2 px-1.5 py-0.5 font-mono text-[0.66rem] tracking-[0.06em] text-ink-3">
                {VISIBILITY_LABEL[post.visibility]}
              </span>
            )}
          </div>
        </div>
      </div>

      {/* 转发说明 */}
      {isRepost && (
        <p className="mb-2 font-mono text-[0.72rem] tracking-[0.04em] text-ink-3">
          转发了{" "}
          <Link href={`/posts/${post.repostOf!.id}`} className="text-seal hover:underline">
            @{post.repostOf!.authorName}
          </Link>{" "}
          的帖子
        </p>
      )}

      {/* 正文 / 转发观点（服务端已 sanitize） */}
      {hasContent &&
        (post.contentHtml ? (
          <div
            className="mb-3.5 text-[0.92rem] leading-relaxed break-words [&_a]:text-seal [&_a]:underline [&_blockquote]:my-2 [&_blockquote]:border-l-2 [&_blockquote]:border-line-strong [&_blockquote]:pl-3 [&_blockquote]:text-ink-2 [&_code]:rounded-[3px] [&_code]:bg-surface-2 [&_code]:px-1 [&_code]:py-0.5 [&_code]:font-mono [&_code]:text-[0.84em] [&_h2]:mt-3 [&_h2]:mb-1.5 [&_h2]:font-display [&_h2]:text-[1.05rem] [&_h2]:font-semibold [&_h3]:mt-2.5 [&_h3]:mb-1 [&_h3]:font-display [&_h3]:font-semibold [&_li]:ml-4 [&_li]:list-disc [&_ol_li]:list-decimal [&_p]:mb-2 [&_p:last-child]:mb-0 [&_pre]:my-2 [&_pre]:overflow-x-auto [&_pre]:rounded-[6px] [&_pre]:bg-surface-2 [&_pre]:p-3 [&_pre]:font-mono [&_pre]:text-[0.8rem] [&_strong]:font-semibold"
            dangerouslySetInnerHTML={{ __html: post.contentHtml }}
          />
        ) : (
          <p className="mb-3.5 text-[0.92rem] leading-relaxed break-words whitespace-pre-wrap">
            {post.contentText}
          </p>
        ))}

      {/* 引用原帖（最多两层：快照内不再展开转发） */}
      {isRepost && (
        <Link
          href={`/posts/${post.repostOf!.id}`}
          className="mb-3.5 block rounded-[6px] border border-line bg-surface-2 px-3.5 py-3 transition-colors hover:border-line-strong"
        >
          <div className="mb-1.5 flex items-center gap-2 text-[0.8rem] font-semibold">
            <span>{post.repostOf!.authorName}</span>
            <span className="font-mono text-[0.68rem] font-normal text-ink-3">原帖</span>
          </div>
          {post.repostOf!.paper && (
            <div className="mb-2.5">
              <PaperPreviewCard paper={post.repostOf!.paper} compact />
            </div>
          )}
          {post.repostOf!.contentText ? (
            <p className="line-clamp-6 text-[0.86rem] leading-relaxed break-words whitespace-pre-wrap text-ink-2">
              {post.repostOf!.contentText}
            </p>
          ) : !post.repostOf!.paper ? (
            <p className="text-[0.82rem] text-ink-3">（原帖内容不可见）</p>
          ) : null}
        </Link>
      )}

      {/* 本贴引用的论文（转发时也保留，便于「转发 + 论文」的组合） */}
      {post.paper && (
        <div className="mb-3.5">
          <PaperPreviewCard paper={post.paper} compact={isRepost} />
        </div>
      )}

      {/* 话题标签 */}
      {post.tags.length > 0 && (
        <div className="mb-3 flex flex-wrap gap-2">
          {post.tags.map((t) => (
            <span key={t} className="rounded-full bg-surface-2 px-3 py-1 text-[0.78rem] text-ink-2">
              <span className="font-mono text-seal">#</span>
              {t}
            </span>
          ))}
        </div>
      )}

      {/* 互动栏 */}
      <div className="flex flex-wrap items-center gap-1 border-t border-line pt-2.5">
        <button
          type="button"
          onClick={toggleLike}
          disabled={pending === "like"}
          aria-pressed={liked}
          aria-label={liked ? "取消点赞" : "点赞"}
          className={`inline-flex items-center gap-1.5 rounded-[6px] px-2.5 py-1.5 text-[0.84rem] transition-colors hover:bg-surface-2 ${
            liked ? "font-semibold text-seal" : "text-ink-2"
          } ${pending === "like" ? "opacity-70" : ""}`}
        >
          <HeartIcon filled={liked} />
          <span className="font-semibold">赞</span>
          <span className="font-mono text-[0.78rem] tabular-nums">{likeCount}</span>
        </button>

        {detail ? (
          <span className="inline-flex items-center gap-1.5 rounded-[6px] px-2.5 py-1.5 text-[0.84rem] text-ink-2">
            <CommentIcon />
            <span className="font-semibold">评论</span>
            <span className="font-mono text-[0.78rem] tabular-nums">{post.commentCount}</span>
          </span>
        ) : (
          <Link
            href={`${detailHref}#comments`}
            className="inline-flex items-center gap-1.5 rounded-[6px] px-2.5 py-1.5 text-[0.84rem] text-ink-2 hover:bg-surface-2"
          >
            <CommentIcon />
            <span className="font-semibold">评论</span>
            <span className="font-mono text-[0.78rem] tabular-nums">{post.commentCount}</span>
          </Link>
        )}

        <button
          type="button"
          onClick={startRepost}
          className="inline-flex items-center gap-1.5 rounded-[6px] px-2.5 py-1.5 text-[0.84rem] text-ink-2 transition-colors hover:bg-surface-2"
          aria-label={`转发这条帖子（已有 ${post.repostCount} 次转发）`}
        >
          <RepostIcon />
          <span className="font-semibold">转发</span>
          <span className="font-mono text-[0.78rem] tabular-nums">{post.repostCount}</span>
        </button>

        <button
          type="button"
          onClick={toggleBookmark}
          disabled={pending === "bookmark"}
          aria-pressed={bookmarked}
          aria-label={bookmarked ? "取消收藏" : "收藏"}
          className={`ml-auto inline-flex items-center gap-1.5 rounded-[6px] px-2.5 py-1.5 text-[0.84rem] transition-colors hover:bg-surface-2 ${
            bookmarked ? "font-semibold text-seal" : "text-ink-2"
          } ${pending === "bookmark" ? "opacity-70" : ""}`}
        >
          <BookmarkIcon filled={bookmarked} />
          <span className="font-semibold">{bookmarked ? "已收藏" : "收藏"}</span>
        </button>
      </div>

      {error && (
        <p
          role="alert"
          className="mt-2 flex items-start gap-1.5 rounded-[6px] border border-danger/40 bg-danger-wash px-2.5 py-1.5 text-[0.78rem] text-danger"
        >
          <span aria-hidden className="font-mono font-semibold">
            !
          </span>
          <span>{error}</span>
        </p>
      )}
    </article>
  );
}

/* -------------------------------------------------------------------------- */
/* 图标（内联 SVG，避免引入图标库）                                            */
/* -------------------------------------------------------------------------- */

function HeartIcon({ filled }: { filled: boolean }) {
  return (
    <svg
      viewBox="0 0 24 24"
      fill={filled ? "currentColor" : "none"}
      stroke="currentColor"
      strokeWidth="1.8"
      strokeLinejoin="round"
      className="size-4"
      aria-hidden
    >
      <path d="M12 20.3 4.7 13a4.7 4.7 0 0 1 6.6-6.6l.7.7.7-.7A4.7 4.7 0 0 1 19.3 13Z" />
    </svg>
  );
}

function CommentIcon() {
  return (
    <svg
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.8"
      strokeLinejoin="round"
      className="size-4"
      aria-hidden
    >
      <path d="M20 15a2.5 2.5 0 0 1-2.5 2.5H8L4 21V6.5A2.5 2.5 0 0 1 6.5 4h11A2.5 2.5 0 0 1 20 6.5Z" />
    </svg>
  );
}

function RepostIcon() {
  return (
    <svg
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.8"
      strokeLinecap="round"
      strokeLinejoin="round"
      className="size-4"
      aria-hidden
    >
      <path d="M17 2.5 20.5 6 17 9.5" />
      <path d="M3.5 11V9.5A3.5 3.5 0 0 1 7 6h13" />
      <path d="M7 21.5 3.5 18 7 14.5" />
      <path d="M20.5 13v1.5A3.5 3.5 0 0 1 17 18H4" />
    </svg>
  );
}

function BookmarkIcon({ filled }: { filled: boolean }) {
  return (
    <svg
      viewBox="0 0 24 24"
      fill={filled ? "currentColor" : "none"}
      stroke="currentColor"
      strokeWidth="1.8"
      strokeLinejoin="round"
      className="size-4"
      aria-hidden
    >
      <path d="M6 3.5h12a1 1 0 0 1 1 1V21l-7-4.5L5 21V4.5a1 1 0 0 1 1-1Z" />
    </svg>
  );
}
