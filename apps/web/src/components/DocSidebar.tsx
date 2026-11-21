"use client";

import { useCallback, useEffect, useState } from "react";
import {
  createSnapshot,
  listAnnotations,
  listSnapshots,
  restoreSnapshot,
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
  // 正在回滚的版本号；非 null 时禁用所有回滚按钮，避免并发触发。
  const [restoring, setRestoring] = useState<number | null>(null);
  // 回滚成功后的提示。不回滚提示「已生效」——必须刷新才能看到旧版本。
  const [restored, setRestored] = useState<{ from: number; to: number } | null>(null);

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

  /**
   * 回滚到指定版本。
   *
   * 二次确认是必要的：回滚会改变所有人看到的正文，而且协同服务内存里的
   * Y.Doc 不会自动重置，必须刷新页面才会加载旧版本——这两点都不能让
   * 用户「点一下才发现」。
   */
  async function handleRestore(version: number) {
    if (!window.confirm(`确定回滚到版本 ${version}？当前正文将被替换，且需要刷新页面才会生效。`)) {
      return;
    }
    setRestoring(version);
    setError(null);
    setRestored(null);
    try {
      const result = await restoreSnapshot(documentId, version);
      setRestored({ from: result.restoredFrom, to: result.newVersion });
      await load();
    } catch (err) {
      setError(err instanceof Error ? err.message : "回滚失败");
    } finally {
      setRestoring(null);
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
                  <div className="flex items-baseline justify-between gap-2">
                    <p className="text-[0.82rem] font-medium">
                      {s.label || `版本 ${s.version}`}
                    </p>
                    {canEdit && (
                      <button
                        type="button"
                        onClick={() => void handleRestore(s.version)}
                        disabled={restoring !== null}
                        className="shrink-0 rounded-[3px] px-1.5 py-0.5 text-[0.7rem] text-ink-3 hover:bg-seal-wash hover:text-seal disabled:opacity-50"
                      >
                        {restoring === s.version ? "回滚中…" : "回滚到此版本"}
                      </button>
                    )}
                  </div>
                  <p className="font-mono text-[0.68rem] text-ink-3">
                    {formatTime(s.createdAt)} · {formatBytes(s.sizeBytes)}
                  </p>
                </li>
              ))}
            </ul>
          )}

          {/*
            回滚成功提示：明确说「需要刷新」，而不是假装已经生效。
            协同服务（apps/ws）把 Y.Doc 放在内存里，REST 侧改快照指针
            不会影响已连接的会话，只有重新加载文档才会读到旧版本。
          */}
          {restored && (
            <div className="mt-3 rounded-[6px] border border-warn/40 bg-warn-wash px-3 py-2">
              <p className="text-[0.75rem] leading-relaxed text-warn">
                已标记回滚到版本 {restored.from}（生成新版本 {restored.to}）。
                协同编辑器仍持有回滚前的正文，请刷新页面以加载该版本。
              </p>
              <button
                type="button"
                onClick={() => window.location.reload()}
                className="mt-1.5 rounded-[3px] bg-warn px-2 py-0.5 text-[0.72rem] font-semibold text-white hover:opacity-90"
              >
                刷新页面
              </button>
            </div>
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
