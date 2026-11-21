"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { useEffect, useRef, useState } from "react";
import { useAuth } from "@/components/AuthProvider";
import { NotificationBell } from "@/components/NotificationBell";

/**
 * 站点顶栏：品牌字标 + 主导航 + 登录态。
 * 印字方块是「墨与印」的视觉锚点，全站唯一固定出现的高饱和色。
 */
export function SiteHeader() {
  const { status, user, signOut } = useAuth();
  const router = useRouter();

  return (
    <header className="sticky top-0 z-20 border-b border-line bg-paper/88 backdrop-blur-md">
      <div className="mx-auto flex h-[60px] max-w-6xl items-center gap-6 px-5">
        <Link href="/" className="flex items-center gap-2.5">
          <span className="grid size-6 place-items-center rounded-[3px] bg-seal font-display text-[13px] font-semibold text-white shadow-[inset_0_0_0_2px_rgba(255,255,255,0.24)]">
            印
          </span>
          <span className="font-display text-[1.1rem] font-semibold tracking-tight">
            Thesis Shared
          </span>
        </Link>

        <nav className="hidden items-center gap-1 text-sm text-ink-2 sm:flex">
          <Link href="/" className="rounded-[6px] px-3 py-1.5 hover:bg-surface-2 hover:text-ink">
            关注流
          </Link>
          <Link
            href="/collections"
            className="rounded-[6px] px-3 py-1.5 hover:bg-surface-2 hover:text-ink"
          >
            精选
          </Link>
          <Link
            href="/papers"
            className="rounded-[6px] px-3 py-1.5 hover:bg-surface-2 hover:text-ink"
          >
            论文库
          </Link>
          <Link
            href="/groups"
            className="rounded-[6px] px-3 py-1.5 hover:bg-surface-2 hover:text-ink"
          >
            群组
          </Link>
          <Link
            href="/search"
            className="rounded-[6px] px-3 py-1.5 hover:bg-surface-2 hover:text-ink"
          >
            搜索
          </Link>
        </nav>

        <div className="ml-auto flex items-center gap-2">
          {/* 手机端导航收起，搜索仍需可达：单独留一个图标入口。 */}
          <Link
            href="/search"
            aria-label="搜索"
            className="grid size-9 place-items-center rounded-[6px] text-ink-2 hover:bg-surface-2 hover:text-ink sm:hidden"
          >
            <SearchIcon />
          </Link>
          {status === "loading" && (
            // 骨架：与服务端首帧一致，避免 hydration 闪烁。
            <span className="h-8 w-24 animate-pulse rounded-[6px] bg-surface-2" aria-hidden />
          )}

          {status === "anonymous" && (
            <>
              <Link
                href="/login"
                className="rounded-[6px] px-3 py-1.5 text-sm text-ink-2 hover:bg-surface-2 hover:text-ink"
              >
                登录
              </Link>
              <Link
                href="/register"
                className="rounded-[6px] bg-seal px-3.5 py-1.5 text-sm font-semibold text-white hover:bg-seal-deep"
              >
                注册
              </Link>
            </>
          )}

          {/* 铃铛放在用户菜单左侧；未登录时自身返回 null。 */}
          <NotificationBell />

          {status === "authenticated" && user && (
            <UserMenu
              name={user.name}
              handle={user.handle}
              onSignOut={() => {
                void signOut();
                router.push("/");
              }}
            />
          )}
        </div>
      </div>
    </header>
  );
}

function SearchIcon() {
  return (
    <svg
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.8"
      strokeLinecap="round"
      className="size-[18px]"
      aria-hidden
    >
      <circle cx="11" cy="11" r="6.5" />
      <path d="m16 16 4.5 4.5" />
    </svg>
  );
}

function UserMenu({
  name,
  handle,
  onSignOut,
}: {
  name: string;
  handle: string;
  onSignOut: () => void;
}) {
  const [open, setOpen] = useState(false);
  const ref = useRef<HTMLDivElement>(null);

  // 点击外部或按 Esc 关闭菜单。
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

  const initial = [...name][0] ?? "?";

  return (
    <div className="relative" ref={ref}>
      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        aria-haspopup="menu"
        aria-expanded={open}
        className="flex items-center gap-2 rounded-[6px] py-1 pr-2 pl-1 hover:bg-surface-2"
      >
        <span className="grid size-7 place-items-center rounded-full bg-ink font-display text-[13px] text-paper">
          {initial}
        </span>
        <span className="hidden max-w-[9rem] truncate text-sm text-ink sm:block">{name}</span>
        <svg
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          strokeWidth="2"
          className={`size-3.5 text-ink-3 transition-transform ${open ? "rotate-180" : ""}`}
          aria-hidden
        >
          <path d="m6 9 6 6 6-6" />
        </svg>
      </button>

      {open && (
        <div
          role="menu"
          className="absolute top-[calc(100%+6px)] right-0 z-30 w-56 overflow-hidden rounded-[12px] border border-line bg-surface shadow-[var(--shadow-2)]"
        >
          <div className="border-b border-line px-4 py-3">
            <p className="truncate text-sm font-semibold text-ink">{name}</p>
            <p className="truncate font-mono text-[0.72rem] text-ink-3">@{handle}</p>
          </div>
          <div className="flex flex-col p-1.5">
            <Link
              href={`/u/${handle}`}
              role="menuitem"
              onClick={() => setOpen(false)}
              className="rounded-[6px] px-3 py-2 text-left text-sm text-ink-2 hover:bg-surface-2 hover:text-ink"
            >
              个人主页
            </Link>
            <Link
              href="/settings"
              role="menuitem"
              onClick={() => setOpen(false)}
              className="rounded-[6px] px-3 py-2 text-left text-sm text-ink-2 hover:bg-surface-2 hover:text-ink"
            >
              设置
            </Link>
            <button
              type="button"
              role="menuitem"
              onClick={() => {
                setOpen(false);
                onSignOut();
              }}
              className="rounded-[6px] px-3 py-2 text-left text-sm text-danger hover:bg-danger-wash"
            >
              退出登录
            </button>
          </div>
        </div>
      )}
    </div>
  );
}
