"use client";

import Link from "next/link";
import { usePathname, useRouter } from "next/navigation";
import { useEffect, useRef, useState } from "react";
import { useAuth } from "@/components/AuthProvider";
import { PaperPreviewCard } from "./PaperPreviewCard";
import { SaveToGroupFile } from "./SaveToGroupFile";
import { ReportDialog } from "./ReportDialog";
import { SealBadge } from "./SealBadge";
import { AuthError } from "@/lib/auth";
import {
  bookmarkPost,
  deletePost,
  formatRelativeTime,
  likePost,
  rememberPost,
  unbookmarkPost,
  unlikePost,
  updatePost,
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
  /** 帖子状态变化时回调，父级据此同步列表（点赞、收藏、评论数、编辑后的正文）。 */
  onPatch?: (next: PostDTO) => void;
  /**
   * 帖子被删除后回调，父级据此从列表移除。
   * 不并入 onPatch：删除是「这条帖子消失了」，而不是「它的字段变了」，
   * 用独立回调让父级能做删除专属的收尾（移除元素、跳转回列表等）。
   */
  onDeleted?: (id: string) => void;
  /** 详情页：不再引导跳转到自身详情。 */
  detail?: boolean;
  /**
   * 群组上下文。仅当帖子属于某个群时传入，用于给附件渲染
   * 「存入群文件库」入口——公开时间线里的帖子无处可存。
   */
  groupSlug?: string;
};

type PendingAction = "like" | "bookmark" | null;

export function PostCard({ post, onPatch, onDeleted, detail = false, groupSlug }: PostCardProps) {
  const { status, user } = useAuth();
  const router = useRouter();
  const pathname = usePathname();

  const [liked, setLiked] = useState(post.liked);
  const [likeCount, setLikeCount] = useState(post.likeCount);
  const [bookmarked, setBookmarked] = useState(post.bookmarked);
  const [pending, setPending] = useState<PendingAction>(null);
  const [error, setError] = useState<string | null>(null);

  // 编辑 / 删除的交互状态。
  // menuOpen：右上角「⋯」菜单；editing：就地展开的编辑框；
  // confirmingDelete：页面内的删除确认块（不使用 window.confirm）。
  const [menuOpen, setMenuOpen] = useState(false);
  const [editing, setEditing] = useState(false);
  const [draft, setDraft] = useState(post.contentText);
  const [confirmingDelete, setConfirmingDelete] = useState(false);
  const [mutating, setMutating] = useState(false);
  // 删除成功后本地隐藏。父级即便没有接 onDeleted（例如精选集里的帖子），
  // 卡片也会立即从视图消失，不必依赖父级重新拉取列表。
  const [deleted, setDeleted] = useState(false);
  // 编辑成功后的本地正文覆盖。父级没有传 onPatch（例如精选集里的帖子）时，
  // props.post 不会刷新；用一份本地覆盖保证编辑后卡片立即显示新正文。
  const [localContent, setLocalContent] = useState<{
    html: string;
    text: string;
  } | null>(null);
  // 举报弹窗。独立于「⋯」管理菜单：举报是任何登录用户都能做的动作，
  // 而管理菜单只对作者与运营可见，两者权限口径不同。
  const [reportOpen, setReportOpen] = useState(false);
  const [reportNotice, setReportNotice] = useState<string | null>(null);
  const menuRef = useRef<HTMLDivElement | null>(null);

  // 只有作者本人或运营（role === "admin"）能看到管理菜单。
  const canManage =
    status === "authenticated" &&
    user !== null &&
    (post.author.id === user.id || user.role === "admin");

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

  // 举报成功提示同理，短暂展示后自动收起。
  useEffect(() => {
    if (!reportNotice) return;
    const timer = window.setTimeout(() => setReportNotice(null), 5000);
    return () => window.clearTimeout(timer);
  }, [reportNotice]);

  // 帖子数据被父级刷新（例如详情页重新拉取）时，同步编辑草稿的初始值，
  // 避免下次打开编辑框仍显示旧正文。
  useEffect(() => {
    setDraft(post.contentText);
  }, [post.contentText]);

  // 父级带来了新的正文（props 变化）时，丢弃本地覆盖，以服务端数据为准。
  useEffect(() => {
    setLocalContent(null);
  }, [post.contentHtml, post.contentText]);

  // 菜单展开时监听外部点击 / Esc 关闭，交互习惯与 SiteHeader 的用户菜单一致。
  useEffect(() => {
    if (!menuOpen) return;
    function onPointerDown(event: MouseEvent | TouchEvent) {
      if (menuRef.current && !menuRef.current.contains(event.target as Node)) {
        setMenuOpen(false);
      }
    }
    function onKeyDown(event: KeyboardEvent) {
      if (event.key === "Escape") setMenuOpen(false);
    }
    document.addEventListener("mousedown", onPointerDown);
    document.addEventListener("touchstart", onPointerDown);
    document.addEventListener("keydown", onKeyDown);
    return () => {
      document.removeEventListener("mousedown", onPointerDown);
      document.removeEventListener("touchstart", onPointerDown);
      document.removeEventListener("keydown", onKeyDown);
    };
  }, [menuOpen]);

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

  /** 打开编辑态：用当前纯文本正文初始化草稿。 */
  function startEdit() {
    setMenuOpen(false);
    setConfirmingDelete(false);
    setDraft(post.contentText);
    setEditing(true);
    setError(null);
  }

  function cancelEdit() {
    setEditing(false);
    setDraft(post.contentText);
  }

  /** 保存编辑：成功后把服务端确认过的正文写回卡片与父级列表。 */
  async function saveEdit() {
    if (mutating) return;
    setMutating(true);
    setError(null);
    try {
      const updated = await updatePost(post.id, { content: draft });
      if (!mounted.current) return;
      setEditing(false);
      setLocalContent({ html: updated.contentHtml, text: updated.contentText });
      // 只回写服务端返回的正文相关字段；同时带上本地互动状态，
      // 避免 patch 用父级的旧 prop 覆盖掉用户刚点的赞 / 收藏。
      patch({
        contentHtml: updated.contentHtml,
        contentText: updated.contentText,
        visibility: updated.visibility,
        liked,
        likeCount,
        bookmarked,
      });
    } catch (err) {
      if (!mounted.current) return;
      setError(err instanceof AuthError ? err.message : "保存失败，请稍后重试。");
    } finally {
      if (mounted.current) setMutating(false);
    }
  }

  /** 删除：先展示页面内确认块，确认后才真正请求。 */
  async function confirmDelete() {
    if (mutating) return;
    setMutating(true);
    setError(null);
    try {
      await deletePost(post.id);
      if (!mounted.current) return;
      // 先本地隐藏，保证卡片立即消失；再通知父级从列表移除。
      setDeleted(true);
      onDeleted?.(post.id);
      // 详情页没有 onDeleted，删除后会停在一条已不存在的内容上，退回首页更合理。
      if (detail && !onDeleted) router.push("/");
    } catch (err) {
      if (!mounted.current) return;
      setConfirmingDelete(false);
      setError(err instanceof AuthError ? err.message : "删除失败，请稍后重试。");
    } finally {
      if (mounted.current) setMutating(false);
    }
  }

  const isRepost = post.type === "repost" && post.repostOf !== null;
  const initial = [...post.author.name][0] ?? "?";
  const detailHref = `/posts/${post.id}`;
  // 渲染用正文：优先本地编辑结果，其次 props。
  const displayHtml = localContent ? localContent.html : post.contentHtml;
  const displayText = localContent ? localContent.text : post.contentText;
  const hasContent = displayHtml.trim().length > 0 || displayText.trim().length > 0;

  // 删除成功后不再渲染，避免父级未接 onDeleted 时残留一张「幽灵卡片」。
  if (deleted) return null;

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

        {/* 管理菜单：仅作者本人或运营可见 */}
        {canManage && (
          <div className="relative shrink-0" ref={menuRef}>
            <button
              type="button"
              onClick={() => setMenuOpen((v) => !v)}
              aria-haspopup="menu"
              aria-expanded={menuOpen}
              aria-label="更多操作"
              className="grid size-7 place-items-center rounded-[6px] text-ink-3 transition-colors hover:bg-surface-2 hover:text-ink"
            >
              <MoreIcon />
            </button>
            {menuOpen && (
              <div
                role="menu"
                className="absolute top-[calc(100%+4px)] right-0 z-30 w-36 overflow-hidden rounded-[10px] border border-line bg-surface shadow-[var(--shadow-2)]"
              >
                <div className="flex flex-col p-1">
                  <button
                    type="button"
                    role="menuitem"
                    onClick={startEdit}
                    className="rounded-[6px] px-3 py-2 text-left text-sm text-ink-2 hover:bg-surface-2 hover:text-ink"
                  >
                    编辑
                  </button>
                  <button
                    type="button"
                    role="menuitem"
                    onClick={() => {
                      setMenuOpen(false);
                      setEditing(false);
                      setConfirmingDelete(true);
                    }}
                    className="rounded-[6px] px-3 py-2 text-left text-sm text-danger hover:bg-danger-wash"
                  >
                    删除
                  </button>
                </div>
              </div>
            )}
          </div>
        )}
      </div>

      {/* 删除确认块：页面内确认，不使用 window.confirm */}
      {confirmingDelete && (
        <div className="mb-3 rounded-[8px] border border-danger/40 bg-danger-wash px-3.5 py-3">
          <p className="mb-2.5 text-[0.84rem] leading-relaxed text-ink">
            确定删除这条帖子？删除后不可恢复{isRepost ? "，但不会影响被转发的原帖" : ""}。
          </p>
          <div className="flex items-center gap-2">
            <button
              type="button"
              onClick={confirmDelete}
              disabled={mutating}
              className="rounded-[6px] bg-danger px-3 py-1.5 text-[0.82rem] font-semibold text-white transition-opacity hover:opacity-90 disabled:opacity-60"
            >
              {mutating ? "删除中…" : "确认删除"}
            </button>
            <button
              type="button"
              onClick={() => setConfirmingDelete(false)}
              disabled={mutating}
              className="rounded-[6px] border border-line-strong px-3 py-1.5 text-[0.82rem] font-semibold text-ink hover:border-ink-3 disabled:opacity-60"
            >
              取消
            </button>
          </div>
        </div>
      )}

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
      {editing ? (
        <div className="mb-3.5">
          <textarea
            value={draft}
            onChange={(e) => setDraft(e.target.value)}
            rows={Math.min(12, Math.max(4, draft.split("\n").length + 1))}
            aria-label="编辑帖子正文"
            className="w-full resize-y rounded-[6px] border border-line-strong bg-paper px-3 py-2 text-[0.92rem] leading-relaxed text-ink outline-none focus:border-seal"
          />
          <div className="mt-2 flex items-center gap-2">
            <button
              type="button"
              onClick={saveEdit}
              disabled={mutating}
              className="rounded-[6px] bg-seal px-3.5 py-1.5 text-[0.82rem] font-semibold text-white transition-colors hover:bg-seal-deep disabled:opacity-60"
            >
              {mutating ? "保存中…" : "保存"}
            </button>
            <button
              type="button"
              onClick={cancelEdit}
              disabled={mutating}
              className="rounded-[6px] border border-line-strong px-3.5 py-1.5 text-[0.82rem] font-semibold text-ink hover:border-ink-3 disabled:opacity-60"
            >
              取消
            </button>
            <span className="ml-auto font-mono text-[0.72rem] text-ink-3 tabular-nums">
              {[...draft].length} / 5000
            </span>
          </div>
        </div>
      ) : (
        hasContent &&
        (displayHtml ? (
          <div
            className="mb-3.5 text-[0.92rem] leading-relaxed break-words [&_a]:text-seal [&_a]:underline [&_blockquote]:my-2 [&_blockquote]:border-l-2 [&_blockquote]:border-line-strong [&_blockquote]:pl-3 [&_blockquote]:text-ink-2 [&_code]:rounded-[3px] [&_code]:bg-surface-2 [&_code]:px-1 [&_code]:py-0.5 [&_code]:font-mono [&_code]:text-[0.84em] [&_h2]:mt-3 [&_h2]:mb-1.5 [&_h2]:font-display [&_h2]:text-[1.05rem] [&_h2]:font-semibold [&_h3]:mt-2.5 [&_h3]:mb-1 [&_h3]:font-display [&_h3]:font-semibold [&_li]:ml-4 [&_li]:list-disc [&_ol_li]:list-decimal [&_p]:mb-2 [&_p:last-child]:mb-0 [&_pre]:my-2 [&_pre]:overflow-x-auto [&_pre]:rounded-[6px] [&_pre]:bg-surface-2 [&_pre]:p-3 [&_pre]:font-mono [&_pre]:text-[0.8rem] [&_strong]:font-semibold"
            dangerouslySetInnerHTML={{ __html: displayHtml }}
          />
        ) : (
          <p className="mb-3.5 text-[0.92rem] leading-relaxed break-words whitespace-pre-wrap">
            {displayText}
          </p>
        ))
      )}

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

      {/* 附件：显示文件名与大小，并在群组上下文里提供「存入文件库」入口。 */}
      {post.attachments.length > 0 && (
        <div className="mb-3 flex flex-col gap-1.5">
          {post.attachments.map((file) => (
            <div
              key={file.fileId}
              className="flex flex-wrap items-center gap-2 rounded-[6px] border border-line bg-surface-2 px-3 py-1.5"
            >
              <span className="min-w-0 flex-1 truncate text-[0.82rem] text-ink-2">
                {file.name}
              </span>
              <span className="shrink-0 font-mono text-[0.68rem] text-ink-3">
                {formatBytes(file.sizeBytes)}
                {file.pageCount > 0 ? ` · ${file.pageCount} 页` : ""}
              </span>
              {/* 「存入文件库」只在群组页出现：公开时间线的帖子不属于任何群。 */}
              {groupSlug && (
                <SaveToGroupFile groupSlug={groupSlug} fileId={file.fileId} />
              )}
            </div>
          ))}
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

        {/* 举报：任何登录用户都可以举报他人的帖子（自举报由后端拒绝）。
            不放进「⋯」管理菜单，因为那个菜单只对作者与运营可见。 */}
        {status === "authenticated" && post.author.id !== user?.id && (
          <button
            type="button"
            onClick={() => setReportOpen(true)}
            aria-label="举报这条帖子"
            className="inline-flex items-center gap-1.5 rounded-[6px] px-2.5 py-1.5 text-[0.84rem] text-ink-3 transition-colors hover:bg-surface-2 hover:text-danger"
          >
            <FlagIcon />
            <span className="font-semibold">举报</span>
          </button>
        )}
      </div>

      <ReportDialog
        open={reportOpen}
        targetType="post"
        targetId={post.id}
        onClose={() => setReportOpen(false)}
        onSubmitted={() => setReportNotice("举报已提交，运营会尽快处理。")}
      />

      {/* 举报成功提示：与错误提示共用一条位置，但不复用 error 状态，
          避免把成功当失败展示成红色。 */}
      {reportNotice && (
        <p
          role="status"
          className="mt-2 rounded-[6px] border border-line bg-surface-2 px-2.5 py-1.5 text-[0.78rem] text-ink-2"
        >
          {reportNotice}
        </p>
      )}

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

function BookmarkIcon({ filled }: { filled: boolean }) {  return (
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

function MoreIcon() {
  return (
    <svg
      viewBox="0 0 24 24"
      fill="currentColor"
      stroke="none"
      className="size-4"
      aria-hidden
    >
      <circle cx="5" cy="12" r="1.6" />
      <circle cx="12" cy="12" r="1.6" />
      <circle cx="19" cy="12" r="1.6" />
    </svg>
  );
}

function FlagIcon() {
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
      <path d="M5 21V4" />
      <path d="M5 5h9.5l-1.4 3.2L14.5 12H5" />
    </svg>
  );
}

/** 人类可读的文件大小。 */
function formatBytes(bytes: number): string {
  if (!Number.isFinite(bytes) || bytes <= 0) return "0 B";
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  return `${(bytes / 1024 / 1024).toFixed(1)} MB`;
}
