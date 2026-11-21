"use client";

import { useEffect, useState } from "react";
import { usePathname, useRouter } from "next/navigation";
import { useAuth } from "@/components/AuthProvider";
import { ApiError } from "@/lib/http";
import { blockUser, getBlockStatus, unblockUser } from "@/lib/moderation";

/**
 * 个人主页的「屏蔽 / 取消屏蔽」按钮（PRD F-06）。
 *
 * 独立成组件而不是写进 ProfileInteractive.tsx：个人主页由另一位代理维护，
 * 这里只交付一个自包含控件，接线时在主页作者信息旁放一行即可：
 *
 *   <BlockButton handle={user.handle} isSelf={...} />
 *
 * 屏蔽只影响「我的信息流」，不会删除对方内容，主页仍可访问；
 * 因此文案强调「不再看到 TA 的帖子」，避免被误解为拉黑删除。
 */
export function BlockButton({
  handle,
  isSelf = false,
}: {
  handle: string;
  /** 自己的主页不显示屏蔽按钮。 */
  isSelf?: boolean;
}) {
  const { status, user } = useAuth();
  const router = useRouter();
  const pathname = usePathname();

  const [blocked, setBlocked] = useState(false);
  const [known, setKnown] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // 屏蔽状态是个人数据，只有登录后才查询；未登录不请求，避免 401 噪音。
  useEffect(() => {
    if (status !== "authenticated" || isSelf) return;
    let cancelled = false;
    void (async () => {
      try {
        const value = await getBlockStatus(handle);
        if (!cancelled) {
          setBlocked(value);
          setKnown(true);
        }
      } catch {
        // 状态查询失败不阻塞页面：按钮按「未屏蔽」展示，点击时仍会得到服务端裁决。
        if (!cancelled) setKnown(true);
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [status, handle, isSelf]);

  // 调用方可能漏传 isSelf（主页是 Server Component，拿不到当前用户），
  // 因此再用会话里的 handle 兜一次底：自己的主页永远不显示屏蔽按钮。
  const self = isSelf || (user !== null && user.handle === handle);
  if (self) return null;

  async function toggle() {
    if (busy) return;
    if (status !== "authenticated") {
      const next = encodeURIComponent(pathname || "/");
      router.push(`/login?next=${next}`);
      return;
    }
    setBusy(true);
    setError(null);
    try {
      if (blocked) {
        await unblockUser(handle);
        setBlocked(false);
      } else {
        await blockUser(handle);
        setBlocked(true);
      }
    } catch (err) {
      // 409 / 404 是「状态已变化」，以服务端为准重新对齐按钮。
      if (err instanceof ApiError && (err.status === 409 || err.status === 404)) {
        setBlocked(err.status === 409);
      } else {
        setError(err instanceof ApiError ? err.message : "操作失败，请稍后重试。");
      }
    } finally {
      setBusy(false);
    }
  }

  // 状态未知时先不渲染，避免按钮文案从「屏蔽」跳到「已屏蔽」。
  if (!known && status === "authenticated") return null;

  return (
    <div className="inline-flex flex-col items-start gap-1">
      <button
        type="button"
        onClick={toggle}
        disabled={busy}
        aria-pressed={blocked}
        className={`rounded-[6px] border px-3.5 py-1.5 text-[0.84rem] font-semibold transition-colors disabled:opacity-60 ${
          blocked
            ? "border-line-strong text-ink-2 hover:border-danger hover:text-danger"
            : "border-line text-ink-2 hover:border-line-strong hover:text-ink"
        }`}
      >
        {busy ? "处理中…" : blocked ? "已屏蔽 · 取消" : "屏蔽"}
      </button>
      {error && (
        <span role="alert" className="text-[0.76rem] text-danger">
          {error}
        </span>
      )}
    </div>
  );
}
