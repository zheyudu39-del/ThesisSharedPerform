"use client";

import { useId, useRef, useState } from "react";
import { useAuth } from "@/components/AuthProvider";
import { AuthError } from "@/lib/auth";
import { resolvePaper } from "@/lib/papers";
import {
  createPost,
  VISIBILITY_LABEL,
  type CreatePostInput,
  type PostDTO,
  type PostVisibility,
} from "@/lib/posts";

/**
 * 发帖框：正文 + 可选论文 + 可见性 + 发布。
 *
 * - 仅登录用户可见（由父级根据登录态渲染）；
 * - 论文引用接受论文 UUID，也接受 DOI / arXiv 编号（自动解析为 UUID）；
 * - 未登录时父级会替换为登录引导，这里不再重复处理。
 */

const MAX_CONTENT = 5000;

const VISIBILITIES: PostVisibility[] = ["public", "followers", "private"];

export type PostComposerProps = {
  /** 发布成功后回调，父级可把新帖插到列表顶部。 */
  onCreated?: (post: PostDTO) => void;
  /** 预填正文（例如从「转发」入口带入）。 */
  initialContent?: string;
  /** 转发目标帖子 id：设置后本框发布的是转发帖，正文可留空。 */
  repostOf?: string;
  /** 转发目标的展示文案，用于提示用户正在转发什么。 */
  repostLabel?: string;
  /** 自动聚焦输入框（从转发入口打开时）。 */
  autoFocus?: boolean;
};

export function PostComposer({
  onCreated,
  initialContent = "",
  repostOf,
  repostLabel,
  autoFocus = false,
}: PostComposerProps) {
  const { user } = useAuth();
  const [content, setContent] = useState(initialContent);
  const [paperInput, setPaperInput] = useState("");
  const [visibility, setVisibility] = useState<PostVisibility>("public");
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);
  const textareaRef = useRef<HTMLTextAreaElement>(null);
  const fieldId = useId();

  const length = [...content].length;
  const tooLong = length > MAX_CONTENT;

  // 论文字段：空串表示不引用；合法 UUID 直接用，否则尝试按 DOI / arXiv 解析。
  async function resolvePaperInput(raw: string): Promise<string | null> {
    const value = raw.trim();
    if (!value) return null;
    if (UUID_PATTERN.test(value)) return value;

    const result = await resolvePaper(value);
    return result.paper.id;
  }

  async function handleSubmit(event: React.FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (submitting) return;

    const trimmed = content.trim();
    // 转发可以只有原帖、没有附加观点；原创帖必须有正文。
    if (!trimmed && !repostOf) {
      setError("请先写点什么再发布。");
      textareaRef.current?.focus();
      return;
    }
    if (tooLong) {
      setError(`正文最多 ${MAX_CONTENT} 个字符，当前 ${length} 个。`);
      return;
    }

    setSubmitting(true);
    setError(null);
    setNotice(null);

    try {
      const wantedPaper = paperInput.trim().length > 0;
      const paperId = await resolvePaperInput(paperInput);
      const input: CreatePostInput = {
        content: trimmed,
        visibility,
      };
      if (paperId) input.paperId = paperId;
      if (repostOf) input.repostOf = repostOf;

      const post = await createPost(input);
      setContent("");
      setPaperInput("");
      setVisibility("public");
      setNotice(
        repostOf
          ? "已转发。"
          : wantedPaper && !post.paper
            ? "已发布（论文未能关联）。"
            : "已发布。",
      );
      onCreated?.(post);
    } catch (err) {
      if (err instanceof AuthError) {
        setError(
          err.code === "paper_not_found"
            ? "找不到这篇论文，请检查 DOI / arXiv 编号。"
            : err.message,
        );
      } else {
        setError("发布失败，请稍后重试。");
      }
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <form
      onSubmit={handleSubmit}
      className="rounded-[12px] border border-line bg-surface p-4 shadow-[var(--shadow-1)]"
      aria-label="发布新帖"
    >
      <div className="mb-2.5 flex items-center gap-2.5">
        <span className="grid size-8 shrink-0 place-items-center rounded-full bg-ink font-display text-[13px] text-paper">
          {[...(user?.name ?? "?")][0] ?? "?"}
        </span>
        <p className="text-[0.88rem] text-ink-2">
          <span className="font-semibold text-ink">{user?.name ?? "你"}</span>
          <span className="ml-1.5 font-mono text-[0.72rem] text-ink-3">
            @{user?.handle ?? ""}
          </span>
        </p>
      </div>

      {repostOf && (
        <p className="mb-2 rounded-[6px] border border-line bg-surface-2 px-3 py-2 font-mono text-[0.74rem] tracking-[0.03em] text-ink-3">
          转发 {repostLabel ? <span className="text-ink-2">{repostLabel}</span> : "这条帖子"}
          <span className="ml-1">· 正文可留空</span>
        </p>
      )}

      <label htmlFor={`${fieldId}-content`} className="sr-only">
        帖子正文
      </label>
      <textarea
        id={`${fieldId}-content`}
        ref={textareaRef}
        value={content}
        autoFocus={autoFocus}
        onChange={(e) => {
          setContent(e.target.value);
          if (error) setError(null);
          if (notice) setNotice(null);
        }}
        rows={3}
        placeholder={repostOf ? "说说你为什么转发……" : "分享你正在读的论文，写下你的判断……"}
        className="w-full resize-y rounded-[6px] border border-line-strong bg-surface px-3.5 py-2.5 text-[0.92rem] leading-relaxed text-ink placeholder:text-ink-3 focus:border-seal focus:outline-none"
      />

      <div className="mt-1.5 flex items-center justify-between gap-3">
        <span
          className={`font-mono text-[0.7rem] tabular-nums ${
            tooLong ? "text-danger" : "text-ink-3"
          }`}
        >
          {length} / {MAX_CONTENT}
        </span>
        <span className="font-mono text-[0.68rem] text-ink-3">支持 Markdown</span>
      </div>

      <div className="mt-3 grid gap-2.5 sm:grid-cols-[minmax(0,1fr)_auto]">
        <div className="flex flex-col gap-1">
          <label
            htmlFor={`${fieldId}-paper`}
            className="font-mono text-[0.68rem] tracking-[0.08em] text-ink-3 uppercase"
          >
            引用论文（可选）
          </label>
          <input
            id={`${fieldId}-paper`}
            value={paperInput}
            onChange={(e) => setPaperInput(e.target.value)}
            placeholder="论文 ID，或 DOI / arXiv 编号"
            className="w-full rounded-[6px] border border-line-strong bg-surface px-3 py-2 font-mono text-[0.82rem] text-ink placeholder:text-ink-3 focus:border-seal focus:outline-none"
          />
        </div>

        <div className="flex flex-col gap-1">
          <label
            htmlFor={`${fieldId}-visibility`}
            className="font-mono text-[0.68rem] tracking-[0.08em] text-ink-3 uppercase"
          >
            可见性
          </label>
          <select
            id={`${fieldId}-visibility`}
            value={visibility}
            onChange={(e) => setVisibility(e.target.value as PostVisibility)}
            className="h-[38px] rounded-[6px] border border-line-strong bg-surface px-2.5 text-[0.85rem] text-ink focus:border-seal focus:outline-none"
          >
            {VISIBILITIES.map((v) => (
              <option key={v} value={v}>
                {VISIBILITY_LABEL[v]}
              </option>
            ))}
          </select>
        </div>
      </div>

      <div className="mt-3 flex flex-wrap items-center gap-2">
        <p className="mr-auto font-mono text-[0.68rem] text-ink-3">
          公开帖会进入全站流与热门流
        </p>
        <button
          type="submit"
          disabled={submitting || tooLong || (content.trim().length === 0 && !repostOf)}
          aria-busy={submitting}
          className="inline-flex items-center justify-center gap-2 rounded-[6px] bg-seal px-5 py-2 text-[0.88rem] font-semibold text-white shadow-[var(--shadow-1)] transition-colors hover:bg-seal-deep disabled:cursor-not-allowed disabled:opacity-50"
        >
          {submitting && (
            <span
              aria-hidden
              className="size-3.5 animate-spin rounded-full border-2 border-white/35 border-t-white"
            />
          )}
          {submitting ? (repostOf ? "转发中…" : "发布中…") : repostOf ? "转发" : "发布"}
        </button>
      </div>

      {error && (
        <p
          role="alert"
          className="mt-2.5 flex items-start gap-1.5 rounded-[6px] border border-danger/40 bg-danger-wash px-3 py-2 text-[0.82rem] leading-relaxed text-danger"
        >
          <span aria-hidden className="mt-0.5 font-mono font-semibold">
            !
          </span>
          <span>{error}</span>
        </p>
      )}

      {notice && (
        <p
          role="status"
          className="mt-2.5 rounded-[6px] border border-good/40 bg-good-wash px-3 py-2 text-[0.82rem] text-good"
        >
          {notice}
        </p>
      )}
    </form>
  );
}

const UUID_PATTERN = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;
