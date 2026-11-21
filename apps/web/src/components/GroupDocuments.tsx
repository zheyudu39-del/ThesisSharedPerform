"use client";

import Link from "next/link";
import { useCallback, useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import { createDocument, listGroupDocuments, type DocumentDTO } from "@/lib/documents";

/**
 * 群组文档列表。
 *
 * 文档是群组协作的产物（PRD D-01），只有成员能创建与查看。
 * 非成员不渲染本组件。
 */
export function GroupDocuments({ slug, isMember }: { slug: string; isMember: boolean }) {
  const router = useRouter();
  const [docs, setDocs] = useState<DocumentDTO[]>([]);
  const [cursor, setCursor] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const [creating, setCreating] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(
    async (next?: string | null) => {
      setLoading(true);
      setError(null);
      try {
        const page = await listGroupDocuments(slug, next);
        setDocs((prev) => (next ? [...prev, ...page.documents] : page.documents));
        setCursor(page.nextCursor);
      } catch (err) {
        setError(err instanceof Error ? err.message : "加载文档失败");
      } finally {
        setLoading(false);
      }
    },
    [slug],
  );

  useEffect(() => {
    if (isMember) void load(null);
  }, [isMember, load]);

  async function handleCreate() {
    setCreating(true);
    setError(null);
    try {
      const created = await createDocument(slug, "未命名文档");
      router.push(`/documents/${created.document.id}`);
    } catch (err) {
      setError(err instanceof Error ? err.message : "创建文档失败");
      setCreating(false);
    }
  }

  if (!isMember) {
    return (
      <p className="rounded-[12px] border border-dashed border-line-strong bg-surface px-5 py-10 text-center text-[0.88rem] leading-relaxed text-ink-2">
        加入群组后可以查看与创建协作文档。
      </p>
    );
  }

  return (
    <div className="flex flex-col gap-3">
      <div className="flex items-center justify-between">
        <p className="font-mono text-[0.7rem] text-ink-3">共 {docs.length} 篇文档</p>
        <button
          type="button"
          onClick={() => void handleCreate()}
          disabled={creating}
          className="rounded-[6px] bg-seal px-3.5 py-1.5 text-[0.82rem] font-semibold text-white hover:bg-seal-deep disabled:opacity-60"
        >
          {creating ? "创建中…" : "+ 新建文档"}
        </button>
      </div>

      {error && (
        <p className="rounded-[6px] border border-danger/40 bg-danger-wash px-3 py-2 text-[0.78rem] text-danger">
          {error}
        </p>
      )}

      {loading && docs.length === 0 ? (
        <div className="flex flex-col gap-2" aria-hidden>
          {[0, 1].map((i) => (
            <div key={i} className="h-20 animate-pulse rounded-[12px] bg-surface-2" />
          ))}
        </div>
      ) : docs.length === 0 ? (
        <p className="rounded-[12px] border border-dashed border-line-strong bg-surface px-5 py-10 text-center text-[0.88rem] text-ink-2">
          还没有文档。点「新建文档」开始协作。
        </p>
      ) : (
        <ul className="flex flex-col gap-2.5">
          {docs.map((d) => (
            <li key={d.id}>
              <Link
                href={`/documents/${d.id}`}
                className="block rounded-[12px] border border-line bg-surface p-4 transition-colors hover:border-line-strong"
              >
                <div className="flex items-baseline justify-between gap-3">
                  <h3 className="font-display text-[1rem] font-semibold">{d.title}</h3>
                  <span className="shrink-0 rounded-full bg-surface-2 px-2 py-0.5 font-mono text-[0.66rem] text-ink-3">
                    {STATUS_LABEL[d.status] ?? d.status}
                  </span>
                </div>
                <p className="mt-1 font-mono text-[0.68rem] text-ink-3">
                  {d.wordCount > 0 ? `${d.wordCount} 字 · ` : ""}
                  由 {d.createdBy.name} 创建
                </p>
              </Link>
            </li>
          ))}
        </ul>
      )}

      {cursor && (
        <button
          type="button"
          onClick={() => void load(cursor)}
          disabled={loading}
          className="mx-auto rounded-[6px] border border-line-strong px-4 py-1.5 text-[0.82rem] font-semibold text-ink hover:border-ink-3 disabled:opacity-60"
        >
          {loading ? "加载中…" : "加载更多"}
        </button>
      )}
    </div>
  );
}

const STATUS_LABEL: Record<string, string> = {
  draft: "草稿",
  active: "进行中",
  archived: "已归档",
};
