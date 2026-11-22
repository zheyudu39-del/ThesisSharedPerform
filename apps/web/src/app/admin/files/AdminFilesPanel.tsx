"use client";

import { useCallback, useEffect, useState } from "react";
import { listAdminFiles, type AdminFile, type FileStat } from "@/lib/admin";
import { ApiError } from "@/lib/http";
import {
  AdminEmpty,
  AdminError,
  AdminPageHeader,
  AdminSkeleton,
  Badge,
  LoadMore,
  StatCard,
} from "@/components/AdminUI";

/**
 * 文件排查：上传量、状态分布、转码失败原因。
 *
 * 这是「用户说传不上去」时的第一落点：失败记录里有 error_reason
 * （缺 LibreOffice、文件损坏、超时），能直接区分「用户文件问题」
 * 还是「服务端依赖缺失」。
 */

const STATUS_LABEL: Record<string, string> = {
  pending: "待上传",
  uploading: "上传中",
  uploaded: "已上传",
  scanning: "扫描中",
  transcoding: "转码中",
  ready: "就绪",
  rejected: "已拒绝",
  failed: "失败",
};

const STATUS_TONE: Record<string, "neutral" | "good" | "warn" | "danger"> = {
  ready: "good",
  failed: "danger",
  rejected: "danger",
  transcoding: "warn",
  pending: "warn",
  uploading: "warn",
  scanning: "warn",
  uploaded: "neutral",
};

const FILTERS = [
  { value: "problem", label: "仅看问题" },
  { value: "", label: "全部" },
  { value: "failed", label: "失败" },
  { value: "rejected", label: "已拒绝" },
  { value: "transcoding", label: "转码中" },
  { value: "ready", label: "就绪" },
] as const;

export function AdminFilesPanel() {
  const [filter, setFilter] = useState<string>("problem");
  const [files, setFiles] = useState<AdminFile[]>([]);
  const [stats, setStats] = useState<FileStat[]>([]);
  const [cursor, setCursor] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const [loadingMore, setLoadingMore] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(
    async (next: string | null, append: boolean) => {
      if (append) setLoadingMore(true);
      else setLoading(true);
      setError(null);
      try {
        const page = await listAdminFiles({
          // "problem" 是「非 ready 且非正常结束」的集合，由服务端 onlyProblem 表达。
          onlyProblem: filter === "problem",
          status: filter !== "problem" && filter !== "" ? filter : undefined,
          cursor: next,
          limit: 20,
        });
        setFiles((prev) => (append ? [...prev, ...page.files] : page.files));
        setCursor(page.nextCursor);
        setStats(page.stats);
      } catch (err) {
        setError(err instanceof ApiError ? err.message : "文件列表加载失败，请稍后重试。");
      } finally {
        setLoading(false);
        setLoadingMore(false);
      }
    },
    [filter],
  );

  useEffect(() => {
    void load(null, false);
  }, [load]);

  const totalBytes = stats.reduce((sum, s) => sum + s.bytes, 0);
  const failedCount = stats.find((s) => s.status === "failed")?.fileCount ?? 0;

  return (
    <>
      <AdminPageHeader
        eyebrow="Files"
        title="文件排查"
        description="上传流水线的状态分布与失败原因。用户反馈「传不上去」时从这里查起。"
      />

      <div className="mb-4 grid grid-cols-2 gap-2.5 sm:grid-cols-4">
        <StatCard label="文件总数" value={stats.reduce((s, x) => s + x.fileCount, 0)} />
        <StatCard label="占用空间" value={formatBytes(totalBytes)} />
        <StatCard
          label="处理失败"
          value={failedCount}
          tone={failedCount > 0 ? "warn" : "good"}
        />
        <StatCard
          label="处理中"
          value={
            (stats.find((s) => s.status === "transcoding")?.fileCount ?? 0) +
            (stats.find((s) => s.status === "pending")?.fileCount ?? 0)
          }
        />
      </div>

      <div className="mb-4 flex flex-wrap items-center gap-1 rounded-[6px] border border-line-strong bg-surface p-0.5">
        {FILTERS.map((f) => (
          <button
            key={f.value}
            type="button"
            onClick={() => setFilter(f.value)}
            aria-pressed={filter === f.value}
            className={`rounded-[4px] px-2.5 py-1 font-mono text-[0.72rem] transition-colors ${
              filter === f.value ? "bg-ink text-paper" : "text-ink-2 hover:text-ink"
            }`}
          >
            {f.label}
          </button>
        ))}
      </div>

      {error && <AdminError message={error} onRetry={() => void load(null, false)} />}

      {loading ? (
        <AdminSkeleton rows={5} />
      ) : files.length === 0 ? (
        <AdminEmpty>
          {filter === "problem" ? "没有处理失败或卡住的文件。" : "没有匹配的文件。"}
        </AdminEmpty>
      ) : (
        <ul className="flex flex-col gap-2">
          {files.map((f) => (
            <li key={f.id} className="rounded-[10px] border border-line bg-surface px-3.5 py-3">
              <div className="mb-1 flex flex-wrap items-center gap-2">
                <span className="truncate text-[0.85rem] font-medium text-ink">
                  {f.originalName}
                </span>
                <Badge
                  label={STATUS_LABEL[f.status] ?? f.status}
                  tone={STATUS_TONE[f.status] ?? "neutral"}
                />
                <span className="font-mono text-[0.68rem] text-ink-3">{f.kind}</span>
                {f.retryCount > 0 && (
                  <span className="font-mono text-[0.68rem] text-warn">重试 {f.retryCount} 次</span>
                )}
                <span className="ml-auto font-mono text-[0.68rem] text-ink-3">
                  {formatDateTime(f.createdAt)}
                </span>
              </div>

              <div className="flex flex-wrap items-center gap-3">
                <span className="font-mono text-[0.68rem] text-ink-3">
                  {formatBytes(f.sizeBytes)}
                  {f.pageCount > 0 ? ` · ${f.pageCount} 页` : ""}
                </span>
                <span className="font-mono text-[0.68rem] text-ink-3">
                  上传者 @{f.ownerHandle}
                </span>
                <span className="truncate font-mono text-[0.66rem] text-ink-3">{f.mime}</span>
              </div>

              {f.errorReason && (
                <p className="mt-1.5 rounded-[4px] border border-danger/30 bg-danger-wash px-2.5 py-1.5 font-mono text-[0.7rem] leading-relaxed break-words text-danger">
                  {f.errorReason}
                </p>
              )}
            </li>
          ))}
        </ul>
      )}

      <LoadMore cursor={cursor} loading={loadingMore} onClick={() => void load(cursor, true)} />
    </>
  );
}

function formatBytes(bytes: number): string {
  if (!Number.isFinite(bytes) || bytes <= 0) return "0 B";
  const units = ["B", "KB", "MB", "GB", "TB"];
  let value = bytes;
  let unit = 0;
  while (value >= 1024 && unit < units.length - 1) {
    value /= 1024;
    unit++;
  }
  return `${value >= 100 || unit === 0 ? Math.round(value) : value.toFixed(1)} ${units[unit]}`;
}

function formatDateTime(iso: string): string {
  if (!iso) return "—";
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return "—";
  return d.toLocaleString("zh-CN", {
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
  });
}
