"use client";

import Link from "next/link";
import { useCallback, useEffect, useState } from "react";
import { useAuth } from "@/components/AuthProvider";
import { ApiError } from "@/lib/http";
import {
  listReports,
  STATUS_LABEL,
  TARGET_TYPE_LABEL,
  REASON_LABEL,
  updateReportStatus,
  type ReportDTO,
  type ReportStatus,
} from "@/lib/moderation";

/**
 * 举报审核工作区。
 *
 * - 仅运营（role === "admin"）可用；非运营直接展示说明，不请求接口
 *   （即便请求了，服务端也会 403，这里的隐藏只是少一次无用请求）；
 * - 状态标签页对应后端的 status 筛选，默认只看「待处理」；
 * - 每条举报可以切到「处理中 / 已处理 / 已驳回」，处理人与时间由后端记录。
 */

/** 标签页顺序：待处理优先，是运营每天真正要清的队列。 */
const STATUS_TABS: { value: ReportStatus | ""; label: string }[] = [
  { value: "pending", label: "待处理" },
  { value: "reviewing", label: "处理中" },
  { value: "resolved", label: "已处理" },
  { value: "rejected", label: "已驳回" },
  { value: "", label: "全部" },
];

/** 处理动作：pending 是初始状态，不作为可选项。 */
const ACTIONS: { value: Exclude<ReportStatus, "pending">; label: string }[] = [
  { value: "reviewing", label: "开始处理" },
  { value: "resolved", label: "标记已处理" },
  { value: "rejected", label: "驳回" },
];

/** 把目标 ID 变成可点击链接：目前只有帖子有公开详情页。 */
function targetHref(report: ReportDTO): string | null {
  if (report.targetType === "post") return `/posts/${report.targetId}`;
  return null;
}

// 登录态与 403 判定由 app/admin/layout.tsx 的 AdminShell 统一处理，
// 这里不再重复包 RequireAuth——否则嵌套两层会让未登录时闪两次加载态。
export function AdminReportsWorkspace() {
  return <ReportsPanel />;
}

function ReportsPanel() {
  const { status, user } = useAuth();
  const isAdmin = status === "authenticated" && user?.role === "admin";

  const [tab, setTab] = useState<ReportStatus | "">("pending");
  const [reports, setReports] = useState<ReportDTO[]>([]);
  const [cursor, setCursor] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);
  const [loadingMore, setLoadingMore] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [actingId, setActingId] = useState<string | null>(null);

  const load = useCallback(
    async (next: string | null) => {
      if (next) setLoadingMore(true);
      else setLoading(true);
      setError(null);
      try {
        const page = await listReports(tab, next);
        setReports((prev) => (next ? [...prev, ...page.reports] : page.reports));
        setCursor(page.nextCursor);
      } catch (err) {
        setError(err instanceof ApiError ? err.message : "举报列表加载失败，请稍后重试。");
      } finally {
        setLoading(false);
        setLoadingMore(false);
      }
    },
    [tab],
  );

  useEffect(() => {
    if (!isAdmin) return;
    void load(null);
  }, [isAdmin, load]);

  async function act(report: ReportDTO, next: Exclude<ReportStatus, "pending">) {
    if (actingId) return;
    setActingId(report.id);
    setError(null);
    try {
      const updated = await updateReportStatus(report.id, next);
      // 在「待处理」标签页里处理完一条后，它不应继续留在队列中。
      setReports((prev) =>
        tab === "pending" && updated.status !== "pending"
          ? prev.filter((item) => item.id !== updated.id)
          : prev.map((item) => (item.id === updated.id ? updated : item)),
      );
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "操作失败，请稍后重试。");
    } finally {
      setActingId(null);
    }
  }

  if (status === "authenticated" && !isAdmin) {
    return (
      <main className="mx-auto w-full max-w-3xl px-5 py-16 text-center">
        <p className="mb-2 font-mono text-[0.72rem] tracking-[0.16em] text-ink-3 uppercase">
          403 · Forbidden
        </p>
        <h1 className="font-display text-[1.5rem] font-semibold">只有运营可以访问举报队列</h1>
        <p className="mt-3 text-[0.92rem] text-ink-2">
          举报包含举报人与被举报人的信息，不对普通用户开放。
        </p>
        <Link href="/" className="mt-6 inline-block text-[0.9rem] font-semibold text-seal hover:underline">
          返回首页
        </Link>
      </main>
    );
  }

  return (
    <main className="mx-auto w-full max-w-4xl px-5 py-10">
      <div className="mb-6 border-b border-line pb-6">
        <p className="mb-2 font-mono text-[0.72rem] tracking-[0.16em] text-ink-3 uppercase">
          运营 · Moderation
        </p>
        <h1 className="font-display text-[clamp(1.6rem,4vw,2.1rem)] leading-tight font-semibold tracking-[-0.02em]">
          举报审核队列
        </h1>
        <p className="mt-2 text-[0.9rem] leading-relaxed text-ink-2">
          按提交时间倒序。处理状态与处理人会被记录，便于复盘。
        </p>
      </div>

      <div role="tablist" aria-label="按状态筛选" className="mb-5 flex flex-wrap gap-1.5">
        {STATUS_TABS.map((item) => {
          const active = tab === item.value;
          return (
            <button
              key={item.value || "all"}
              type="button"
              role="tab"
              aria-selected={active}
              onClick={() => setTab(item.value)}
              className={`rounded-full px-3.5 py-1.5 text-[0.84rem] transition-colors ${
                active
                  ? "bg-ink font-semibold text-paper"
                  : "border border-line text-ink-2 hover:border-line-strong"
              }`}
            >
              {item.label}
            </button>
          );
        })}
      </div>

      {error && (
        <p
          role="alert"
          className="mb-4 rounded-[6px] border border-danger/40 bg-danger-wash px-3 py-2 text-[0.84rem] text-danger"
        >
          {error}
        </p>
      )}

      {loading ? (
        <p className="py-16 text-center font-mono text-[0.8rem] tracking-[0.08em] text-ink-3 uppercase">
          正在加载举报…
        </p>
      ) : reports.length === 0 ? (
        <div className="rounded-[10px] border border-dashed border-line px-6 py-16 text-center">
          <p className="text-[0.95rem] text-ink-2">这个状态下没有举报。</p>
          <p className="mt-1 text-[0.84rem] text-ink-3">队列清空时，说明社区目前是干净的。</p>
        </div>
      ) : (
        <ul className="flex flex-col gap-3">
          {reports.map((report) => (
            <ReportRow
              key={report.id}
              report={report}
              busy={actingId === report.id}
              onAct={(next) => act(report, next)}
            />
          ))}
        </ul>
      )}

      {cursor && !loading && (
        <div className="mt-6 flex justify-center">
          <button
            type="button"
            onClick={() => void load(cursor)}
            disabled={loadingMore}
            className="rounded-[6px] border border-line-strong px-4 py-2 text-[0.86rem] font-semibold text-ink hover:border-ink-3 disabled:opacity-60"
          >
            {loadingMore ? "加载中…" : "加载更多"}
          </button>
        </div>
      )}
    </main>
  );
}

function ReportRow({
  report,
  busy,
  onAct,
}: {
  report: ReportDTO;
  busy: boolean;
  onAct: (next: Exclude<ReportStatus, "pending">) => void;
}) {
  const href = targetHref(report);

  return (
    <li className="rounded-[10px] border border-line bg-surface p-4">
      <div className="mb-2.5 flex flex-wrap items-center gap-2 text-[0.8rem]">
        <span className="rounded-[4px] bg-surface-2 px-1.5 py-0.5 font-mono text-[0.7rem] tracking-[0.04em] text-ink-2">
          {TARGET_TYPE_LABEL[report.targetType]}
        </span>
        <span className="font-semibold text-ink">{REASON_LABEL[report.reason]}</span>
        <StatusBadge status={report.status} />
        <span className="ml-auto text-ink-3">{formatTime(report.createdAt)}</span>
      </div>

      <div className="mb-2 flex flex-wrap items-center gap-x-2 gap-y-1 text-[0.82rem] text-ink-2">
        <span>目标</span>
        {href ? (
          <Link href={href} className="font-mono text-seal hover:underline">
            {report.targetId}
          </Link>
        ) : (
          <span className="font-mono text-ink-3">{report.targetId}</span>
        )}
        {report.reporter && (
          <span className="text-ink-3">
            · 举报人{" "}
            <Link
              href={`/u/${encodeURIComponent(report.reporter.handle)}`}
              className="text-ink-2 hover:underline"
            >
              @{report.reporter.handle}
            </Link>
          </span>
        )}
      </div>

      {report.detail && (
        <p className="mb-3 rounded-[6px] bg-surface-2 px-3 py-2 text-[0.86rem] leading-relaxed break-words whitespace-pre-wrap text-ink-2">
          {report.detail}
        </p>
      )}

      <div className="flex flex-wrap items-center gap-2">
        {ACTIONS.filter((action) => action.value !== report.status).map((action) => (
          <button
            key={action.value}
            type="button"
            onClick={() => onAct(action.value)}
            disabled={busy}
            className={`rounded-[6px] border px-3 py-1.5 text-[0.8rem] font-semibold transition-colors disabled:opacity-60 ${
              action.value === "rejected"
                ? "border-line text-ink-2 hover:border-line-strong"
                : "border-seal/50 text-seal hover:bg-surface-2"
            }`}
          >
            {action.label}
          </button>
        ))}
        {report.handledBy && (
          <span className="ml-auto text-[0.76rem] text-ink-3">
            由 @{report.handledBy.handle} 处理
            {report.handledAt ? ` · ${formatTime(report.handledAt)}` : ""}
          </span>
        )}
      </div>
    </li>
  );
}

function StatusBadge({ status }: { status: ReportStatus }) {
  const tone =
    status === "pending"
      ? "bg-danger-wash text-danger"
      : status === "reviewing"
        ? "bg-surface-2 text-ink-2"
        : "bg-surface-2 text-ink-3";
  return (
    <span className={`rounded-full px-2 py-0.5 text-[0.72rem] font-semibold ${tone}`}>
      {STATUS_LABEL[status]}
    </span>
  );
}

/** 时间展示到分钟即可：审核不依赖秒级精度。 */
function formatTime(value: string): string {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return date.toLocaleString("zh-CN", {
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
  });
}
