"use client";

import { useEffect, useId, useRef, useState, type FormEvent } from "react";
import { ApiError } from "@/lib/http";
import {
  createReport,
  REPORT_DETAIL_MAX,
  REASON_LABEL,
  REPORT_REASONS,
  type ReportReason,
  type ReportTargetType,
} from "@/lib/moderation";

/**
 * 举报弹窗。
 *
 * 只负责「选原因 + 补充说明 + 提交」，不关心从哪个入口打开；
 * 帖子卡片的「⋯」菜单与个人主页都可以复用它。
 *
 * 用原生 <dialog> 而不是自绘遮罩：浏览器自带焦点陷阱与 Esc 关闭，
 * 键盘用户与屏幕阅读器的行为不需要额外实现。
 */
export type ReportDialogProps = {
  open: boolean;
  targetType: ReportTargetType;
  targetId: string;
  /** 关闭弹窗；提交成功后调用方通常也会关闭。 */
  onClose: () => void;
  /** 举报成功后的回调，便于调用方收起菜单或提示。 */
  onSubmitted?: () => void;
};

export function ReportDialog({
  open,
  targetType,
  targetId,
  onClose,
  onSubmitted,
}: ReportDialogProps) {
  const dialogRef = useRef<HTMLDialogElement>(null);
  const formId = useId();

  const [reason, setReason] = useState<ReportReason>("spam");
  const [detail, setDetail] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // open 由父级控制：命令式开关原生 dialog，避免把 open 属性写成受控状态
  // （原生 dialog 的 open 属性与 showModal() 的栈行为并不完全等价）。
  useEffect(() => {
    const dialog = dialogRef.current;
    if (!dialog) return;
    if (open && !dialog.open) {
      dialog.showModal();
    } else if (!open && dialog.open) {
      dialog.close();
    }
  }, [open]);

  // 每次打开都从干净状态开始，避免上一次的补充说明被误提交。
  useEffect(() => {
    if (!open) return;
    setReason("spam");
    setDetail("");
    setError(null);
  }, [open]);

  const detailLength = [...detail].length;
  const overLimit = detailLength > REPORT_DETAIL_MAX;

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (submitting || overLimit) return;

    setSubmitting(true);
    setError(null);
    try {
      await createReport({ targetType, targetId, reason, detail });
      onSubmitted?.();
      onClose();
    } catch (err) {
      // 409 / 422 是业务结果，直接展示后端文案（已举报 / 不能举报自己）。
      setError(err instanceof ApiError ? err.message : "提交失败，请稍后重试。");
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <dialog
      ref={dialogRef}
      aria-labelledby={`${formId}-title`}
      onClose={onClose}
      // 点击遮罩关闭：原生 dialog 的点击目标是 dialog 本身而不是内部容器。
      onClick={(event) => {
        if (event.target === dialogRef.current) onClose();
      }}
      // 原生 dialog 的 showModal() 自带焦点陷阱与 Esc 关闭，动画只加在视觉上。
      // backdrop 用单独的 keyframes：::backdrop 不继承 dialog 的 transform。
      className="anim-pop w-[min(92vw,28rem)] rounded-[12px] border border-line bg-surface p-0 text-ink shadow-[var(--shadow-1)] backdrop:animate-[ink-fade_200ms_var(--ease-ink)_both] backdrop:bg-black/40"
    >
      <form onSubmit={handleSubmit} className="flex flex-col gap-4 p-5">
        <div>
          <h2
            id={`${formId}-title`}
            className="font-display text-[1.05rem] font-semibold tracking-[-0.01em]"
          >
            举报这条内容
          </h2>
          <p className="mt-1 text-[0.82rem] leading-relaxed text-ink-3">
            举报会进入运营审核队列。恶意举报不会影响被举报人，但会记录在你的账号下。
          </p>
        </div>

        <fieldset className="flex flex-col gap-2">
          <legend className="mb-1 font-mono text-[0.72rem] tracking-[0.08em] text-ink-3 uppercase">
            原因
          </legend>
          {REPORT_REASONS.map((value) => (
            <label
              key={value}
              className={`flex cursor-pointer items-center gap-2.5 rounded-[6px] border px-3 py-2 text-[0.88rem] transition-colors ${
                reason === value
                  ? "border-seal bg-surface-2 text-ink"
                  : "border-line text-ink-2 hover:border-line-strong"
              }`}
            >
              <input
                type="radio"
                name={`${formId}-reason`}
                value={value}
                checked={reason === value}
                onChange={() => setReason(value)}
                disabled={submitting}
                className="accent-[var(--seal,#9a3f2f)]"
              />
              {REASON_LABEL[value]}
            </label>
          ))}
        </fieldset>

        <label className="flex flex-col gap-1.5">
          <span className="flex items-center justify-between text-[0.82rem] font-semibold">
            <span>补充说明（可选）</span>
            <span
              className={`font-mono text-[0.72rem] font-normal tabular-nums ${
                overLimit ? "text-danger" : "text-ink-3"
              }`}
            >
              {detailLength} / {REPORT_DETAIL_MAX}
            </span>
          </span>
          <textarea
            value={detail}
            onChange={(e) => setDetail(e.target.value)}
            rows={3}
            maxLength={REPORT_DETAIL_MAX + 50}
            disabled={submitting}
            placeholder="请描述具体问题，便于运营判断。"
            className="resize-y rounded-[6px] border border-line bg-paper px-3 py-2 text-[0.88rem] outline-none focus:border-seal"
          />
        </label>

        {error && (
          <p
            role="alert"
            className="rounded-[6px] border border-danger/40 bg-danger-wash px-2.5 py-1.5 text-[0.8rem] text-danger"
          >
            {error}
          </p>
        )}

        <div className="flex justify-end gap-2 pt-1">
          <button
            type="button"
            onClick={onClose}
            disabled={submitting}
            className="rounded-[6px] px-3.5 py-2 text-[0.86rem] text-ink-2 hover:bg-surface-2"
          >
            取消
          </button>
          <button
            type="submit"
            disabled={submitting || overLimit}
            className="rounded-[6px] bg-seal px-3.5 py-2 text-[0.86rem] font-semibold text-white hover:bg-seal-deep disabled:opacity-60"
          >
            {submitting ? "提交中…" : "提交举报"}
          </button>
        </div>
      </form>
    </dialog>
  );
}
