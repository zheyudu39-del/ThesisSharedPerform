"use client";

import { usePathname, useRouter } from "next/navigation";
import { useEffect, useRef, useState } from "react";
import { useAuth } from "@/components/AuthProvider";
import { AuthError } from "@/lib/auth";
import {
  getGroup,
  joinByCode,
  joinPublicGroup,
  requestJoin,
  GROUP_LIMITS,
  GROUP_ROLE_LABEL,
  type GroupRole,
  type JoinPolicy,
} from "@/lib/groups";

/**
 * 群组成员身份的前端交互。
 *
 * 与服务端渲染的分工：
 * - Server Component 已经把群名 / 简介 / 成员数直出；
 * - 这里接管「加入 / 申请 / 邀请码」按钮，以及加入成功后跨组件同步成员数
 *   （顶部的计数与下方的动态 / 成员列表并不相邻，用浏览器事件传递，避免整页变客户端）。
 */

const MEMBERSHIP_EVENT = "thesis:group-membership";

export type GroupMembershipDetail = {
  slug: string;
  myRole: GroupRole | null;
  memberCount: number;
};

export function emitGroupMembership(detail: GroupMembershipDetail): void {
  if (typeof window === "undefined") return;
  window.dispatchEvent(
    new CustomEvent<GroupMembershipDetail>(MEMBERSHIP_EVENT, { detail }),
  );
}

/** 群成员数：初始值来自服务端，加入 / 审批通过后跟随事件更新。 */
export function GroupMemberCount({ slug, initial }: { slug: string; initial: number }) {
  const [count, setCount] = useState(initial);

  useEffect(() => {
    function onMembership(event: Event) {
      const detail = (event as CustomEvent<GroupMembershipDetail>).detail;
      if (detail?.slug === slug) setCount(detail.memberCount);
    }
    window.addEventListener(MEMBERSHIP_EVENT, onMembership);
    return () => window.removeEventListener(MEMBERSHIP_EVENT, onMembership);
  }, [slug]);

  return <span className="tabular-nums">{count}</span>;
}

/** 供动态 / 成员列表订阅「我的身份变了」事件。 */
export function useGroupMembership(slug: string, onMembership: (detail: GroupMembershipDetail) => void) {
  const handler = useRef(onMembership);
  useEffect(() => {
    handler.current = onMembership;
  }, [onMembership]);

  useEffect(() => {
    function listener(event: Event) {
      const detail = (event as CustomEvent<GroupMembershipDetail>).detail;
      if (detail?.slug === slug) handler.current(detail);
    }
    window.addEventListener(MEMBERSHIP_EVENT, listener);
    return () => window.removeEventListener(MEMBERSHIP_EVENT, listener);
  }, [slug]);
}

export type GroupJoinButtonProps = {
  slug: string;
  joinPolicy: JoinPolicy;
  /** 服务端给出的当前身份；null 表示非成员（或匿名）。 */
  initialRole: GroupRole | null;
  /** 当前成员数：加入成功后用于广播 +1。 */
  memberCount: number;
};

export function GroupJoinButton({
  slug,
  joinPolicy,
  initialRole,
  memberCount,
}: GroupJoinButtonProps) {
  const { status } = useAuth();
  const router = useRouter();
  const pathname = usePathname();

  const [role, setRole] = useState<GroupRole | null>(initialRole);
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);
  const [mode, setMode] = useState<"idle" | "request" | "code">("idle");
  const [message, setMessage] = useState("");
  const [code, setCode] = useState("");

  const mounted = useRef(true);
  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
    };
  }, []);

  // 服务端身份变化（例如 router.refresh）时同步。
  useEffect(() => {
    setRole(initialRole);
  }, [initialRole]);

  useEffect(() => {
    if (!error && !notice) return;
    const timer = window.setTimeout(() => {
      setError(null);
      setNotice(null);
    }, 6000);
    return () => window.clearTimeout(timer);
  }, [error, notice]);

  function goLogin() {
    const next = encodeURIComponent(pathname || `/groups/${slug}`);
    router.push(`/login?next=${next}`);
  }

  /** 重新向服务端确认身份（用于 already_member 等边界情况）。 */
  async function refreshMembership() {
    try {
      const detail = await getGroup(slug);
      if (!mounted.current) return;
      setRole(detail.myRole);
      emitGroupMembership({
        slug,
        myRole: detail.myRole,
        memberCount: detail.memberCount,
      });
    } catch {
      // 确认失败时保持现状，用户可手动刷新。
    }
  }

  async function handleJoin() {
    if (pending) return;
    if (status !== "authenticated") {
      goLogin();
      return;
    }

    if (joinPolicy === "request") {
      setMode((m) => (m === "request" ? "idle" : "request"));
      return;
    }
    if (joinPolicy === "invite") {
      setMode((m) => (m === "code" ? "idle" : "code"));
      return;
    }

    // public：直接加入。
    setPending(true);
    setError(null);
    try {
      const group = await joinPublicGroup(slug);
      if (!mounted.current) return;
      const nextRole = group.myRole ?? "member";
      setRole(nextRole);
      emitGroupMembership({ slug, myRole: nextRole, memberCount: memberCount + 1 });
      setNotice("已加入群组。");
    } catch (err) {
      if (!mounted.current) return;
      if (err instanceof AuthError && err.code === "already_member") {
        await refreshMembership();
      } else {
        setError(err instanceof AuthError ? err.message : "加入失败，请稍后重试。");
      }
    } finally {
      if (mounted.current) setPending(false);
    }
  }

  async function submitRequest() {
    if (pending) return;
    if ([...message].length > GROUP_LIMITS.requestMessageMax) {
      setError(`申请说明最多 ${GROUP_LIMITS.requestMessageMax} 个字符。`);
      return;
    }

    setPending(true);
    setError(null);
    try {
      await requestJoin(slug, message);
      if (!mounted.current) return;
      setMode("idle");
      setMessage("");
      setNotice("已提交申请，等待群主或管理员审批。");
    } catch (err) {
      if (!mounted.current) return;
      if (err instanceof AuthError && err.code === "already_member") {
        await refreshMembership();
      } else if (err instanceof AuthError && err.code === "already_requested") {
        setMode("idle");
        setNotice("你已经提交过申请，请等待审批。");
      } else {
        setError(err instanceof AuthError ? err.message : "提交失败，请稍后重试。");
      }
    } finally {
      if (mounted.current) setPending(false);
    }
  }

  async function submitCode() {
    if (pending) return;
    if (!code.trim()) {
      setError("请输入邀请码。");
      return;
    }

    setPending(true);
    setError(null);
    try {
      const group = await joinByCode(code);
      if (!mounted.current) return;
      setCode("");
      setMode("idle");
      if (group.slug !== slug) {
        // 邀请码属于另一个群组：直接带用户过去。
        router.push(`/groups/${encodeURIComponent(group.slug)}`);
        return;
      }
      const nextRole = group.myRole ?? "member";
      setRole(nextRole);
      emitGroupMembership({ slug, myRole: nextRole, memberCount: memberCount + 1 });
      setNotice("已加入群组。");
    } catch (err) {
      if (!mounted.current) return;
      if (err instanceof AuthError && err.code === "already_member") {
        await refreshMembership();
      } else if (err instanceof AuthError && err.code === "invite_not_found") {
        setError("邀请码不存在、已过期或已用尽。");
      } else {
        setError(err instanceof AuthError ? err.message : "加入失败，请稍后重试。");
      }
    } finally {
      if (mounted.current) setPending(false);
    }
  }

  if (status === "loading") {
    return (
      <span
        aria-hidden
        className="h-9 w-24 animate-pulse rounded-[6px] bg-surface-2"
      />
    );
  }

  // 已是成员：身份徽章 + 管理入口。
  if (role) {
    return (
      <div className="flex flex-col items-end gap-1.5">
        <span className="inline-flex items-center gap-1.5 rounded-full border border-line bg-surface px-3 py-1 text-[0.8rem] font-semibold text-ink">
          <span aria-hidden className="size-1.5 rounded-full bg-good" />
          {GROUP_ROLE_LABEL[role]}
        </span>
        {(role === "owner" || role === "admin") && (
          <a href="#group-admin" className="text-[0.78rem] text-seal hover:underline">
            管理群组
          </a>
        )}
      </div>
    );
  }

  const buttonLabel =
    joinPolicy === "public" ? "加入" : joinPolicy === "request" ? "申请加入" : "需要邀请码";

  return (
    <div className="flex max-w-sm flex-col items-end gap-2">
      <button
        type="button"
        onClick={handleJoin}
        disabled={pending}
        aria-busy={pending}
        className="inline-flex items-center justify-center gap-2 rounded-[6px] bg-seal px-4 py-1.5 text-[0.85rem] font-semibold text-white shadow-[var(--shadow-1)] transition-colors hover:bg-seal-deep disabled:cursor-not-allowed disabled:opacity-60"
      >
        {pending && (
          <span
            aria-hidden
            className="size-3 animate-spin rounded-full border-2 border-white/35 border-t-white"
          />
        )}
        {buttonLabel}
      </button>

      {mode === "request" && (
        <div className="w-full rounded-[6px] border border-line bg-surface p-3">
          <label
            htmlFor={`group-request-${slug}`}
            className="mb-1.5 block font-mono text-[0.68rem] tracking-[0.08em] text-ink-3 uppercase"
          >
            申请说明（可选）
          </label>
          <textarea
            id={`group-request-${slug}`}
            value={message}
            onChange={(e) => setMessage(e.target.value)}
            rows={3}
            maxLength={GROUP_LIMITS.requestMessageMax}
            placeholder="简单介绍你的研究方向，方便管理员判断。"
            className="w-full resize-y rounded-[6px] border border-line-strong bg-paper px-2.5 py-2 text-[0.84rem] leading-relaxed text-ink placeholder:text-ink-3 focus:border-seal focus:outline-none"
          />
          <div className="mt-2 flex items-center justify-between gap-2">
            <span className="font-mono text-[0.66rem] text-ink-3 tabular-nums">
              {[...message].length}/{GROUP_LIMITS.requestMessageMax}
            </span>
            <button
              type="button"
              onClick={submitRequest}
              disabled={pending}
              className="rounded-[6px] bg-seal px-3 py-1.5 text-[0.8rem] font-semibold text-white hover:bg-seal-deep disabled:opacity-60"
            >
              提交申请
            </button>
          </div>
        </div>
      )}

      {mode === "code" && (
        <div className="w-full rounded-[6px] border border-line bg-surface p-3">
          <label
            htmlFor={`group-code-${slug}`}
            className="mb-1.5 block font-mono text-[0.68rem] tracking-[0.08em] text-ink-3 uppercase"
          >
            邀请码
          </label>
          <div className="flex gap-2">
            <input
              id={`group-code-${slug}`}
              value={code}
              onChange={(e) => setCode(e.target.value)}
              autoComplete="off"
              placeholder="粘贴群主或管理员给出的邀请码"
              className="min-w-0 flex-1 rounded-[6px] border border-line-strong bg-paper px-2.5 py-1.5 font-mono text-[0.82rem] text-ink placeholder:text-ink-3 focus:border-seal focus:outline-none"
            />
            <button
              type="button"
              onClick={submitCode}
              disabled={pending || !code.trim()}
              className="rounded-[6px] bg-seal px-3 py-1.5 text-[0.8rem] font-semibold text-white hover:bg-seal-deep disabled:opacity-60"
            >
              加入
            </button>
          </div>
        </div>
      )}

      {notice && (
        <p role="status" className="text-[0.75rem] text-good">
          {notice}
        </p>
      )}
      {error && (
        <p role="alert" className="text-[0.75rem] text-danger">
          {error}
        </p>
      )}
    </div>
  );
}
