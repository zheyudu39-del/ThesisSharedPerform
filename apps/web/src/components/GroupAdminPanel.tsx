"use client";

import { useCallback, useEffect, useRef, useState } from "react";
import { useAuth } from "@/components/AuthProvider";
import { emitGroupMembership } from "@/components/GroupMembership";
import { AuthError } from "@/lib/auth";
import {
  approveRequest,
  createInvite,
  formatDateTime,
  GROUP_ROLE_LABEL,
  listJoinRequests,
  listMembers,
  rejectRequest,
  removeMember,
  updateMemberRole,
  type GroupInviteDTO,
  type GroupRole,
  type JoinRequestDTO,
  type MemberDTO,
} from "@/lib/groups";

/**
 * 群组管理面板（仅 owner / admin）。
 *
 * 三个分区：成员 / 待审批申请 / 邀请码。
 * - 成员：改角色、移除成员（owner 的角色不可改，由服务端 422 兜底）；
 * - 申请：批准 / 拒绝，操作成功后从列表移除；
 * - 邀请：生成一次性邀请码，展示可复制的一行。
 *
 * 数据按需加载：面板挂载时并发拉取成员与申请，避免一次性拖慢首屏。
 */

type Tab = "members" | "requests" | "invites";

export type GroupAdminPanelProps = {
  slug: string;
  /** 当前用户的角色（owner/admin）；owner 可改他人角色，admin 也可（后端裁定）。 */
  myRole: GroupRole;
  /** 服务端渲染时的成员数：移除 / 批准后据此广播新计数。 */
  memberCount: number;
};

export function GroupAdminPanel({ slug, myRole, memberCount }: GroupAdminPanelProps) {
  const { user } = useAuth();
  const myUserId = user?.id ?? "";

  // 成员数变化时广播给顶部的计数与成员预览，避免整页变客户端。
  const changeMemberCount = useCallback(
    (delta: number) => {
      emitGroupMembership({ slug, myRole, memberCount: Math.max(0, memberCount + delta) });
    },
    [slug, myRole, memberCount],
  );
  const [tab, setTab] = useState<Tab>("members");
  const [error, setError] = useState<string | null>(null);

  return (
    <section
      id="group-admin"
      className="scroll-mt-24 rounded-[12px] border border-line bg-surface shadow-[var(--shadow-1)]"
    >
      <div className="flex flex-wrap items-center gap-2 border-b border-line px-4 py-3">
        <h2 className="font-display text-[1.05rem] font-semibold">群组管理</h2>
        <span className="rounded-full bg-surface-2 px-2 py-0.5 font-mono text-[0.66rem] tracking-[0.04em] text-ink-2">
          {GROUP_ROLE_LABEL[myRole]}
        </span>
      </div>

      <div className="flex gap-1 border-b border-line px-2 py-1.5" role="tablist" aria-label="群组管理分区">
        <TabButton active={tab === "members"} onClick={() => setTab("members")}>
          成员
        </TabButton>
        <TabButton active={tab === "requests"} onClick={() => setTab("requests")}>
          入群申请
        </TabButton>
        <TabButton active={tab === "invites"} onClick={() => setTab("invites")}>
          邀请码
        </TabButton>
      </div>

      {error && (
        <p role="alert" className="border-b border-line bg-danger-wash px-4 py-2 text-[0.8rem] text-danger">
          {error}
        </p>
      )}

      <div className="p-4">
        {tab === "members" && (
          <MembersTab
            slug={slug}
            myRole={myRole}
            myUserId={myUserId}
            onError={setError}
            onMemberCountChange={changeMemberCount}
          />
        )}
        {tab === "requests" && (
          <RequestsTab slug={slug} onError={setError} onMemberCountChange={changeMemberCount} />
        )}
        {tab === "invites" && <InvitesTab slug={slug} onError={setError} />}
      </div>
    </section>
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
      role="tab"
      aria-selected={active}
      onClick={onClick}
      className={`rounded-[6px] px-3 py-1.5 text-[0.82rem] font-semibold transition-colors ${
        active ? "bg-surface-2 text-ink" : "text-ink-2 hover:bg-surface-2 hover:text-ink"
      }`}
    >
      {children}
    </button>
  );
}

/* -------------------------------------------------------------------------- */
/* 成员                                                                        */
/* -------------------------------------------------------------------------- */

function MembersTab({
  slug,
  myRole,
  myUserId,
  onError,
  onMemberCountChange,
}: {
  slug: string;
  myRole: GroupRole;
  myUserId: string;
  onError: (message: string | null) => void;
  onMemberCountChange?: (delta: number) => void;
}) {
  const [members, setMembers] = useState<MemberDTO[]>([]);
  const [nextCursor, setNextCursor] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const [loadingMore, setLoadingMore] = useState(false);
  const [pendingId, setPendingId] = useState<string | null>(null);

  const mounted = useRef(true);
  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
    };
  }, []);

  const load = useCallback(async () => {
    setLoading(true);
    onError(null);
    try {
      const page = await listMembers(slug, null, 20);
      if (!mounted.current) return;
      setMembers(page.members);
      setNextCursor(page.nextCursor);
    } catch (err) {
      if (!mounted.current) return;
      onError(err instanceof AuthError ? err.message : "成员列表加载失败。");
    } finally {
      if (mounted.current) setLoading(false);
    }
    // onError 由父级 setState 提供，引用稳定。
  }, [slug, onError]);

  useEffect(() => {
    void load();
  }, [load]);

  async function loadMore() {
    if (!nextCursor || loadingMore) return;
    setLoadingMore(true);
    try {
      const page = await listMembers(slug, nextCursor, 20);
      if (!mounted.current) return;
      setMembers((prev) => {
        const seen = new Set(prev.map((m) => m.userId));
        return [...prev, ...page.members.filter((m) => !seen.has(m.userId))];
      });
      setNextCursor(page.nextCursor);
    } catch (err) {
      if (!mounted.current) return;
      onError(err instanceof AuthError ? err.message : "加载更多成员失败。");
    } finally {
      if (mounted.current) setLoadingMore(false);
    }
  }

  async function changeRole(member: MemberDTO, role: "admin" | "member") {
    if (pendingId) return;
    setPendingId(member.userId);
    onError(null);
    try {
      const updated = await updateMemberRole(slug, member.userId, role);
      if (!mounted.current) return;
      setMembers((prev) => prev.map((m) => (m.userId === updated.userId ? updated : m)));
    } catch (err) {
      if (!mounted.current) return;
      onError(
        err instanceof AuthError && err.status === 422
          ? "不能修改群主的角色。"
          : err instanceof AuthError
            ? err.message
            : "修改角色失败。",
      );
    } finally {
      if (mounted.current) setPendingId(null);
    }
  }

  async function remove(member: MemberDTO) {
    if (pendingId) return;
    const label = member.userId === myUserId ? "退出群组" : `移除 ${member.name}`;
    if (!window.confirm(`${label}？此操作会立即生效。`)) return;

    setPendingId(member.userId);
    onError(null);
    try {
      await removeMember(slug, member.userId);
      if (!mounted.current) return;
      setMembers((prev) => prev.filter((m) => m.userId !== member.userId));
      onMemberCountChange?.(-1);
    } catch (err) {
      if (!mounted.current) return;
      onError(err instanceof AuthError ? err.message : "移除成员失败。");
    } finally {
      if (mounted.current) setPendingId(null);
    }
  }

  if (loading) return <ListSkeleton rows={3} />;

  if (members.length === 0) {
    return <Empty text="还没有成员。" />;
  }

  return (
    <div>
      <ul className="flex flex-col divide-y divide-line">
        {members.map((member) => {
          const isSelf = member.userId === myUserId;
          const isOwner = member.role === "owner";
          // 群主角色不可改；owner 只能被自己「退出」（实际上群主不能退出）。
          const canChangeRole = !isOwner && (myRole === "owner" || myRole === "admin");
          // owner/admin 可移除他人；本人可退出（群主不能退出）。
          const canRemove = !isOwner && (myRole === "owner" || myRole === "admin" || isSelf);

          return (
            <li key={member.userId} className="flex flex-wrap items-center gap-2 py-2.5">
              <div className="min-w-0 flex-1">
                <p className="flex items-center gap-1.5 truncate text-[0.88rem] font-semibold">
                  {member.name}
                  {isSelf && (
                    <span className="rounded-[3px] bg-surface-2 px-1.5 py-0.5 font-mono text-[0.62rem] font-normal text-ink-3">
                      我
                    </span>
                  )}
                </p>
                <p className="truncate font-mono text-[0.7rem] text-ink-3">@{member.handle}</p>
              </div>

              {canChangeRole ? (
                <select
                  value={member.role}
                  disabled={pendingId === member.userId}
                  onChange={(e) => changeRole(member, e.target.value === "admin" ? "admin" : "member")}
                  aria-label={`修改 ${member.name} 的角色`}
                  className="rounded-[6px] border border-line-strong bg-paper px-2 py-1 text-[0.78rem] text-ink focus:border-seal focus:outline-none disabled:opacity-60"
                >
                  <option value="member">成员</option>
                  <option value="admin">管理员</option>
                </select>
              ) : (
                <span className="rounded-full border border-line px-2 py-0.5 font-mono text-[0.66rem] text-ink-2">
                  {GROUP_ROLE_LABEL[member.role]}
                </span>
              )}

              {canRemove && (
                <button
                  type="button"
                  onClick={() => remove(member)}
                  disabled={pendingId === member.userId}
                  className="rounded-[6px] px-2 py-1 text-[0.78rem] text-danger transition-colors hover:bg-danger-wash disabled:opacity-60"
                >
                  {isSelf ? "退出" : "移除"}
                </button>
              )}
            </li>
          );
        })}
      </ul>

      {nextCursor && (
        <button
          type="button"
          onClick={loadMore}
          disabled={loadingMore}
          className="mt-3 w-full rounded-[6px] border border-line-strong py-2 text-[0.8rem] font-semibold text-ink hover:border-ink-3 disabled:opacity-60"
        >
          {loadingMore ? "加载中…" : "加载更多成员"}
        </button>
      )}
    </div>
  );
}

/* -------------------------------------------------------------------------- */
/* 入群申请                                                                    */
/* -------------------------------------------------------------------------- */

function RequestsTab({
  slug,
  onError,
  onMemberCountChange,
}: {
  slug: string;
  onError: (message: string | null) => void;
  onMemberCountChange?: (delta: number) => void;
}) {
  const [requests, setRequests] = useState<JoinRequestDTO[]>([]);
  const [loading, setLoading] = useState(true);
  const [pendingId, setPendingId] = useState<string | null>(null);

  const mounted = useRef(true);
  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
    };
  }, []);

  const load = useCallback(async () => {
    setLoading(true);
    onError(null);
    try {
      const list = await listJoinRequests(slug);
      if (!mounted.current) return;
      setRequests(list);
    } catch (err) {
      if (!mounted.current) return;
      onError(err instanceof AuthError ? err.message : "入群申请加载失败。");
    } finally {
      if (mounted.current) setLoading(false);
    }
  }, [slug, onError]);

  useEffect(() => {
    void load();
  }, [load]);

  async function review(request: JoinRequestDTO, action: "approve" | "reject") {
    if (pendingId) return;
    setPendingId(request.id);
    onError(null);
    try {
      if (action === "approve") await approveRequest(slug, request.id);
      else await rejectRequest(slug, request.id);
      if (!mounted.current) return;
      setRequests((prev) => prev.filter((r) => r.id !== request.id));
      if (action === "approve") onMemberCountChange?.(1);
    } catch (err) {
      if (!mounted.current) return;
      onError(err instanceof AuthError ? err.message : "操作失败，请稍后重试。");
    } finally {
      if (mounted.current) setPendingId(null);
    }
  }

  if (loading) return <ListSkeleton rows={2} />;
  if (requests.length === 0) return <Empty text="没有待审批的入群申请。" />;

  return (
    <ul className="flex flex-col divide-y divide-line">
      {requests.map((request) => (
        <li key={request.id} className="py-3">
          <div className="flex flex-wrap items-start justify-between gap-2">
            <div className="min-w-0">
              <p className="truncate text-[0.88rem] font-semibold">
                {request.user.name}
                <span className="ml-1.5 font-mono text-[0.72rem] font-normal text-ink-3">
                  @{request.user.handle}
                </span>
              </p>
              <p className="font-mono text-[0.68rem] text-ink-3">{formatDateTime(request.createdAt)}</p>
            </div>
            <div className="flex items-center gap-1.5">
              <button
                type="button"
                onClick={() => review(request, "approve")}
                disabled={pendingId === request.id}
                className="rounded-[6px] bg-good px-2.5 py-1 text-[0.78rem] font-semibold text-white hover:opacity-90 disabled:opacity-60"
              >
                批准
              </button>
              <button
                type="button"
                onClick={() => review(request, "reject")}
                disabled={pendingId === request.id}
                className="rounded-[6px] border border-line-strong px-2.5 py-1 text-[0.78rem] font-semibold text-ink-2 hover:border-danger/60 hover:text-danger disabled:opacity-60"
              >
                拒绝
              </button>
            </div>
          </div>
          {request.message && (
            <p className="mt-2 rounded-[6px] bg-surface-2 px-3 py-2 text-[0.82rem] leading-relaxed whitespace-pre-wrap text-ink-2">
              {request.message}
            </p>
          )}
        </li>
      ))}
    </ul>
  );
}

/* -------------------------------------------------------------------------- */
/* 邀请码                                                                      */
/* -------------------------------------------------------------------------- */

function InvitesTab({
  slug,
  onError,
}: {
  slug: string;
  onError: (message: string | null) => void;
}) {
  const [invite, setInvite] = useState<GroupInviteDTO | null>(null);
  const [creating, setCreating] = useState(false);
  const [copied, setCopied] = useState(false);

  async function generate() {
    if (creating) return;
    setCreating(true);
    onError(null);
    setCopied(false);
    try {
      const next = await createInvite(slug);
      setInvite(next);
    } catch (err) {
      onError(err instanceof AuthError ? err.message : "生成邀请码失败。");
    } finally {
      setCreating(false);
    }
  }

  async function copy() {
    if (!invite) return;
    try {
      await navigator.clipboard.writeText(invite.code);
      setCopied(true);
      window.setTimeout(() => setCopied(false), 3000);
    } catch {
      // 剪贴板不可用（非安全上下文 / 权限被拒）：用户可手动选中复制。
      setCopied(false);
      onError("无法自动复制，请手动选中邀请码复制。");
    }
  }

  return (
    <div className="flex flex-col gap-3">
      <p className="text-[0.84rem] leading-relaxed text-ink-2">
        生成邀请码后，把它发给要邀请的人。默认 7 天有效，可在创建时调整。
      </p>

      <button
        type="button"
        onClick={generate}
        disabled={creating}
        className="inline-flex w-fit items-center gap-2 rounded-[6px] bg-seal px-4 py-1.5 text-[0.85rem] font-semibold text-white hover:bg-seal-deep disabled:opacity-60"
      >
        {creating && (
          <span
            aria-hidden
            className="size-3 animate-spin rounded-full border-2 border-white/35 border-t-white"
          />
        )}
        {creating ? "生成中…" : "生成邀请码"}
      </button>

      {invite && (
        <div className="rounded-[6px] border border-line bg-surface-2 p-3">
          <div className="flex flex-wrap items-center gap-2">
            <code className="min-w-0 flex-1 truncate rounded-[3px] bg-paper px-2.5 py-1.5 font-mono text-[0.9rem] text-ink select-all">
              {invite.code}
            </code>
            <button
              type="button"
              onClick={copy}
              className="rounded-[6px] border border-line-strong bg-surface px-3 py-1.5 text-[0.8rem] font-semibold text-ink hover:border-ink-3"
            >
              {copied ? "已复制" : "复制"}
            </button>
          </div>
          <p className="mt-2 font-mono text-[0.68rem] text-ink-3">
            {invite.expiresAt ? `有效期至 ${formatDateTime(invite.expiresAt)}` : "长期有效"}
            {invite.maxUses !== null ? ` · 最多 ${invite.maxUses} 次` : ""}
          </p>
        </div>
      )}
    </div>
  );
}

/* -------------------------------------------------------------------------- */
/* 小部件                                                                      */
/* -------------------------------------------------------------------------- */

function ListSkeleton({ rows }: { rows: number }) {
  return (
    <div className="flex flex-col gap-2" aria-hidden>
      {Array.from({ length: rows }, (_, i) => (
        <div key={i} className="h-12 animate-pulse rounded-[6px] bg-surface-2" />
      ))}
    </div>
  );
}

function Empty({ text }: { text: string }) {
  return (
    <p className="rounded-[6px] border border-dashed border-line-strong bg-surface px-4 py-6 text-center text-[0.84rem] text-ink-2">
      {text}
    </p>
  );
}
