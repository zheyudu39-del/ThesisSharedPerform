"use client";

import { useCallback, useEffect, useState } from "react";
import {
  createSnapshot,
  listAnnotations,
  listSnapshots,
  updateAnnotation,
  type AnnotationDTO,
  type SnapshotDTO,
} from "@/lib/documents";

/**
 * 文档侧栏：版本快照与批注列表。
 *
 * 两者都是 REST 数据，不参与实时同步——快照是时间点副本，
 * 批注锚点在服务端用 Yjs RelativePosition 维护。
 */
export function DocSidebar({ documentId, canEdit }: { documentId: string; canEdit: boolean }) {
  const [tab, setTab] = useState<"snapshots" | "annotations">("snapshots");
  const [snapshots, setSnapshots] = useState<SnapshotDTO[]>([]);
  const [annotations, setAnnotations] = useState<AnnotationDTO[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [creating, setCreating] = useState(false);

  const load = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const [snap, ann] = await Promise.all([listSnapshots(documentId), listAnnotations(documentId)]);
      setSnapshots(snap.snapshots ?? []);
      setAnnotations(ann.annotations ?? []);
    } catch (err) {
      setError(err instanceof Error ? err.message : "加载失败");
    } finally {
      setLoading(false);
    }
  }, [documentId]);

  useEffect(() => {
    void load();
  }, [load]);

  async function handleSnapshot() {
    setCreating(true);
    setError(null);
    try {
      await createSnapshot(documentId);
      await load();
    } catch (err) {
      setError(err instanceof Error ? err.message : "创建快照失败");
    } finally {
      setCreating(false);
    }
  }

  async function toggleResolved(a: AnnotationDTO) {
    try {
      await updateAnnotation(documentId, a.id, { resolved: !a.resolvedAt });
      await load();
    } catch (err) {
      setError(err instanceof Error ? err.message : "更新批注失败");
    }
  }

  return (
    <aside className="flex flex-col gap-3">
      <div className="flex gap-1 rounded-[6px] border border-line bg-surface-2 p-1">
        <TabButton active={tab === "snapshots"} onClick={() => setTab("snapshots")}>
          快照
        </TabButton>
        <TabButton active={tab === "annotations"} onClick={() => setTab("annotations")}>
          批注
        </TabButton>
      </div>

      {error && (
        <p className="rounded-[6px] border border-danger/40 bg-danger-wash px-3 py-2 text-[0.75rem] text-danger">
          {error}
        </p>
      )}

      {tab === "snapshots" && (
        <section className="rounded-[12px] border border-line bg-surface p-3.5">
          <div className="mb-2.5 flex items-center justify-between">
            <h2 className="font-mono text-[0.68rem] tracking-[0.1em] text-ink-3 uppercase">版本历史</h2>
            {canEdit && (
              <button
                type="button"
                onClick={() => void handleSnapshot()}
                disabled={creating}
                className="rounded-[3px] px-2 py-0.5 text-[0.72rem] font-semibold text-seal hover:bg-seal-wash disabled:opacity-50"
              >
                {creating ? "创建中…" : "+ 快照"}
              </button>
            )}
          </div>
          {loading ? (
            <Skeleton />
          ) : snapshots.length === 0 ? (
            <p className="py-3 text-center text-[0.78rem] text-ink-3">还没有快照</p>
          ) : (
            <ul className="flex flex-col gap-2">
              {snapshots.map((s) => (
                <li key={s.version} className="border-b border-line pb-2 last:border-0 last:pb-0">
                  <p className="text-[0.82rem] font-medium">
                    {s.label || `版本 ${s.version}`}
                  </p>
                  <p className="font-mono text-[0.68rem] text-ink-3">
                    {formatTime(s.createdAt)} · {formatBytes(s.sizeBytes)}
                  </p>
                </li>
              ))}
            </ul>
          )}
        </section>
      )}

      {tab === "annotations" && (
        <section className="rounded-[12px] border border-line bg-surface p-3.5">
          <h2 className="mb-2.5 font-mono text-[0.68rem] tracking-[0.1em] text-ink-3 uppercase">
            批注 · {annotations.length}
          </h2>
          {loading ? (
            <Skeleton />
          ) : annotations.length === 0 ? (
            <p className="py-3 text-center text-[0.78rem] text-ink-3">还没有批注</p>
          ) : (
            <ul className="flex flex-col gap-2.5">
              {annotations.map((a) => (
                <li
                  key={a.id}
                  className={`border-b border-line pb-2.5 last:border-0 last:pb-0 ${
                    a.resolvedAt ? "opacity-55" : ""
                  }`}
                >
                  <p className="mb-1 text-[0.82rem] leading-relaxed">{a.content}</p>
                  {a.quotedText && (
                    <p className="mb-1 border-l-2 border-line-strong pl-2 text-[0.74rem] text-ink-3 italic">
                      {a.quotedText.slice(0, 60)}
                    </p>
                  )}
                  <div className="flex items-center gap-2 font-mono text-[0.66rem] text-ink-3">
                    <span>{a.authorName}</span>
                    <span aria-hidden>·</span>
                    <span>{formatTime(a.createdAt)}</span>
                    {canEdit && (
                      <button
                        type="button"
                        onClick={() => void toggleResolved(a)}
                        className="ml-auto text-ink-3 hover:text-seal"
                      >
                        {a.resolvedAt ? "重开" : "解决"}
                      </button>
                    )}
                  </div>
                </li>
              ))}
            </ul>
          )}
        </section>
      )}
    </aside>
  );
}

function TabButton({
  active,
  onClick,
  children,
}: {
  active: boolean;
  onClick: () => void;
  children: React.ReactNode;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      className={`flex-1 rounded-[3px] px-2 py-1 text-[0.78rem] ${
        active ? "bg-surface font-semibold text-ink shadow-[var(--shadow-1)]" : "text-ink-2"
      }`}
    >
      {children}
    </button>
  );
}

function Skeleton() {
  return (
    <div className="flex flex-col gap-2" aria-hidden>
      {[0, 1].map((i) => (
        <div key={i} className="h-10 animate-pulse rounded-[6px] bg-surface-2" />
      ))}
    </div>
  );
}

function formatTime(iso: string): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return "";
  return `${d.getMonth() + 1}/${d.getDate()} ${String(d.getHours()).padStart(2, "0")}:${String(
    d.getMinutes(),
  ).padStart(2, "0")}`;
}

function formatBytes(n: number): string {
  if (n < 1024) return `${n} B`;
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`;
  return `${(n / 1024 / 1024).toFixed(1)} MB`;
}
