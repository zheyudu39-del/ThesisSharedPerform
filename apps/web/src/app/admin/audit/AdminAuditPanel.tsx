"use client";

import Link from "next/link";
import { useCallback, useEffect, useState } from "react";
import { listAdminAuditLogs, type AdminAuditLog } from "@/lib/admin";
import { ApiError } from "@/lib/http";
import {
  AdminEmpty,
  AdminError,
  AdminPageHeader,
  AdminSkeleton,
  Badge,
  LoadMore,
} from "@/components/AdminUI";

/**
 * 操作审计（A-05 要求「操作留审计日志」）。
 *
 * 记录的是运营对他人数据的写操作：改角色、封禁、认证、删帖删评论删群组。
 * 只读，没有删除入口——审计日志的价值就在于不可篡改，连管理员也不该能删。
 */

const ACTION_LABEL: Record<string, string> = {
  "user.role": "修改角色",
  "user.status": "封禁 / 解封",
  "user.verify": "修改认证",
  "post.delete": "删除帖子",
  "post.restore": "恢复帖子",
  "comment.delete": "删除评论",
  "comment.restore": "恢复评论",
  "group.delete": "删除群组",
  "group.restore": "恢复群组",
};

const ACTION_TONE: Record<string, "neutral" | "good" | "warn" | "danger" | "seal"> = {
  "user.role": "seal",
  "user.status": "danger",
  "user.verify": "seal",
  "post.delete": "danger",
  "post.restore": "good",
  "comment.delete": "danger",
  "comment.restore": "good",
  "group.delete": "danger",
  "group.restore": "good",
};

const FILTERS = [
  { value: "", label: "全部" },
  { value: "user.", label: "用户" },
  { value: "post.", label: "帖子" },
  { value: "comment.", label: "评论" },
  { value: "group.", label: "群组" },
] as const;

const DETAIL_LABEL: Record<string, string> = {
  before: "原值",
  after: "新值",
  org: "机构",
};

export function AdminAuditPanel() {
  const [filter, setFilter] = useState("");
  const [logs, setLogs] = useState<AdminAuditLog[]>([]);
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
        const page = await listAdminAuditLogs({
          action: filter || undefined,
          cursor: next,
          limit: 30,
        });
        setLogs((prev) => (append ? [...prev, ...page.logs] : page.logs));
        setCursor(page.nextCursor);
      } catch (err) {
        setError(err instanceof ApiError ? err.message : "审计日志加载失败，请稍后重试。");
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

  return (
    <>
      <AdminPageHeader
        eyebrow="Audit"
        title="操作审计"
        description="记录运营对他人数据的写操作。日志只读，不提供删除入口。"
      />

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
        <AdminSkeleton rows={6} />
      ) : logs.length === 0 ? (
        <AdminEmpty>还没有审计记录。运营进行封禁、认证、删除等操作后会出现在这里。</AdminEmpty>
      ) : (
        <ul className="flex flex-col gap-2">
          {logs.map((log, i) => (
            <li
              key={log.id}
              className="anim-rise rounded-[10px] border border-line bg-surface px-3.5 py-2.5"
              style={{ animationDelay: `${Math.min(i, 8) * 30}ms` }}
            >
              <div className="flex flex-wrap items-center gap-2">
                <Badge
                  label={ACTION_LABEL[log.action] ?? log.action}
                  tone={ACTION_TONE[log.action] ?? "neutral"}
                />
                <span className="font-mono text-[0.74rem] text-ink-2">
                  操作人 <span className="text-ink">{log.actorHandle}</span>
                </span>
                <span className="font-mono text-[0.74rem] text-ink-3">→</span>
                <span className="font-mono text-[0.74rem] text-ink-2">
                  {log.targetLabel ? (
                    log.targetType === "user" ? (
                      <Link
                        href={`/u/${encodeURIComponent(log.targetLabel)}`}
                        className="text-ink hover:text-seal hover:underline"
                      >
                        @{log.targetLabel}
                      </Link>
                    ) : (
                      <span className="text-ink">{log.targetLabel}</span>
                    )
                  ) : (
                    <span className="text-ink-3">{log.targetType}</span>
                  )}
                </span>
                <span className="ml-auto font-mono text-[0.68rem] text-ink-3">
                  {formatDateTime(log.createdAt)}
                </span>
              </div>

              {Object.keys(log.detail).length > 0 && (
                <div className="mt-1.5 flex flex-wrap items-center gap-3">
                  {Object.entries(log.detail).map(([key, value]) => {
                    const text = String(value ?? "");
                    if (!text) return null;
                    return (
                      <span key={key} className="font-mono text-[0.68rem] text-ink-3">
                        {DETAIL_LABEL[key] ?? key}:{" "}
                        <span className="text-ink-2">{displayValue(key, text)}</span>
                      </span>
                    );
                  })}
                  <span className="font-mono text-[0.62rem] text-ink-3 opacity-60">
                    {log.targetId.slice(0, 8)}
                  </span>
                </div>
              )}
            </li>
          ))}
        </ul>
      )}

      <LoadMore cursor={cursor} loading={loadingMore} onClick={() => void load(cursor, true)} />
    </>
  );
}

/** 把内部枚举值翻译成中文，避免运营看到 "influencer" 这种字面量。 */
function displayValue(key: string, value: string): string {
  if (key === "before" || key === "after") {
    const map: Record<string, string> = {
      none: "无",
      scholar: "学者",
      influencer: "大V",
      admin: "管理员",
      user: "普通用户",
      active: "正常",
      suspended: "已封禁",
    };
    return map[value] ?? value;
  }
  return value;
}

function formatDateTime(iso: string): string {
  if (!iso) return "—";
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return "—";
  return d.toLocaleString("zh-CN", {
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
  });
}
