"use client";

import Link from "next/link";
import { useEffect, useRef, useState, type FormEvent } from "react";
import { useAuth } from "@/components/AuthProvider";
import { CommentItem, groupComments } from "@/components/CommentThread";
import { AuthError } from "@/lib/auth";
import { createComment, getComments, type CommentDTO } from "@/lib/posts";

/**
 * 帖子详情页的评论区（客户端部分）。
 *
 * SSR 已经把首屏评论直出；本组件只负责后续交互：
 * - 发表评论（需要登录）；
 * - 「加载更多」按游标翻页；
 * - 新评论插入本地列表。
 *
 * 首屏评论由 Server Component 传给 {@link CommentThread}，这里不重复请求，
 * 除非用户点「加载更多」。
 */

const MAX_COMMENT = 2000;

/** 新评论产生的广播：让标题里的评论计数跟随更新，而不必把整页变客户端。 */
const COMMENT_EVENT = "thesis:comment-created";

/** 评论计数：初始值来自服务端，发表新评论后 +1。 */
export function CommentCount({ postId, initial }: { postId: string; initial: number }) {
  const [count, setCount] = useState(initial);

  useEffect(() => {
    function onCreated(event: Event) {
      const detail = (event as CustomEvent<{ postId: string }>).detail;
      if (detail?.postId === postId) setCount((c) => c + 1);
    }
    window.addEventListener(COMMENT_EVENT, onCreated);
    return () => window.removeEventListener(COMMENT_EVENT, onCreated);
  }, [postId]);

  return <>{count}</>;
}

export type PostCommentsProps = {
  postId: string;
  /** 首屏是否有下一页游标。 */
  initialNextCursor: string | null;
  /** 首屏评论是否成功获取；false 时展示「暂时无法加载」而不是「还没有评论」。 */
  commentsAvailable: boolean;
};

export function PostComments({
  postId,
  initialNextCursor,
  commentsAvailable,
}: PostCommentsProps) {
  const [extra, setExtra] = useState<CommentDTO[]>([]);
  const [cursor, setCursor] = useState<string | null>(initialNextCursor);
  const [moreLoading, setMoreLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const mounted = useRef(true);
  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
    };
  }, []);

  async function loadMore() {
    if (!cursor || moreLoading) return;
    setMoreLoading(true);
    setError(null);
    try {
      const page = await getComments(postId, cursor, 20);
      if (!mounted.current) return;
      setExtra((prev) => {
        const seen = new Set(prev.map((c) => c.id));
        return [...prev, ...page.comments.filter((c) => !seen.has(c.id))];
      });
      setCursor(page.nextCursor);
    } catch (err) {
      if (!mounted.current) return;
      setError(err instanceof AuthError ? err.message : "加载更多评论失败，请重试。");
    } finally {
      if (mounted.current) setMoreLoading(false);
    }
  }

  function handleCreated(comment: CommentDTO) {
    setExtra((prev) => (prev.some((c) => c.id === comment.id) ? prev : [...prev, comment]));
    window.dispatchEvent(
      new CustomEvent<{ postId: string }>(COMMENT_EVENT, { detail: { postId } }),
    );
  }

  // 「加载更多」拿到的评论需要并入首屏列表渲染，因此这里自带一份渲染；
  // 首屏评论由父级 Server Component 渲染（见 page.tsx 的 CommentThread）。
  const extraNodes = groupComments(extra);

  return (
    <>
      <CommentComposer postId={postId} onCreated={handleCreated} />

      {!commentsAvailable && (
        <p className="mt-4 rounded-[12px] border border-dashed border-line-strong bg-surface px-5 py-6 text-center text-[0.86rem] text-ink-2">
          评论暂时无法加载，请稍后刷新重试。
        </p>
      )}

      {extraNodes.length > 0 && (
        <ul className="mt-4 flex flex-col gap-3">
          {extraNodes.map((node) => (
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

      {cursor && (
        <div className="mt-4 flex justify-center">
          <button
            type="button"
            onClick={() => void loadMore()}
            disabled={moreLoading}
            aria-busy={moreLoading}
            className="inline-flex items-center gap-2 rounded-[6px] border border-line-strong bg-surface px-4 py-1.5 text-[0.82rem] font-semibold text-ink hover:border-ink-3 disabled:opacity-60"
          >
            {moreLoading && (
              <span
                aria-hidden
                className="size-3 animate-spin rounded-full border-2 border-ink-3/40 border-t-ink"
              />
            )}
            {moreLoading ? "加载中…" : "加载更多评论"}
          </button>
        </div>
      )}

      {error && (
        <p role="alert" className="mt-3 text-center text-[0.8rem] text-danger">
          {error}
        </p>
      )}
    </>
  );
}

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
