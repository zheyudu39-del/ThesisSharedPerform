"use client";

import Link from "next/link";
import { useCallback, useEffect, useState } from "react";
import {
  deleteAdminGroup,
  listAdminGroups,
  restoreAdminGroup,
  type AdminGroup,
} from "@/lib/admin";
import { ApiError } from "@/lib/http";
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
 * 群组管理：查看群组规模、删除与恢复。
 *
 * 删除群组会让群内帖子与文档一起从前台消失（读路径都带 deleted_at IS NULL），
 * 因此要求二次确认，并明确告知影响范围。
 */

const JOIN_POLICY_LABEL: Record<string, string> = {
  public: "公开加入",
  request: "需审核",
  invite: "仅邀请",
};

export function AdminGroupsPanel() {
  const [query, setQuery] = useState("");
  const [includeDeleted, setIncludeDeleted] = useState(false);
  const [groups, setGroups] = useState<AdminGroup[]>([]);
  const [cursor, setCursor] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const [loadingMore, setLoadingMore] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [actingId, setActingId] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);

  const load = useCallback(
    async (next: string | null, append: boolean) => {
      if (append) setLoadingMore(true);
      else setLoading(true);
      setError(null);
      try {
        const page = await listAdminGroups({
          q: query.trim() || undefined,
          cursor: next,
          limit: 20,
          includeDeleted,
        });
        setGroups((prev) => (append ? [...prev, ...page.groups] : page.groups));
        setCursor(page.nextCursor);
      } catch (err) {
        setError(err instanceof ApiError ? err.message : "群组列表加载失败，请稍后重试。");
      } finally {
        setLoading(false);
        setLoadingMore(false);
      }
    },
    [query, includeDeleted],
  );

  useEffect(() => {
    void load(null, false);
  }, [load]);

  async function handleDelete(group: AdminGroup) {
    if (actingId) return;
    const confirmed = window.confirm(
      `确定删除群组「${group.name}」？\n\n` +
        `该群的 ${group.memberCount} 名成员将无法访问，` +
        `${group.documentCount} 篇群文档与群内帖子会从前台隐藏。删除后可恢复。`,
    );
    if (!confirmed) return;

    setActingId(group.id);
    setError(null);
    setNotice(null);
    try {
      await deleteAdminGroup(group.id);
      const now = new Date().toISOString();
      setGroups((prev) =>
        prev.map((g) => (g.id === group.id ? { ...g, deletedAt: now } : g)),
      );
      setNotice(`群组「${group.name}」已删除（可恢复）。`);
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "删除失败，请稍后重试。");
    } finally {
      setActingId(null);
    }
  }

  async function handleRestore(group: AdminGroup) {
    if (actingId) return;
    setActingId(group.id);
    setError(null);
    setNotice(null);
    try {
      await restoreAdminGroup(group.id);
      setGroups((prev) =>
        prev.map((g) => (g.id === group.id ? { ...g, deletedAt: null } : g)),
      );
      setNotice(`群组「${group.name}」已恢复。`);
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "恢复失败，请稍后重试。");
    } finally {
      setActingId(null);
    }
  }

  return (
    <>
      <AdminPageHeader
        eyebrow="Groups"
        title="群组管理"
        description="删除群组会让群内帖子与文档一起从前台隐藏；删除为软删除，可恢复。"
      />

      <AdminSearch
        value={query}
        onChange={setQuery}
        onSubmit={() => void load(null, false)}
        placeholder="搜索群组名称或 slug"
      >
        <label className="flex items-center gap-1.5 rounded-[6px] border border-line-strong bg-surface px-2.5 py-2 text-[0.8rem] text-ink-2">
          <input
            type="checkbox"
            checked={includeDeleted}
            onChange={(e) => setIncludeDeleted(e.target.checked)}
            className="size-3.5 accent-[var(--color-seal)]"
          />
          显示已删除
        </label>
      </AdminSearch>

      {error && <AdminError message={error} onRetry={() => void load(null, false)} />}
      {notice && (
        <p role="status" className="mb-4 rounded-[6px] border border-good/40 bg-good-wash px-3.5 py-2.5 text-[0.82rem] text-good">
          {notice}
        </p>
      )}

      {loading ? (
        <AdminSkeleton rows={4} />
      ) : groups.length === 0 ? (
        <AdminEmpty>没有匹配的群组。</AdminEmpty>
      ) : (
        <ul className="flex flex-col gap-2">
          {groups.map((g) => {
            const deleted = g.deletedAt !== null;
            const busy = actingId === g.id;
            return (
              <li
                key={g.id}
                className={`rounded-[10px] border border-line bg-surface px-3.5 py-3 ${
                  deleted ? "opacity-60" : ""
                }`}
              >
                <div className="mb-1 flex flex-wrap items-center gap-2">
                  <Link
                    href={`/groups/${encodeURIComponent(g.slug)}`}
                    className="text-[0.88rem] font-medium text-ink hover:text-seal hover:underline"
                  >
                    {g.name}
                  </Link>
                  <span className="font-mono text-[0.7rem] text-ink-3">/{g.slug}</span>
                  <Badge label={JOIN_POLICY_LABEL[g.joinPolicy] ?? g.joinPolicy} />
                  {deleted && <Badge label="已删除" tone="danger" />}
                  <span className="ml-auto font-mono text-[0.68rem] text-ink-3">
                    {formatDate(g.createdAt)}
                  </span>
                </div>

                {g.description && (
                  <p className="mb-1.5 line-clamp-2 text-[0.82rem] leading-relaxed text-ink-2">
                    {g.description}
                  </p>
                )}

                <div className="flex flex-wrap items-center gap-3">
                  <span className="font-mono text-[0.68rem] text-ink-3 tabular-nums">
                    {g.memberCount} 成员 · {g.documentCount} 文档
                  </span>
                  <span className="font-mono text-[0.68rem] text-ink-3">
                    群主 @{g.ownerHandle}
                  </span>
                  <div className="ml-auto flex items-center gap-1.5">
                    <Link
                      href={`/groups/${encodeURIComponent(g.slug)}`}
                      className="rounded-[4px] border border-line-strong px-2 py-1 font-mono text-[0.68rem] text-ink-2 hover:border-ink-3 hover:text-ink"
                    >
                      查看
                    </Link>
                    {deleted ? (
                      <button
                        type="button"
                        disabled={busy}
                        onClick={() => void handleRestore(g)}
                        className="rounded-[4px] border border-good/40 px-2 py-1 font-mono text-[0.68rem] text-good hover:bg-good-wash disabled:opacity-40"
                      >
                        恢复
                      </button>
                    ) : (
                      <button
                        type="button"
                        disabled={busy}
                        onClick={() => void handleDelete(g)}
                        className="rounded-[4px] border border-danger/40 px-2 py-1 font-mono text-[0.68rem] text-danger hover:bg-danger-wash disabled:opacity-40"
                      >
                        删除
                      </button>
                    )}
                  </div>
                </div>
              </li>
            );
          })}
        </ul>
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
