"use client";

import Link from "next/link";
import { useCallback, useEffect, useRef, useState } from "react";
import { useAuth } from "@/components/AuthProvider";
import { usePresence } from "@/components/motion";
import { formatRelativeTime } from "@/lib/posts";
import {
  getUnreadCount,
  listNotifications,
  markAllNotificationsRead,
  markNotificationRead,
  notificationHref,
  notificationText,
  type NotificationDTO,
} from "@/lib/notifications";

/**
 * 顶栏通知铃铛。
 *
 * 行为：
 * - 登录后轮询未读数（60 秒），点开时拉最近 10 条；
 * - 点击单条标记已读并跳转（有站内地址时）；
 * - 「全部已读」一次性清空角标。
 *
 * 未登录（或会话失效返回 401）时不渲染任何东西：通知属于个人数据，
 * 顶栏不该为一个空铃铛闪一下。
 */
const POLL_MS = 60_000;
const RECENT_LIMIT = 10;

export function NotificationBell() {
  const { status } = useAuth();
  const [open, setOpen] = useState(false);
  // 关闭时先播退场再卸载。
  const panel = usePresence(open, 100);
  const [unread, setUnread] = useState(0);
  const [items, setItems] = useState<NotificationDTO[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const ref = useRef<HTMLDivElement>(null);

  const authenticated = status === "authenticated";

  const refreshCount = useCallback(async () => {
    try {
      setUnread(await getUnreadCount());
    } catch {
      // 401 / 网络故障：保留上一次的数字，不打扰用户。
    }
  }, []);

  // 登录后拉一次未读数，随后定时轮询；登出时清零。
  useEffect(() => {
    if (!authenticated) {
      setUnread(0);
      setItems([]);
      setOpen(false);
      return;
    }
    void refreshCount();
    const timer = window.setInterval(() => void refreshCount(), POLL_MS);
    return () => window.clearInterval(timer);
  }, [authenticated, refreshCount]);

  // 点击外部或按 Esc 关闭面板。
  useEffect(() => {
    if (!open) return;

    function onPointerDown(event: MouseEvent | TouchEvent) {
      if (ref.current && !ref.current.contains(event.target as Node)) {
        setOpen(false);
      }
    }
    function onKeyDown(event: KeyboardEvent) {
      if (event.key === "Escape") setOpen(false);
    }

    document.addEventListener("mousedown", onPointerDown);
    document.addEventListener("touchstart", onPointerDown);
    document.addEventListener("keydown", onKeyDown);
    return () => {
      document.removeEventListener("mousedown", onPointerDown);
      document.removeEventListener("touchstart", onPointerDown);
      document.removeEventListener("keydown", onKeyDown);
    };
  }, [open]);

  const loadRecent = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const page = await listNotifications(null, RECENT_LIMIT);
      setItems(page.notifications);
    } catch {
      setError("通知加载失败，请稍后重试。");
    } finally {
      setLoading(false);
    }
  }, []);

  function toggle() {
    const next = !open;
    setOpen(next);
    if (next) void loadRecent();
  }

  async function handleMarkAll() {
    // 乐观更新：先清角标与未读态，失败时回滚到重新拉取的结果。
    const previous = items;
    setUnread(0);
    setItems((prev) => prev.map((item) => ({ ...item, read: true })));
    try {
      await markAllNotificationsRead();
    } catch {
      setItems(previous);
      void refreshCount();
      setError("操作失败，请稍后重试。");
    }
  }

  async function handleOpenItem(item: NotificationDTO) {
    if (item.read) return;
    // 乐观更新；失败时由下一次拉取纠正。
    setItems((prev) =>
      prev.map((n) => (n.id === item.id ? { ...n, read: true } : n)),
    );
    setUnread((n) => Math.max(0, n - 1));
    try {
      await markNotificationRead(item.id);
    } catch {
      void refreshCount();
    }
  }

  if (!authenticated) return null;

  return (
    <div className="relative" ref={ref}>
      <button
        type="button"
        onClick={toggle}
        aria-haspopup="dialog"
        aria-expanded={open}
        aria-label={unread > 0 ? `通知，${unread} 条未读` : "通知"}
        className="relative grid size-9 place-items-center rounded-[6px] text-ink-2 transition-colors hover:bg-surface-2 hover:text-ink"
      >
        <BellIcon />
        {unread > 0 && (
          <span
            aria-hidden
            className="absolute -top-0.5 -right-0.5 grid min-w-[16px] place-items-center rounded-[3px] bg-seal px-1 font-mono text-[0.62rem] leading-[16px] font-semibold text-white"
          >
            {unread > 99 ? "99+" : unread}
          </span>
        )}
      </button>

      {panel.mounted && (
        <div
          role="dialog"
          aria-label="通知"
          className={`absolute top-[calc(100%+6px)] right-0 z-30 w-[min(22rem,calc(100vw-2.5rem))] overflow-hidden rounded-[12px] border border-line bg-surface shadow-[var(--shadow-2)] ${
            panel.closing ? "anim-leave-drop" : "anim-drop"
          }`}
        >
          <div className="flex items-center justify-between gap-3 border-b border-line px-4 py-2.5">
            <p className="font-mono text-[0.72rem] tracking-[0.14em] text-ink-3 uppercase">
              通知
            </p>
            <button
              type="button"
              onClick={() => void handleMarkAll()}
              disabled={unread === 0}
              className="text-[0.78rem] font-semibold text-ink-2 underline underline-offset-2 hover:text-ink disabled:cursor-not-allowed disabled:no-underline disabled:opacity-45"
            >
              全部已读
            </button>
          </div>

          {error && (
            <p role="alert" className="border-b border-line px-4 py-2 text-[0.78rem] text-danger">
              {error}
            </p>
          )}

          {loading ? (
            <div className="flex flex-col gap-2 p-3" aria-hidden>
              {[0, 1, 2].map((i) => (
                <div key={i} className="h-12 animate-pulse rounded-[6px] bg-surface-2" />
              ))}
            </div>
          ) : items.length === 0 ? (
            <p className="px-4 py-10 text-center text-[0.85rem] text-ink-3">
              暂无通知。有人评论、点赞或申请入群时会出现在这里。
            </p>
          ) : (
            <ul className="max-h-[24rem] overflow-y-auto">
              {items.map((item) => (
                <NotificationRow
                  key={item.id}
                  item={item}
                  onOpen={() => void handleOpenItem(item)}
                />
              ))}
            </ul>
          )}

          <div className="border-t border-line px-4 py-2.5">
            <Link
              href="/settings"
              onClick={() => setOpen(false)}
              className="text-[0.78rem] font-semibold text-ink-2 hover:text-ink"
            >
              通知偏好设置
            </Link>
          </div>
        </div>
      )}
    </div>
  );
}

function NotificationRow({ item, onOpen }: { item: NotificationDTO; onOpen: () => void }) {
  const href = notificationHref(item);
  const body = (
    <>
      <span
        aria-hidden
        className={`mt-1.5 size-1.5 shrink-0 rounded-full ${
          item.read ? "bg-transparent" : "bg-seal"
        }`}
      />
      <span className="min-w-0 flex-1">
        <span className="block text-[0.85rem] leading-snug text-ink">{notificationText(item)}</span>
        <span className="mt-0.5 block font-mono text-[0.68rem] text-ink-3">
          {formatRelativeTime(item.createdAt)}
        </span>
      </span>
    </>
  );

  const className = `flex w-full items-start gap-2.5 border-b border-line px-4 py-3 text-left transition-colors last:border-b-0 hover:bg-surface-2 ${
    item.read ? "" : "bg-seal-wash/40"
  }`;

  if (href) {
    return (
      <li>
        <Link href={href} onClick={onOpen} className={className}>
          {body}
        </Link>
      </li>
    );
  }
  return (
    <li>
      <button type="button" onClick={onOpen} className={className}>
        {body}
      </button>
    </li>
  );
}

function BellIcon() {
  return (
    <svg
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.7"
      strokeLinecap="round"
      strokeLinejoin="round"
      className="size-[18px]"
      aria-hidden
    >
      <path d="M18 8a6 6 0 0 0-12 0c0 7-3 9-3 9h18s-3-2-3-9" />
      <path d="M13.7 21a2 2 0 0 1-3.4 0" />
    </svg>
  );
}
