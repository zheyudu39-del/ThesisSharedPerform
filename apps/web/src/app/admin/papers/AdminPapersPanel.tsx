"use client";

import Link from "next/link";
import { Fragment, useCallback, useEffect, useState } from "react";
import { listAdminPapers, type AdminPaper } from "@/lib/admin";
import { ApiError } from "@/lib/http";
import { AdminPaperEditor } from "@/app/admin/papers/AdminPaperEditor";
import {
  AdminEmpty,
  AdminError,
  AdminPageHeader,
  AdminSearch,
  AdminSkeleton,
  Badge,
  LoadMore,
} from "@/components/AdminUI";

/**
 * 论文管理：查看论文库、元数据来源、全文挂载情况。
 *
 * 这里不提供删除：论文可能被大量帖子引用，删除会留下悬空 paper_id。
 * 需要合并重复论文时走论文详情页的「合并」功能（服务端会迁移关联）。
 * 后台的价值在于发现「抓取失败 / 没有全文 / 来源可疑」的条目。
 */

const SOURCE_LABEL: Record<string, string> = {
  crossref: "Crossref",
  arxiv: "arXiv",
  openreview: "OpenReview",
  manual: "手动补录",
};

export function AdminPapersPanel() {
  const [query, setQuery] = useState("");
  // 同时只展开一行编辑器：多行同时展开会让表格变得很长，也容易改错行。
  const [editingId, setEditingId] = useState<string | null>(null);
  const [papers, setPapers] = useState<AdminPaper[]>([]);
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
        const page = await listAdminPapers({
          q: query.trim() || undefined,
          cursor: next,
          limit: 20,
        });
        setPapers((prev) => (append ? [...prev, ...page.papers] : page.papers));
        setCursor(page.nextCursor);
      } catch (err) {
        setError(err instanceof ApiError ? err.message : "论文列表加载失败，请稍后重试。");
      } finally {
        setLoading(false);
        setLoadingMore(false);
      }
    },
    [query],
  );

  useEffect(() => {
    void load(null, false);
  }, [load]);

  const missingPdf = papers.filter((p) => !p.pdfFileId).length;

  return (
    <>
      <AdminPageHeader
        eyebrow="Papers"
        title="论文管理"
        description={
          papers.length > 0
            ? `当前页 ${papers.length} 篇，其中 ${missingPdf} 篇没有全文。后台不提供删除：论文可能被帖子引用，删除会留下悬空关联。`
            : "论文库中的条目、元数据来源与全文挂载情况。"
        }
      />

      <AdminSearch
        value={query}
        onChange={setQuery}
        onSubmit={() => void load(null, false)}
        placeholder="搜索论文标题"
      />

      {error && <AdminError message={error} onRetry={() => void load(null, false)} />}

      {loading ? (
        <AdminSkeleton rows={5} />
      ) : papers.length === 0 ? (
        <AdminEmpty>没有匹配的论文。</AdminEmpty>
      ) : (
        <div className="overflow-hidden rounded-[10px] border border-line bg-surface">
          <table className="w-full border-collapse text-left">
            <thead>
              <tr className="border-b border-line bg-surface-2">
                {["论文", "来源", "全文", "讨论", "入库时间", "操作"].map((h) => (
                  <th
                    key={h}
                    className="px-3 py-2 font-mono text-[0.66rem] font-normal tracking-[0.06em] text-ink-3 uppercase"
                  >
                    {h}
                  </th>
                ))}
              </tr>
            </thead>
            <tbody>
              {papers.map((p) => (
                <Fragment key={p.id}>
                <tr className="border-b border-line last:border-0 hover:bg-surface-2/60">
                  <td className="max-w-[420px] px-3 py-2.5">
                    <Link
                      href={`/papers/${p.id}`}
                      className="line-clamp-2 text-[0.85rem] font-medium text-ink hover:text-seal hover:underline"
                    >
                      {p.title}
                    </Link>
                    <p className="mt-0.5 font-mono text-[0.68rem] text-ink-3">
                      {p.authorCount} 位作者
                      {p.venue ? ` · ${p.venue}` : ""}
                      {p.publishedAt ? ` · ${p.publishedAt.slice(0, 4)}` : ""}
                      {p.citationCount > 0 ? ` · 引用 ${p.citationCount}` : ""}
                    </p>
                  </td>
                  <td className="px-3 py-2.5">
                    <Badge
                      label={SOURCE_LABEL[p.metaSource] ?? (p.metaSource || "未知")}
                      tone={p.metaSource === "manual" ? "warn" : "neutral"}
                    />
                  </td>
                  <td className="px-3 py-2.5">
                    {p.pdfFileId ? (
                      <Badge label="已挂载" tone="good" />
                    ) : (
                      <Badge label="缺失" tone="warn" />
                    )}
                  </td>
                  <td className="px-3 py-2.5 font-mono text-[0.72rem] text-ink-2 tabular-nums">
                    {p.postCount} 帖
                  </td>
                  <td className="px-3 py-2.5 font-mono text-[0.7rem] text-ink-3">
                    {formatDate(p.createdAt)}
                  </td>
                  <td className="px-3 py-2.5">
                    <button
                      type="button"
                      onClick={() => setEditingId(editingId === p.id ? null : p.id)}
                      aria-expanded={editingId === p.id}
                      className="rounded-[4px] border border-line-strong px-2 py-1 font-mono text-[0.68rem] text-ink-2 hover:border-ink-3 hover:text-ink"
                    >
                      {editingId === p.id ? "收起" : "编辑"}
                    </button>
                  </td>
                </tr>
                {editingId === p.id && (
                  <tr className="border-b border-line last:border-0">
                    <td colSpan={6} className="px-3 pb-3">
                      <AdminPaperEditor
                        paper={p}
                        onSaved={(patch) =>
                          setPapers((prev) =>
                            prev.map((x) => (x.id === p.id ? { ...x, ...patch } : x)),
                          )
                        }
                        onMerged={(removedId) => {
                          setPapers((prev) => prev.filter((x) => x.id !== removedId));
                          setEditingId(null);
                        }}
                        onClose={() => setEditingId(null)}
                      />
                    </td>
                  </tr>
                )}
                </Fragment>
              ))}
            </tbody>
          </table>
        </div>
      )}

      <LoadMore cursor={cursor} loading={loadingMore} onClick={() => void load(cursor, true)} />
    </>
  );
}

function formatDate(iso: string): string {
  if (!iso) return "—";
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return "—";
  return d.toLocaleDateString("zh-CN", { year: "numeric", month: "2-digit", day: "2-digit" });
}
