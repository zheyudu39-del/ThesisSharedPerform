"use client";

import { usePathname, useRouter } from "next/navigation";
import { useEffect, useRef, useState } from "react";
import { useAuth } from "@/components/AuthProvider";
import { AuthError } from "@/lib/auth";
import { followUser, unfollowUser } from "@/lib/users";

/**
 * 关注 / 取关按钮。
 *
 * - 自己：不渲染（没有关注自己的语义，服务端也会返回 422 self_follow）；
 * - 未登录：点击后跳转登录页，并用 `next` 记住当前路径；
 * - 已登录：乐观更新，失败回滚并短暂提示，避免静默失败。
 *
 * 计数以服务端响应为准（幂等接口返回 followerCount），父级可通过
 * `onChange` 同步自己的展示。
 */

export type FollowButtonProps = {
  /** 被关注者的句柄（不含 @）。 */
  handle: string;
  /** 被关注者的 id：与当前用户相同则不渲染。 */
  userId?: string;
  /** 服务端给出的初始关注状态。 */
  initialFollowing?: boolean;
  /** 关注状态或计数变化时回调。 */
  onChange?: (state: { following: boolean; followerCount: number }) => void;
  /** 紧凑模式：用于卡片、列表中。 */
  size?: "sm" | "md";
};

export function FollowButton({
  handle,
  userId,
  initialFollowing = false,
  onChange,
  size = "md",
}: FollowButtonProps) {
  const { status, user } = useAuth();
  const router = useRouter();
  const pathname = usePathname();

  const [following, setFollowing] = useState(initialFollowing);
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const mounted = useRef(true);
  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
    };
  }, []);

  // 父级异步拿到真实资料后（例如从骨架切到内容），同步服务端状态。
  useEffect(() => {
    setFollowing(initialFollowing);
  }, [initialFollowing, handle]);

  useEffect(() => {
    if (!error) return;
    const timer = window.setTimeout(() => setError(null), 5000);
    return () => window.clearTimeout(timer);
  }, [error]);

  // 自己不发关注按钮；登录态未确定时也先占位，避免出现后又消失。
  const isSelf = status === "authenticated" && user !== null && (user.id === userId || user.handle === handle);
  if (isSelf) return null;
  if (status === "loading") {
    return (
      <span
        aria-hidden
        className={`animate-pulse rounded-[6px] bg-surface-2 ${
          size === "sm" ? "h-7 w-16" : "h-9 w-24"
        }`}
      />
    );
  }

  async function toggle() {
    if (pending) return;
    if (status !== "authenticated") {
      const next = encodeURIComponent(pathname || `/u/${handle}`);
      router.push(`/login?next=${next}`);
      return;
    }

    const wasFollowing = following;
    const nextFollowing = !wasFollowing;
    setFollowing(nextFollowing);
    setPending(true);
    setError(null);

    try {
      const result = nextFollowing ? await followUser(handle) : await unfollowUser(handle);
      if (!mounted.current) return;
      setFollowing(result.following);
      onChange?.({ following: result.following, followerCount: result.followerCount });
    } catch (err) {
      if (!mounted.current) return;
      setFollowing(wasFollowing);
      const message =
        err instanceof AuthError && err.code === "self_follow"
          ? "不能关注自己。"
          : err instanceof AuthError
            ? err.message
            : "操作失败，请稍后重试。";
      setError(message);
      // 计数从未被乐观修改（只在成功后由 onChange 同步），因此无需回滚父级。
    } finally {
      if (mounted.current) setPending(false);
    }
  }

  const base =
    size === "sm"
      ? "inline-flex items-center justify-center gap-1.5 rounded-[6px] px-3 py-1 text-[0.8rem]"
      : "inline-flex items-center justify-center gap-1.5 rounded-[6px] px-4 py-1.5 text-[0.85rem]";

  return (
    <div className="flex flex-col items-end gap-1.5">
      <button
        type="button"
        onClick={toggle}
        disabled={pending}
        aria-pressed={following}
        aria-busy={pending}
        className={`${base} font-semibold transition-colors disabled:cursor-not-allowed disabled:opacity-60 ${
          following
            ? "border border-line-strong bg-surface text-ink hover:border-danger/60 hover:text-danger"
            : "bg-seal text-white shadow-[var(--shadow-1)] hover:bg-seal-deep"
        }`}
      >
        {pending && (
          <span
            aria-hidden
            className={`size-3 animate-spin rounded-full border-2 ${
              following ? "border-ink-3/40 border-t-ink" : "border-white/35 border-t-white"
            }`}
          />
        )}
        {following ? "已关注" : "关注"}
      </button>

      {error && (
        <p role="alert" className="text-[0.75rem] text-danger">
          {error}
        </p>
      )}
    </div>
  );
}
