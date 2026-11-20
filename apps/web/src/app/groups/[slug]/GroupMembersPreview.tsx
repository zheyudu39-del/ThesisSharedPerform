"use client";

import Link from "next/link";
import { useCallback, useEffect, useRef, useState } from "react";
import { GroupMemberCount, useGroupMembership } from "@/components/GroupMembership";
import { SealBadge } from "@/components/SealBadge";
import { AuthError } from "@/lib/auth";
import { GROUP_ROLE_LABEL, listMembers, type MemberDTO } from "@/lib/groups";

/**
 * 群组成员预览（侧栏）。
 *
 * - 成员列表需要登录；非成员展示「加入后可见」；
 * - 加入成功后自动加载；
 * - 展示前 8 位，按角色排序（群主 → 管理员 → 成员）由后端顺序保证。
 */

export type GroupMembersPreviewProps = {
  slug: string;
  isMember: boolean;
  memberCount: number;
};

const PREVIEW_LIMIT = 8;

export function GroupMembersPreview({ slug, isMember, memberCount }: GroupMembersPreviewProps) {
  const [members, setMembers] = useState<MemberDTO[]>([]);
  const [loading, setLoading] = useState(isMember);
  const [error, setError] = useState<string | null>(null);
  const [visible, setVisible] = useState(isMember);

  const mounted = useRef(true);
  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
    };
  }, []);

  // 服务端身份变化时同步（例如 router.refresh）。
  useEffect(() => {
    setVisible(isMember);
    if (!isMember) setMembers([]);
  }, [isMember]);

  const load = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const page = await listMembers(slug, null, 20);
      if (!mounted.current) return;
      setMembers(page.members);
      setVisible(true);
    } catch (err) {
      if (!mounted.current) return;
      setError(err instanceof AuthError ? err.message : "成员列表加载失败。");
    } finally {
      if (mounted.current) setLoading(false);
    }
  }, [slug]);

  useEffect(() => {
    if (visible && members.length === 0) void load();
  }, [visible, members.length, load]);

  // 加入群组后自动加载成员（setVisible 会触发下面的加载 effect）；
  // 成员数变化（审批通过 / 移除）时刷新列表。
  useGroupMembership(slug, (detail) => {
    if (!detail.myRole) return;
    if (!visible) {
      setVisible(true);
      return;
    }
    void load();
  });

  return (
    <section className="rounded-[12px] border border-line bg-surface p-4 shadow-[var(--shadow-1)]">
      <h2 className="mb-3 flex items-baseline justify-between gap-2 border-b border-line pb-2.5">
        <span className="font-display text-[1rem] font-semibold">成员</span>
        <span className="font-mono text-[0.7rem] text-ink-3">
          <GroupMemberCount slug={slug} initial={memberCount} /> 人
        </span>
      </h2>

      {!visible ? (
        <p className="text-[0.82rem] leading-relaxed text-ink-2">加入群组后可以查看成员。</p>
      ) : loading ? (
        <ul className="flex flex-col gap-2" aria-hidden>
          {[0, 1, 2, 3].map((i) => (
            <li key={i} className="h-9 animate-pulse rounded-[6px] bg-surface-2" />
          ))}
        </ul>
      ) : error ? (
        <div>
          <p role="alert" className="text-[0.8rem] text-danger">
            {error}
          </p>
          <button
            type="button"
            onClick={() => void load()}
            className="mt-2 text-[0.78rem] font-semibold text-seal hover:underline"
          >
            重试
          </button>
        </div>
      ) : members.length === 0 ? (
        <p className="text-[0.82rem] text-ink-2">还没有成员。</p>
      ) : (
        <ul className="flex flex-col gap-1">
          {members.slice(0, PREVIEW_LIMIT).map((member) => (
            <li key={member.userId}>
              <Link
                href={`/u/${encodeURIComponent(member.handle)}`}
                className="flex items-center gap-2.5 rounded-[6px] px-1.5 py-1.5 hover:bg-surface-2"
              >
                <span className="grid size-7 shrink-0 place-items-center rounded-full bg-ink font-display text-[12px] text-paper">
                  {[...member.name][0] ?? "?"}
                </span>
                <span className="min-w-0 flex-1">
                  <span className="flex items-center gap-1.5">
                    <span className="truncate text-[0.84rem] font-semibold">{member.name}</span>
                    {member.verifyType === "scholar" && <SealBadge variant="scholar" />}
                  </span>
                  <span className="block truncate font-mono text-[0.66rem] text-ink-3">
                    @{member.handle}
                  </span>
                </span>
                {member.role !== "member" && (
                  <span className="shrink-0 rounded-full bg-surface-2 px-1.5 py-0.5 font-mono text-[0.62rem] text-ink-2">
                    {GROUP_ROLE_LABEL[member.role]}
                  </span>
                )}
              </Link>
            </li>
          ))}
        </ul>
      )}

      {visible && members.length > PREVIEW_LIMIT && (
        <p className="mt-2 text-center font-mono text-[0.7rem] text-ink-3">
          还有 {members.length - PREVIEW_LIMIT} 位成员，完整名单见下方管理面板。
        </p>
      )}
    </section>
  );
}
