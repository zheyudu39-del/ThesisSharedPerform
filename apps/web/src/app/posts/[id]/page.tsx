"use client";

import Link from "next/link";
import { useParams } from "next/navigation";
import { useCallback, useEffect, useRef, useState, type FormEvent } from "react";
import { useAuth } from "@/components/AuthProvider";
import { PostCard } from "@/components/PostCard";
import { SiteHeader } from "@/components/SiteHeader";
import { AuthError } from "@/lib/auth";
import {
  createComment,
  formatRelativeTime,
  getComments,
  getPost,
  recallPost,
  rememberPost,
  type CommentDTO,
  type PostDTO,
} from "@/lib/posts";

/**
 * 帖子详情页：正文（复用 PostCard，互动一致）+ 评论区。
 *
 * 评论区结构：一级评论平铺；带 parentId 的回复缩进显示在其父评论下方。
 * 匿名用户可浏览评论，但发表评论需要登录。
 */

const MAX_COMMENT = 2000;

type CommentNode = { comment: CommentDTO; replies: CommentDTO[] };

/** 把扁平评论列表组织成「一级 + 回复」两层结构（更深层回复归入其根评论）。 */
function groupComments(list: CommentDTO[]): CommentNode[] {
  const byId = new Map(list.map((c) => [c.id, c]));

  // 沿 parentId 向上找到根评论 id；缺失父评论或成环时把自身当作根。
  function rootIdOf(comment: CommentDTO): string {
    const visited = new Set<string>([comment.id]);
    let current = comment;
    while (current.parentId) {
      const parent = byId.get(current.parentId);
      if (!parent || visited.has(parent.id)) return current.id;
      visited.add(parent.id);
      current = parent;
    }
    return current.id;
  }

  const roots: CommentNode[] = [];
  const index = new Map<string, CommentNode>();

  for (const c of list) {
    if (rootIdOf(c) !== c.id) continue;
    const node: CommentNode = { comment: c, replies: [] };
    roots.push(node);
    index.set(c.id, node);
  }

  for (const c of list) {
    const root = index.get(rootIdOf(c));
    if (root && root.comment.id !== c.id) root.replies.push(c);
  }

  return roots;
}

export default function PostDetailPage() {
  const params = useParams<{ id: string }>();
  const postId = params.id;

  const [post, setPost] = useState<PostDTO | null>(null);
  const [postLoading, setPostLoading] = useState(true);
  const [postError, setPostError] = useState<{ message: string; notFound: boolean } | null>(null);

  const [comments, setComments] = useState<CommentDTO[]>([]);
  const [commentsCursor, setCommentsCursor] = useState<string | null>(null);
  const [commentsLoading, setCommentsLoading] = useState(true);
  const [commentsMoreLoading, setCommentsMoreLoading] = useState(false);
  const [commentsError, setCommentsError] = useState<string | null>(null);
  const [commentsAuthRequired, setCommentsAuthRequired] = useState(false);

  const requestId = useRef(0);

  const loadPost = useCallback(async () => {
    const id = ++requestId.current;
    setPostError(null);

    // 详情接口尚未上线：帖子来自信息流卡片的会话内快照（PostCard 写入）。
    const cached = recallPost(postId);
    if (cached) {
      setPost(cached);
      setPostLoading(false);
      // 快照已足够渲染；不额外请求，避免刷新链接时出现 404 干扰。
      return;
    }

    setPostLoading(true);
    try {
      const data = await getPost(postId);
      if (id !== requestId.current) return;
      setPost(data);
      setPostError(null);
    } catch (err) {
      if (id !== requestId.current) return;
      const notFound = err instanceof AuthError && err.status === 404;
      setPostError({
        notFound,
        message: notFound
          ? "这个帖子不在当前会话中，或已被删除。请从信息流点开帖子查看。"
          : err instanceof AuthError
            ? err.message
            : "帖子加载失败，请稍后重试。",
      });
    } finally {
      if (id === requestId.current) setPostLoading(false);
    }
  }, [postId]);

  function patchPost(next: PostDTO) {
    setPost(next);
    rememberPost(next);
  }

  const loadComments = useCallback(async () => {
    setCommentsLoading(true);
    setCommentsError(null);
    setCommentsAuthRequired(false);
    setComments([]);
    setCommentsCursor(null);
    try {
      const page = await getComments(postId, null, 20);
      setComments(page.comments);
      setCommentsCursor(page.nextCursor);
    } catch (err) {
      // 后端评论列表也要求登录：401 单独引导，不算「加载失败」。
      if (err instanceof AuthError && err.status === 401) {
        setCommentsAuthRequired(true);
      } else {
        setCommentsError(err instanceof AuthError ? err.message : "评论加载失败，请稍后重试。");
      }
    } finally {
      setCommentsLoading(false);
    }
  }, [postId]);

  useEffect(() => {
    void loadPost();
    void loadComments();
  }, [loadPost, loadComments]);

  async function loadMoreComments() {
    if (!commentsCursor || commentsMoreLoading || commentsLoading) return;
    setCommentsMoreLoading(true);
    setCommentsError(null);
    try {
      const page = await getComments(postId, commentsCursor, 20);
      setComments((prev) => {
        const seen = new Set(prev.map((c) => c.id));
        return [...prev, ...page.comments.filter((c) => !seen.has(c.id))];
      });
      setCommentsCursor(page.nextCursor);
    } catch (err) {
      setCommentsError(err instanceof AuthError ? err.message : "加载更多评论失败，请重试。");
    } finally {
      setCommentsMoreLoading(false);
    }
  }

  function handleCommentCreated(comment: CommentDTO) {
    setComments((prev) => (prev.some((c) => c.id === comment.id) ? prev : [...prev, comment]));
    setPost((prev) => {
      if (!prev) return prev;
      const next = { ...prev, commentCount: prev.commentCount + 1 };
      rememberPost(next);
      return next;
    });
  }

  return (
    <>
      <SiteHeader />

      <main className="mx-auto w-full max-w-3xl px-5 py-10">
        <Link
          href="/"
          className="mb-6 inline-flex items-center gap-1.5 text-[0.82rem] text-ink-2 hover:text-ink"
        >
          ← 返回信息流
        </Link>

        {postLoading && <PostSkeleton />}

        {!postLoading && postError && (
          <div className="rounded-[12px] border border-line bg-surface p-6 text-center">
            <p className="font-mono text-[0.68rem] tracking-[0.12em] text-danger uppercase">
              {postError.notFound ? "帖子不可用" : "加载失败"}
            </p>
            <p className="mt-2 text-[0.88rem] leading-relaxed text-ink-2">{postError.message}</p>
            {postError.notFound ? (
              <Link
                href="/"
                className="mt-4 inline-block rounded-[6px] border border-line-strong px-4 py-1.5 text-[0.85rem] font-semibold text-ink hover:border-ink-3"
              >
                返回信息流
              </Link>
            ) : (
              // 非 404 时才提供重试；404 是确定性结果，重试没有意义。

              <button
                type="button"
                onClick={() => void loadPost()}
                className="mt-4 rounded-[6px] border border-line-strong px-4 py-1.5 text-[0.85rem] font-semibold text-ink hover:border-ink-3"
              >
                重试
              </button>
            )}
          </div>
        )}

        {post && (
          <>
            <PostCard post={post} detail onPatch={patchPost} />

            <section id="comments" className="mt-8 scroll-mt-24">
              <h2 className="mb-4 flex items-baseline gap-2 border-b border-line pb-2.5">
                <span className="font-display text-[1.15rem] font-semibold">评论</span>
                <span className="font-mono text-[0.75rem] tabular-nums text-ink-3">
                  {post.commentCount}
                </span>
              </h2>

              <CommentComposer postId={post.id} onCreated={handleCommentCreated} />

              {commentsLoading && <CommentSkeleton />}

              {!commentsLoading && commentsAuthRequired && (
                <p className="mt-4 rounded-[12px] border border-dashed border-line-strong bg-surface px-5 py-6 text-center text-[0.86rem] text-ink-2">
                  <Link href="/login" className="font-semibold text-seal hover:underline">
                    登录
                  </Link>
                  后查看评论。
                </p>
              )}

              {!commentsLoading && !commentsAuthRequired && commentsError && !comments.length && (
                <div className="mt-4 rounded-[12px] border border-line bg-surface p-5 text-center">
                  <p className="text-[0.86rem] text-ink-2">{commentsError}</p>
                  <button
                    type="button"
                    onClick={() => void loadComments()}
                    className="mt-3 rounded-[6px] border border-line-strong px-4 py-1.5 text-[0.82rem] font-semibold text-ink hover:border-ink-3"
                  >
                    重试
                  </button>
                </div>
              )}

              {!commentsLoading &&
                !commentsAuthRequired &&
                !commentsError &&
                comments.length === 0 && (
                  <p className="mt-4 rounded-[12px] border border-dashed border-line-strong bg-surface px-5 py-8 text-center text-[0.86rem] text-ink-2">
                    还没有评论。写下你的第一条判断。
                  </p>
                )}

              {comments.length > 0 && (
                <ul className="mt-4 flex flex-col gap-3">
                  {groupComments(comments).map((node) => (
                    <li key={node.comment.id}>
                      <CommentItem comment={node.comment} />
                      {node.replies.length > 0 && (
                        <ul className="mt-2 ml-4 flex flex-col gap-2 border-l border-line pl-3.5">
                          {node.replies.map((reply) => (
                            <li key={reply.id}>
                              <CommentItem comment={reply} compact />
                            </li>
                          ))}
                        </ul>
                      )}
                    </li>
                  ))}
                </ul>
              )}

              {commentsCursor && (
                <div className="mt-4 flex justify-center">
                  <button
                    type="button"
                    onClick={() => void loadMoreComments()}
                    disabled={commentsMoreLoading}
                    aria-busy={commentsMoreLoading}
                    className="inline-flex items-center gap-2 rounded-[6px] border border-line-strong bg-surface px-4 py-1.5 text-[0.82rem] font-semibold text-ink hover:border-ink-3 disabled:opacity-60"
                  >
                    {commentsMoreLoading && (
                      <span
                        aria-hidden
                        className="size-3 animate-spin rounded-full border-2 border-ink-3/40 border-t-ink"
                      />
                    )}
                    {commentsMoreLoading ? "加载中…" : "加载更多评论"}
                  </button>
                </div>
              )}

              {commentsError && comments.length > 0 && (
                <p role="alert" className="mt-3 text-center text-[0.8rem] text-danger">
                  {commentsError}
                </p>
              )}
            </section>
          </>
        )}
      </main>
    </>
  );
}

/* -------------------------------------------------------------------------- */
/* 评论                                                                        */
/* -------------------------------------------------------------------------- */

function CommentComposer({
  postId,
  onCreated,
}: {
  postId: string;
  onCreated: (comment: CommentDTO) => void;
}) {
  const { status } = useAuth();
  const [content, setContent] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const length = [...content].length;
  const tooLong = length > MAX_COMMENT;

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (submitting) return;

    const trimmed = content.trim();
    if (!trimmed) {
      setError("评论不能为空。");
      return;
    }
    if (tooLong) {
      setError(`评论最多 ${MAX_COMMENT} 个字符。`);
      return;
    }

    setSubmitting(true);
    setError(null);
    try {
      const comment = await createComment(postId, { content: trimmed });
      setContent("");
      onCreated(comment);
    } catch (err) {
      setError(err instanceof AuthError ? err.message : "评论失败，请稍后重试。");
    } finally {
      setSubmitting(false);
    }
  }

  if (status === "loading") {
    return <div className="h-24 animate-pulse rounded-[12px] bg-surface-2" aria-hidden />;
  }

  if (status === "anonymous") {
    return (
      <div className="rounded-[12px] border border-dashed border-line-strong bg-surface px-4 py-4 text-center">
        <p className="text-[0.86rem] text-ink-2">
          <Link href="/login" className="font-semibold text-seal hover:underline">
            登录
          </Link>
          后参与讨论。
        </p>
      </div>
    );
  }

  return (
    <form
      onSubmit={handleSubmit}
      className="rounded-[12px] border border-line bg-surface p-3.5 shadow-[var(--shadow-1)]"
    >
      <label htmlFor="comment-content" className="sr-only">
        评论内容
      </label>
      <textarea
        id="comment-content"
        value={content}
        onChange={(e) => {
          setContent(e.target.value);
          if (error) setError(null);
        }}
        rows={3}
        placeholder="写下你的判断，或指出值得讨论的地方……"
        className="w-full resize-y rounded-[6px] border border-line-strong bg-surface px-3 py-2 text-[0.9rem] leading-relaxed text-ink placeholder:text-ink-3 focus:border-seal focus:outline-none"
      />

      <div className="mt-2 flex items-center gap-3">
        <span
          className={`font-mono text-[0.7rem] tabular-nums ${tooLong ? "text-danger" : "text-ink-3"}`}
        >
          {length} / {MAX_COMMENT}
        </span>
        <button
          type="submit"
          disabled={submitting || tooLong || content.trim().length === 0}
          aria-busy={submitting}
          className="ml-auto inline-flex items-center gap-2 rounded-[6px] bg-seal px-4 py-1.5 text-[0.85rem] font-semibold text-white transition-colors hover:bg-seal-deep disabled:cursor-not-allowed disabled:opacity-50"
        >
          {submitting && (
            <span
              aria-hidden
              className="size-3 animate-spin rounded-full border-2 border-white/35 border-t-white"
            />
          )}
          {submitting ? "发送中…" : "发表评论"}
        </button>
      </div>

      {error && (
        <p role="alert" className="mt-2 text-[0.8rem] text-danger">
          {error}
        </p>
      )}
    </form>
  );
}

function CommentItem({ comment, compact = false }: { comment: CommentDTO; compact?: boolean }) {
  const initial = [...comment.author.name][0] ?? "?";
  const hasHtml = comment.contentHtml.trim().length > 0;

  return (
    <article
      className={`rounded-[12px] border border-line bg-surface ${
        compact ? "px-3 py-2.5" : "p-3.5"
      } shadow-[var(--shadow-1)]`}
    >
      <div className="mb-1.5 flex items-center gap-2">
        <span
          className={`grid shrink-0 place-items-center rounded-full bg-ink font-display text-paper ${
            compact ? "size-5 text-[10px]" : "size-6 text-[11px]"
          }`}
          aria-hidden
        >
          {initial}
        </span>
        <Link
          href={`/u/${encodeURIComponent(comment.author.handle)}`}
          className="truncate text-[0.84rem] font-semibold hover:underline"
        >
          {comment.author.name}
        </Link>
        <span className="font-mono text-[0.7rem] text-ink-3">
          @{comment.author.handle}
        </span>
        <time
          dateTime={comment.createdAt}
          className="ml-auto shrink-0 font-mono text-[0.7rem] text-ink-3"
        >
          {formatRelativeTime(comment.createdAt)}
        </time>
      </div>

      {hasHtml ? (
        <div
          className="text-[0.88rem] leading-relaxed break-words text-ink-2 [&_a]:text-seal [&_a]:underline [&_code]:rounded-[3px] [&_code]:bg-surface-2 [&_code]:px-1 [&_code]:font-mono [&_p]:mb-1.5 [&_p:last-child]:mb-0"
          dangerouslySetInnerHTML={{ __html: comment.contentHtml }}
        />
      ) : (
        <p className="text-[0.88rem] leading-relaxed break-words whitespace-pre-wrap text-ink-2">
          {comment.contentText}
        </p>
      )}
    </article>
  );
}

/* -------------------------------------------------------------------------- */
/* 骨架                                                                        */
/* -------------------------------------------------------------------------- */

function PostSkeleton() {
  return (
    <div className="rounded-[12px] border border-line bg-surface p-4 shadow-[var(--shadow-1)]" aria-hidden>
      <div className="mb-3 flex items-center gap-2.5">
        <div className="size-[34px] animate-pulse rounded-full bg-surface-2" />
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
  );
}

function CommentSkeleton() {
  return (
    <div className="mt-4 flex flex-col gap-2.5" aria-hidden>
      {[0, 1, 2].map((i) => (
        <div key={i} className="rounded-[12px] border border-line bg-surface p-3.5">
          <div className="mb-2 h-2.5 w-24 animate-pulse rounded-[3px] bg-surface-2" />
          <div className="h-3 w-full animate-pulse rounded-[3px] bg-surface-2" />
        </div>
      ))}
    </div>
  );
}
