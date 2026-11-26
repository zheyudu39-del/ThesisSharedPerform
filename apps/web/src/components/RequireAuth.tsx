"use client";

import Link from "next/link";
import { usePathname, useRouter } from "next/navigation";
import { useEffect } from "react";
import { useAuth } from "@/components/AuthProvider";

/**
 * 路由保护：未登录用户访问受保护页面时跳转到登录页，
 * 并在 `next` 参数中记录原路径，登录后可回跳。
 *
 * 注意：这是客户端保护，只影响体验，不构成安全边界。
 * 真正的授权必须在 Go 服务端按令牌校验（接口层已有 Authenticate 中间件）。
 */
export function RequireAuth({ children }: { children: React.ReactNode }) {
  const { status } = useAuth();
  const router = useRouter();
  const pathname = usePathname();

  useEffect(() => {
    if (status === "anonymous") {
      const next = encodeURIComponent(pathname || "/");
      router.replace(`/login?next=${next}`);
    }
  }, [status, router, pathname]);

  if (status === "loading") {
    return (
      <div className="mx-auto flex min-h-[50vh] w-full max-w-page items-center justify-center px-5">
        <p className="font-mono text-[0.8rem] tracking-[0.08em] text-ink-3 uppercase">
          正在确认登录状态…
        </p>
      </div>
    );
  }

  if (status === "anonymous") {
    // 跳转生效前的一帧，避免闪现受保护内容。
    return (
      <div className="mx-auto flex min-h-[50vh] w-full max-w-page flex-col items-center justify-center gap-3 px-5 text-center">
        <p className="text-sm text-ink-2">请先登录后访问此页面。</p>
        <Link href="/login" className="text-sm font-semibold text-seal hover:underline">
          前往登录
        </Link>
      </div>
    );
  }

  return <>{children}</>;
}
