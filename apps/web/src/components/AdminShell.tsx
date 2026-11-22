"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import { useAuth } from "@/components/AuthProvider";
import { RequireAuth } from "@/components/RequireAuth";

/**
 * 后台外壳：侧边栏导航 + 内容区。
 *
 * 权限处理与 /admin/reports 一致：
 *   1. 未登录 → RequireAuth 跳转登录页；
 *   2. 已登录但不是 admin → 展示 403 说明，不发任何后台请求；
 *   3. 是 admin → 渲染页面。
 *
 * 这只影响体验，不构成安全边界：所有 /admin/* 接口在 Go 服务层
 * 都会用 requireAdmin 再查一次角色，非运营一律 403。
 */

const NAV = [
  { href: "/admin", label: "概览", hint: "总量与趋势" },
  { href: "/admin/users", label: "用户", hint: "账号与权限" },
  { href: "/admin/posts", label: "帖子", hint: "内容管理" },
  { href: "/admin/comments", label: "评论", hint: "评论管理" },
  { href: "/admin/papers", label: "论文", hint: "论文库" },
  { href: "/admin/groups", label: "群组", hint: "群组管理" },
  { href: "/admin/files", label: "文件", hint: "上传与转码" },
  { href: "/admin/reports", label: "举报", hint: "审核队列" },
  { href: "/admin/audit", label: "审计", hint: "操作记录" },
] as const;

export function AdminShell({ children }: { children: React.ReactNode }) {
  return (
    <RequireAuth>
      <Shell>{children}</Shell>
    </RequireAuth>
  );
}

function Shell({ children }: { children: React.ReactNode }) {
  const { status, user } = useAuth();
  const pathname = usePathname();
  const isAdmin = status === "authenticated" && user?.role === "admin";

  if (status === "authenticated" && !isAdmin) {
    return (
      <main className="mx-auto w-full max-w-3xl px-5 py-16 text-center">
        <p className="mb-2 font-mono text-[0.72rem] tracking-[0.16em] text-ink-3 uppercase">
          403 · Forbidden
        </p>
        <h1 className="font-display text-[1.5rem] font-semibold">只有运营可以访问后台</h1>
        <p className="mt-3 text-[0.92rem] text-ink-2">
          后台包含全站用户与内容数据，不对普通用户开放。
        </p>
        <Link
          href="/"
          className="mt-6 inline-block text-[0.9rem] font-semibold text-seal hover:underline"
        >
          返回首页
        </Link>
      </main>
    );
  }

  return (
    <div className="min-h-dvh bg-surface-2">
      {/* 后台自己的顶栏：不复用 SiteHeader，避免用户误以为还在前台。 */}
      <header className="sticky top-0 z-20 border-b border-line bg-paper/92 backdrop-blur-md">
        <div className="mx-auto flex h-[56px] max-w-7xl items-center gap-4 px-5">
          <Link href="/admin" className="flex items-center gap-2.5">
            <span className="grid size-6 place-items-center rounded-[3px] bg-ink font-display text-[12px] font-semibold text-paper">
              运
            </span>
            <span className="font-display text-[1rem] font-semibold tracking-tight">
              运营后台
            </span>
          </Link>
          <span className="hidden font-mono text-[0.7rem] text-ink-3 sm:inline">
            {user?.handle ? `@${user.handle}` : ""}
          </span>
          <div className="ml-auto flex items-center gap-3">
            <Link
              href="/"
              className="rounded-[6px] border border-line-strong px-3 py-1.5 text-[0.8rem] text-ink-2 hover:border-ink-3 hover:text-ink"
            >
              返回前台
            </Link>
          </div>
        </div>
      </header>

      <div className="mx-auto flex max-w-7xl flex-col gap-6 px-5 py-6 lg:flex-row">
        <nav
          aria-label="后台导航"
          className="lg:sticky lg:top-[72px] lg:h-fit lg:w-[188px] lg:shrink-0"
        >
          <ul className="flex gap-1 overflow-x-auto lg:flex-col lg:overflow-visible">
            {NAV.map((item) => {
              // 精确匹配根路径，其余按前缀匹配，避免 /admin 高亮所有子页。
              const active =
                item.href === "/admin"
                  ? pathname === "/admin"
                  : pathname.startsWith(item.href);
              return (
                <li key={item.href} className="shrink-0">
                  <Link
                    href={item.href}
                    aria-current={active ? "page" : undefined}
                    className={`flex flex-col rounded-[6px] px-3 py-2 transition-colors ${
                      active
                        ? "bg-ink text-paper"
                        : "text-ink-2 hover:bg-surface hover:text-ink"
                    }`}
                  >
                    <span className="text-[0.85rem] font-medium">{item.label}</span>
                    <span
                      className={`font-mono text-[0.66rem] ${
                        active ? "text-paper/60" : "text-ink-3"
                      }`}
                    >
                      {item.hint}
                    </span>
                  </Link>
                </li>
              );
            })}
          </ul>
        </nav>

        <div className="min-w-0 flex-1">{children}</div>
      </div>
    </div>
  );
}
