"use client";

import { useState, type FormEvent, type ReactNode } from "react";

/**
 * 后台共用的小组件：统计卡、区块标题、搜索框、状态徽章、空状态。
 *
 * 抽出来是因为四个管理页的列表骨架高度一致，重复写会让「改一处忘三处」
 * 变成常态（例如状态徽章的配色）。
 */

/** 页面标题区：等宽小标题 + 大标题 + 说明。 */
export function AdminPageHeader({
  eyebrow,
  title,
  description,
  actions,
}: {
  eyebrow: string;
  title: string;
  description?: string;
  actions?: ReactNode;
}) {
  return (
    <div className="mb-5 flex flex-wrap items-end justify-between gap-3 border-b border-line pb-4">
      <div>
        <p className="mb-1.5 font-mono text-[0.68rem] tracking-[0.16em] text-ink-3 uppercase">
          {eyebrow}
        </p>
        <h1 className="font-display text-[clamp(1.3rem,3vw,1.7rem)] leading-tight font-semibold tracking-[-0.02em]">
          {title}
        </h1>
        {description && (
          <p className="mt-1.5 max-w-[62ch] text-[0.86rem] leading-relaxed text-ink-2">
            {description}
          </p>
        )}
      </div>
      {actions && <div className="flex items-center gap-2">{actions}</div>}
    </div>
  );
}

/** 单个统计数字。 */
export function StatCard({
  label,
  value,
  hint,
  tone = "default",
}: {
  label: string;
  value: string | number;
  hint?: string;
  tone?: "default" | "warn" | "good";
}) {
  const valueTone =
    tone === "warn" ? "text-danger" : tone === "good" ? "text-good" : "text-ink";
  return (
    <div className="rounded-[10px] border border-line bg-surface px-3.5 py-3">
      <p className="font-mono text-[0.66rem] tracking-[0.08em] text-ink-3 uppercase">
        {label}
      </p>
      <p className={`mt-1 font-display text-[1.4rem] leading-none font-semibold tabular-nums ${valueTone}`}>
        {value}
      </p>
      {hint && <p className="mt-1.5 font-mono text-[0.66rem] text-ink-3">{hint}</p>}
    </div>
  );
}

/** 列表上方的搜索框；受控，回车提交。 */
export function AdminSearch({
  value,
  onChange,
  onSubmit,
  placeholder,
  children,
}: {
  value: string;
  onChange: (v: string) => void;
  onSubmit: () => void;
  placeholder: string;
  children?: ReactNode;
}) {
  function handleSubmit(e: FormEvent) {
    e.preventDefault();
    onSubmit();
  }
  return (
    <form onSubmit={handleSubmit} className="mb-4 flex flex-wrap items-center gap-2">
      <input
        value={value}
        onChange={(e) => onChange(e.target.value)}
        placeholder={placeholder}
        aria-label={placeholder}
        className="min-w-[200px] flex-1 rounded-[6px] border border-line-strong bg-surface px-3 py-2 text-[0.85rem] text-ink placeholder:text-ink-3 focus:border-seal focus:outline-none"
      />
      {children}
      <button
        type="submit"
        className="rounded-[6px] bg-ink px-4 py-2 text-[0.82rem] font-medium text-paper hover:bg-ink/90"
      >
        搜索
      </button>
    </form>
  );
}

/** 状态徽章。tone 决定配色，label 是展示文案。 */
export function Badge({
  label,
  tone = "neutral",
}: {
  label: string;
  tone?: "neutral" | "good" | "warn" | "danger" | "seal";
}) {
  const classes = {
    neutral: "border-line-strong bg-surface-2 text-ink-2",
    good: "border-good/40 bg-good-wash text-good",
    warn: "border-warn/40 bg-warn-wash text-warn",
    danger: "border-danger/40 bg-danger-wash text-danger",
    seal: "border-seal/40 bg-seal-wash text-seal-deep",
  }[tone];
  return (
    <span
      className={`inline-flex shrink-0 items-center rounded-full border px-2 py-0.5 font-mono text-[0.66rem] ${classes}`}
    >
      {label}
    </span>
  );
}

/** 空状态与加载骨架。 */
export function AdminEmpty({ children }: { children: ReactNode }) {
  return (
    <p className="rounded-[10px] border border-dashed border-line-strong bg-surface px-5 py-8 text-center text-[0.85rem] leading-relaxed text-ink-2">
      {children}
    </p>
  );
}

export function AdminSkeleton({ rows = 3 }: { rows?: number }) {
  return (
    <div className="flex flex-col gap-2" aria-hidden>
      {Array.from({ length: rows }, (_, i) => (
        <div key={i} className="h-16 animate-pulse rounded-[10px] border border-line bg-surface" />
      ))}
    </div>
  );
}

/** 错误提示条。 */
export function AdminError({ message, onRetry }: { message: string; onRetry?: () => void }) {
  return (
    <div role="alert" className="mb-4 rounded-[6px] border border-danger/40 bg-danger-wash px-3.5 py-2.5">
      <p className="text-[0.82rem] leading-relaxed text-danger">{message}</p>
      {onRetry && (
        <button
          type="button"
          onClick={onRetry}
          className="mt-1.5 text-[0.78rem] font-semibold text-danger underline"
        >
          重试
        </button>
      )}
    </div>
  );
}

/** 加载更多按钮。 */
export function LoadMore({
  cursor,
  loading,
  onClick,
}: {
  cursor: string | null;
  loading: boolean;
  onClick: () => void;
}) {
  if (!cursor) {
    return (
      <p className="py-3 text-center font-mono text-[0.68rem] tracking-[0.08em] text-ink-3 uppercase">
        已经到底了
      </p>
    );
  }
  return (
    <div className="py-3 text-center">
      <button
        type="button"
        onClick={onClick}
        disabled={loading}
        className="rounded-[6px] border border-line-strong px-4 py-1.5 text-[0.8rem] text-ink-2 hover:border-ink-3 hover:text-ink disabled:opacity-50"
      >
        {loading ? "加载中…" : "加载更多"}
      </button>
    </div>
  );
}

/** 简易折线/柱状趋势图（纯 SVG，不引第三方图表库）。 */
export function TrendChart({
  data,
  series,
}: {
  data: { day: string }[];
  series: { key: string; label: string; color: string; values: number[] }[];
}) {
  const [hover, setHover] = useState<number | null>(null);
  if (data.length === 0) return null;

  const width = 720;
  const height = 160;
  const padX = 8;
  const padY = 12;
  const max = Math.max(1, ...series.flatMap((s) => s.values));
  const stepX = data.length > 1 ? (width - padX * 2) / (data.length - 1) : 0;
  const scaleY = (v: number) => height - padY - (v / max) * (height - padY * 2);

  return (
    <div className="rounded-[10px] border border-line bg-surface p-3.5">
      <div className="mb-2 flex flex-wrap items-center gap-3">
        {series.map((s) => (
          <span key={s.key} className="flex items-center gap-1.5">
            <span className="size-2 rounded-full" style={{ backgroundColor: s.color }} />
            <span className="font-mono text-[0.68rem] text-ink-2">{s.label}</span>
          </span>
        ))}
        <span className="ml-auto font-mono text-[0.66rem] text-ink-3">
          {data[0]?.day} → {data[data.length - 1]?.day}
        </span>
      </div>

      <div className="overflow-x-auto">
        <svg
          viewBox={`0 0 ${width} ${height}`}
          className="h-[160px] w-full min-w-[520px]"
          role="img"
          aria-label="新增趋势图"
        >
          {/* 基线 */}
          <line x1={padX} y1={height - padY} x2={width - padX} y2={height - padY} stroke="currentColor" className="text-line" strokeWidth="1" />
          {series.map((s) => {
            const points = s.values
              .map((v, i) => `${padX + i * stepX},${scaleY(v)}`)
              .join(" ");
            return (
              <polyline
                key={s.key}
                points={points}
                fill="none"
                stroke={s.color}
                strokeWidth="1.8"
                strokeLinejoin="round"
                strokeLinecap="round"
              />
            );
          })}
          {/* 悬停指示 */}
          {hover !== null && (
            <line
              x1={padX + hover * stepX}
              y1={padY}
              x2={padX + hover * stepX}
              y2={height - padY}
              stroke="currentColor"
              className="text-line-strong"
              strokeDasharray="3 3"
            />
          )}
          {/* 透明的悬停热区 */}
          {data.map((_, i) => (
            <rect
              key={i}
              x={padX + i * stepX - stepX / 2}
              y={0}
              width={Math.max(stepX, 8)}
              height={height}
              fill="transparent"
              onMouseEnter={() => setHover(i)}
              onMouseLeave={() => setHover(null)}
            />
          ))}
        </svg>
      </div>

      {hover !== null && (
        <div className="mt-1 flex flex-wrap items-center gap-3 rounded-[6px] bg-surface-2 px-2.5 py-1.5">
          <span className="font-mono text-[0.7rem] font-semibold text-ink">{data[hover].day}</span>
          {series.map((s) => (
            <span key={s.key} className="font-mono text-[0.68rem] text-ink-2">
              {s.label} <span className="tabular-nums text-ink">{s.values[hover] ?? 0}</span>
            </span>
          ))}
        </div>
      )}
    </div>
  );
}
