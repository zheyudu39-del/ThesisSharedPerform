"use client";

import Link from "next/link";
import { useCallback, useEffect, useState } from "react";
import { getAdminOverview, type AdminOverview } from "@/lib/admin";
import { ApiError } from "@/lib/http";
import {
  AdminEmpty,
  AdminError,
  AdminPageHeader,
  AdminSkeleton,
  StatCard,
  TrendChart,
} from "@/components/AdminUI";

/**
 * 后台概览：总量、近两周新增趋势、热门话题。
 *
 * 一次请求拿回全部数据（服务端用一条 SQL 汇总总量），避免首屏发七个请求。
 */

const DAY_OPTIONS = [7, 14, 30] as const;

export function AdminOverviewPanel() {
  const [days, setDays] = useState<number>(14);
  const [data, setData] = useState<AdminOverview | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(async (d: number) => {
    setLoading(true);
    setError(null);
    try {
      setData(await getAdminOverview(d));
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "概览数据加载失败，请稍后重试。");
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void load(days);
  }, [load, days]);

  const totals = data?.totals;

  return (
    <>
      <AdminPageHeader
        eyebrow="Dashboard"
        title="数据概览"
        description="全站核心指标。所有统计口径均排除已注销账号与已删除内容。"
        actions={
          <div className="flex items-center gap-1 rounded-[6px] border border-line-strong bg-surface p-0.5">
            {DAY_OPTIONS.map((d) => (
              <button
                key={d}
                type="button"
                onClick={() => setDays(d)}
                aria-pressed={days === d}
                className={`rounded-[4px] px-2.5 py-1 font-mono text-[0.72rem] transition-colors ${
                  days === d ? "bg-ink text-paper" : "text-ink-2 hover:text-ink"
                }`}
              >
                {d} 天
              </button>
            ))}
          </div>
        }
      />

      {error && <AdminError message={error} onRetry={() => void load(days)} />}

      {loading && !data ? (
        <AdminSkeleton rows={4} />
      ) : (
        totals && (
          <>
            <section className="mb-5">
              <h2 className="mb-2 font-mono text-[0.68rem] tracking-[0.1em] text-ink-3 uppercase">
                用户
              </h2>
              <div className="grid grid-cols-2 gap-2.5 sm:grid-cols-3 lg:grid-cols-4">
                <StatCard label="注册用户" value={totals.users} hint={`活跃 ${totals.activeUsers}`} />
                <StatCard label="管理员" value={totals.admins} />
                <StatCard
                  label="待处理举报"
                  value={totals.pendingReports}
                  tone={totals.pendingReports > 0 ? "warn" : "good"}
                  hint={totals.pendingReports > 0 ? "需要处理" : "队列已清空"}
                />
                <StatCard label="屏蔽关系" value={totals.blocks} />
              </div>
            </section>

            <section className="mb-5">
              <h2 className="mb-2 font-mono text-[0.68rem] tracking-[0.1em] text-ink-3 uppercase">
                内容
              </h2>
              <div className="grid grid-cols-2 gap-2.5 sm:grid-cols-3 lg:grid-cols-4">
                <StatCard label="帖子" value={totals.posts} hint={`公开 ${totals.publicPosts}`} />
                <StatCard label="评论" value={totals.comments} />
                <StatCard
                  label="论文"
                  value={totals.papers}
                  hint={`有全文 ${totals.papersWithPdf}`}
                />
                <StatCard label="群组" value={totals.groups} hint={`文档 ${totals.documents}`} />
              </div>
            </section>

            <section className="mb-5">
              <h2 className="mb-2 font-mono text-[0.68rem] tracking-[0.1em] text-ink-3 uppercase">
                文件与存储
              </h2>
              <div className="grid grid-cols-2 gap-2.5 sm:grid-cols-3">
                <StatCard label="文件总数" value={totals.files} />
                <StatCard
                  label="处理失败"
                  value={totals.failedFiles}
                  tone={totals.failedFiles > 0 ? "warn" : "good"}
                  hint={totals.failedFiles > 0 ? "去文件页排查" : "无失败"}
                />
                <StatCard label="占用空间" value={formatBytes(totals.storageBytes)} />
              </div>
            </section>

            <section className="mb-5">
              <h2 className="mb-2 font-mono text-[0.68rem] tracking-[0.1em] text-ink-3 uppercase">
                近 {days} 天新增
              </h2>
              {data.activity.length === 0 ? (
                <AdminEmpty>暂无趋势数据。</AdminEmpty>
              ) : (
                <TrendChart
                  data={data.activity}
                  series={[
                    {
                      key: "users",
                      label: "用户",
                      color: "#b53a2e",
                      values: data.activity.map((d) => d.newUsers),
                    },
                    {
                      key: "posts",
                      label: "帖子",
                      color: "#525a6a",
                      values: data.activity.map((d) => d.newPosts),
                    },
                    {
                      key: "papers",
                      label: "论文",
                      color: "#8b919e",
                      values: data.activity.map((d) => d.newPapers),
                    },
                    {
                      key: "comments",
                      label: "评论",
                      color: "#d0cdc5",
                      values: data.activity.map((d) => d.newComments),
                    },
                  ]}
                />
              )}
            </section>

            <section>
              <div className="mb-2 flex items-baseline justify-between">
                <h2 className="font-mono text-[0.68rem] tracking-[0.1em] text-ink-3 uppercase">
                  热门话题
                </h2>
                <Link href="/tags" className="font-mono text-[0.7rem] text-ink-3 hover:text-seal">
                  前台查看 →
                </Link>
              </div>
              {data.topTags.length === 0 ? (
                <AdminEmpty>还没有带标签的公开帖子。</AdminEmpty>
              ) : (
                <ul className="flex flex-wrap gap-2">
                  {data.topTags.map((t) => (
                    <li key={t.slug}>
                      <Link
                        href={`/tags/${encodeURIComponent(t.slug)}`}
                        className="inline-flex items-baseline gap-1.5 rounded-full border border-line bg-surface px-3 py-1.5 text-[0.82rem] text-ink-2 hover:border-seal/50 hover:bg-seal-wash hover:text-seal"
                      >
                        <span className="font-mono text-seal">#</span>
                        {t.name}
                        <span className="font-mono text-[0.68rem] text-ink-3 tabular-nums">
                          {t.postCount}
                        </span>
                      </Link>
                    </li>
                  ))}
                </ul>
              )}
            </section>
          </>
        )
      )}
    </>
  );
}

/** 字节数格式化，与前台 files.ts 的口径一致。 */
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
