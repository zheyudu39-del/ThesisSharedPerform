import type { InputHTMLAttributes, ReactNode } from "react";

/**
 * 认证表单字段：标签 + 输入 + 字段级错误。
 *
 * 只负责「一个输入」的视觉与无障碍连线；表单状态、校验与提交由页面持有。
 * 错误用语义色（danger）呈现，并通过 aria-describedby / aria-invalid 暴露给读屏。
 */

type AuthFieldProps = {
  /** 输入框 id，同时用于把 <label> 绑定到输入。 */
  id: string;
  label: string;
  /** 标签右侧的等宽说明，例如「必填」。 */
  hint?: ReactNode;
  /** 字段级错误；存在时输入框边框转为语义色并显示提示。 */
  error?: string;
} & Omit<InputHTMLAttributes<HTMLInputElement>, "id" | "aria-invalid" | "aria-describedby">;

export function AuthField({ id, label, hint, error, className = "", ...inputProps }: AuthFieldProps) {
  const errorId = `${id}-error`;
  const hintId = `${id}-hint`;
  // 提示与错误同时存在时都关联，错误更新会被读屏再次播报。
  const describedBy =
    [hint ? hintId : null, error ? errorId : null].filter(Boolean).join(" ") || undefined;

  return (
    <div className="flex flex-col gap-1.5">
      <div className="flex items-baseline justify-between gap-3">
        <label htmlFor={id} className="text-[0.86rem] font-semibold text-ink">
          {label}
        </label>
        {hint && (
          <span id={hintId} className="font-mono text-[0.68rem] tracking-[0.08em] text-ink-3 uppercase">
            {hint}
          </span>
        )}
      </div>

      <input
        id={id}
        aria-invalid={error ? true : undefined}
        aria-describedby={describedBy}
        className={`w-full rounded-[6px] border bg-surface px-3 py-2.5 text-[0.95rem] text-ink placeholder:text-ink-3 transition-colors duration-150 focus:outline-none focus-visible:border-seal focus-visible:ring-2 focus-visible:ring-seal/35 disabled:cursor-not-allowed disabled:opacity-60 ${
          error ? "border-danger focus-visible:border-danger focus-visible:ring-danger/30" : "border-line-strong hover:border-ink-3"
        } ${className}`}
        {...inputProps}
      />

      {error && (
        <p id={errorId} role="alert" className="flex items-start gap-1.5 text-[0.78rem] font-medium text-danger">
          <span aria-hidden="true" className="mt-px font-mono leading-none">
            !
          </span>
          <span>{error}</span>
        </p>
      )}
    </div>
  );
}
