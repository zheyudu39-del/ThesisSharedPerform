"use client";

import Link from "next/link";
import { useCallback, useEffect, useState } from "react";
import {
  listAdminUsers,
  setAdminUserRole,
  setAdminUserStatus,
  type AdminUser,
} from "@/lib/admin";
import { ApiError } from "@/lib/http";
import { useAuth } from "@/components/AuthProvider";
import { VerifyDialog } from "@/app/admin/users/VerifyDialog";
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
 * 用户管理：搜索、筛选、改角色、封禁 / 解封。
 *
 * 危险操作（降级管理员、封禁）都要求二次确认：后台误点一次就可能
 * 让某个用户立刻掉线，或让平台失去管理员。
 */

const ROLE_OPTIONS = [
  { value: "", label: "全部角色" },
  { value: "user", label: "普通用户" },
  { value: "admin", label: "管理员" },
] as const;

const STATUS_OPTIONS = [
  { value: "", label: "全部状态" },
  { value: "active", label: "正常" },
  { value: "suspended", label: "已封禁" },
] as const;

export function AdminUsersPanel() {
  const { user: me } = useAuth();
  const [query, setQuery] = useState("");
  const [role, setRole] = useState("");
  const [status, setStatus] = useState("");
  const [users, setUsers] = useState<AdminUser[]>([]);
  const [total, setTotal] = useState(0);
  const [cursor, setCursor] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const [loadingMore, setLoadingMore] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [actingId, setActingId] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);
  // 认证弹窗的目标用户；null 表示关闭。
  const [verifyTarget, setVerifyTarget] = useState<AdminUser | null>(null);

  const load = useCallback(
    async (next: string | null, append: boolean) => {
      if (append) setLoadingMore(true);
      else setLoading(true);
      setError(null);
      try {
        const page = await listAdminUsers({
          q: query.trim() || undefined,
          role: role || undefined,
          status: status || undefined,
          cursor: next,
          limit: 20,
        });
        setUsers((prev) => (append ? [...prev, ...page.users] : page.users));
        setCursor(page.nextCursor);
        setTotal(page.total);
      } catch (err) {
        setError(err instanceof ApiError ? err.message : "用户列表加载失败，请稍后重试。");
      } finally {
        setLoading(false);
        setLoadingMore(false);
      }
    },
    [query, role, status],
  );

  useEffect(() => {
    void load(null, false);
  }, [load]);

  async function changeRole(target: AdminUser, nextRole: "user" | "admin") {
    if (actingId) return;
    const verb = nextRole === "admin" ? "设为管理员" : "降为普通用户";
    if (!window.confirm(`确定把 @${target.handle} ${verb}？`)) return;

    setActingId(target.id);
    setError(null);
    setNotice(null);
    try {
      await setAdminUserRole(target.id, nextRole);
      setUsers((prev) =>
        prev.map((u) => (u.id === target.id ? { ...u, role: nextRole } : u)),
      );
      setNotice(`@${target.handle} 已${nextRole === "admin" ? "设为管理员" : "降为普通用户"}。`);
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "修改角色失败，请稍后重试。");
    } finally {
      setActingId(null);
    }
  }

  async function toggleStatus(target: AdminUser) {
    if (actingId) return;
    const nextStatus = target.status === "active" ? "suspended" : "active";
    const verb = nextStatus === "suspended" ? "封禁" : "解封";
    if (!window.confirm(`确定${verb} @${target.handle}？${nextStatus === "suspended" ? "该用户会立即掉线。" : ""}`)) {
      return;
    }

    setActingId(target.id);
    setError(null);
    setNotice(null);
    try {
      await setAdminUserStatus(target.id, nextStatus);
      setUsers((prev) =>
        prev.map((u) => (u.id === target.id ? { ...u, status: nextStatus } : u)),
      );
      setNotice(`@${target.handle} 已${verb}。`);
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "修改状态失败，请稍后重试。");
    } finally {
      setActingId(null);
    }
  }

  return (
    <>
      <AdminPageHeader
        eyebrow="Users"
        title="用户管理"
        description={`共 ${total} 个账号。封禁立即生效：该用户的下一次请求就会因会话失效而被拒绝。`}
      />

      <AdminSearch
        value={query}
        onChange={setQuery}
        onSubmit={() => void load(null, false)}
        placeholder="搜索句柄 / 姓名 / 邮箱"
      >
        <select
          value={role}
          onChange={(e) => setRole(e.target.value)}
          aria-label="按角色筛选"
          className="rounded-[6px] border border-line-strong bg-surface px-2.5 py-2 text-[0.82rem] text-ink focus:border-seal focus:outline-none"
        >
          {ROLE_OPTIONS.map((o) => (
            <option key={o.value} value={o.value}>
              {o.label}
            </option>
          ))}
        </select>
        <select
          value={status}
          onChange={(e) => setStatus(e.target.value)}
          aria-label="按状态筛选"
          className="rounded-[6px] border border-line-strong bg-surface px-2.5 py-2 text-[0.82rem] text-ink focus:border-seal focus:outline-none"
        >
          {STATUS_OPTIONS.map((o) => (
            <option key={o.value} value={o.value}>
              {o.label}
            </option>
          ))}
        </select>
      </AdminSearch>

      {error && <AdminError message={error} onRetry={() => void load(null, false)} />}
      {notice && (
        <p role="status" className="mb-4 rounded-[6px] border border-good/40 bg-good-wash px-3.5 py-2.5 text-[0.82rem] text-good">
          {notice}
        </p>
      )}

      {loading ? (
        <AdminSkeleton rows={5} />
      ) : users.length === 0 ? (
        <AdminEmpty>没有匹配的账号。</AdminEmpty>
      ) : (
        <div className="overflow-hidden rounded-[10px] border border-line bg-surface">
          <table className="w-full border-collapse text-left">
            <thead>
              <tr className="border-b border-line bg-surface-2">
                {["账号", "角色", "认证", "状态", "内容", "注册时间", "操作"].map((h) => (
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
              {users.map((u, i) => {
                const isSelf = me?.id === u.id;
                const busy = actingId === u.id;
                return (
                  <tr
                    key={u.id}
                    className="anim-rise border-b border-line last:border-0 hover:bg-surface-2/60"
                    style={{ animationDelay: `${Math.min(i, 8) * 30}ms` }}
                  >
                    <td className="px-3 py-2.5">
                      <Link
                        href={`/u/${encodeURIComponent(u.handle)}`}
                        className="text-[0.85rem] font-medium text-ink hover:text-seal hover:underline"
                      >
                        {u.name}
                      </Link>
                      <p className="font-mono text-[0.7rem] text-ink-3">@{u.handle}</p>
                      {u.email && (
                        <p className="font-mono text-[0.68rem] text-ink-3">{u.email}</p>
                      )}
                    </td>
                    <td className="px-3 py-2.5">
                      <Badge
                        label={u.role === "admin" ? "管理员" : "用户"}
                        tone={u.role === "admin" ? "seal" : "neutral"}
                      />
                    </td>
                    <td className="px-3 py-2.5">
                      {u.verifyType === "influencer" ? (
                        <div className="flex flex-col gap-0.5">
                          <Badge label="大V" tone="seal" />
                          {u.verifyOrg && (
                            <span className="font-mono text-[0.64rem] text-ink-3">{u.verifyOrg}</span>
                          )}
                        </div>
                      ) : u.verifyType === "scholar" ? (
                        <div className="flex flex-col gap-0.5">
                          <Badge label="学者" tone="good" />
                          {u.verifyOrg && (
                            <span className="font-mono text-[0.64rem] text-ink-3">{u.verifyOrg}</span>
                          )}
                        </div>
                      ) : (
                        <span className="font-mono text-[0.68rem] text-ink-3">—</span>
                      )}
                    </td>
                    <td className="px-3 py-2.5">
                      <Badge
                        label={u.status === "active" ? "正常" : "已封禁"}
                        tone={u.status === "active" ? "good" : "danger"}
                      />
                    </td>
                    <td className="px-3 py-2.5 font-mono text-[0.72rem] text-ink-2 tabular-nums">
                      {u.postCount} 帖 · {u.followerCount} 关注者
                    </td>
                    <td className="px-3 py-2.5 font-mono text-[0.7rem] text-ink-3">
                      {formatDate(u.createdAt)}
                    </td>
                    <td className="px-3 py-2.5">
                      <div className="flex flex-wrap items-center gap-1.5">
                        <button
                          type="button"
                          disabled={busy || isSelf}
                          onClick={() => void changeRole(u, u.role === "admin" ? "user" : "admin")}
                          title={isSelf ? "不能修改自己的角色" : undefined}
                          className="rounded-[4px] border border-line-strong px-2 py-1 font-mono text-[0.68rem] text-ink-2 hover:border-ink-3 hover:text-ink disabled:opacity-40"
                        >
                          {u.role === "admin" ? "降为普通" : "设为管理员"}
                        </button>
                        <button
                          type="button"
                          disabled={busy || isSelf}
                          onClick={() => void toggleStatus(u)}
                          title={isSelf ? "不能封禁自己" : undefined}
                          className={`rounded-[4px] border px-2 py-1 font-mono text-[0.68rem] disabled:opacity-40 ${
                            u.status === "active"
                              ? "border-danger/40 text-danger hover:bg-danger-wash"
                              : "border-good/40 text-good hover:bg-good-wash"
                          }`}
                        >
                          {u.status === "active" ? "封禁" : "解封"}
                        </button>
                        <button
                          type="button"
                          disabled={busy}
                          onClick={() => setVerifyTarget(u)}
                          className="rounded-[4px] border border-seal/40 px-2 py-1 font-mono text-[0.68rem] text-seal hover:bg-seal-wash disabled:opacity-40"
                        >
                          认证
                        </button>
                      </div>
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      )}

      <LoadMore cursor={cursor} loading={loadingMore} onClick={() => void load(cursor, true)} />

      {verifyTarget && (
        <VerifyDialog
          user={verifyTarget}
          onClose={() => setVerifyTarget(null)}
          onSaved={(verifyType, verifyOrg) => {
            setUsers((prev) =>
              prev.map((u) => (u.id === verifyTarget.id ? { ...u, verifyType, verifyOrg } : u)),
            );
            setNotice(`@${verifyTarget.handle} 的认证已更新。`);
          }}
        />
      )}
    </>
  );
}

function formatDate(iso: string): string {
  if (!iso) return "—";
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return "—";
  return d.toLocaleDateString("zh-CN", { year: "numeric", month: "2-digit", day: "2-digit" });
}
