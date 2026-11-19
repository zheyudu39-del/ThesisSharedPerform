import Link from "next/link";
import type { ReactNode } from "react";
import { SiteHeader } from "@/components/SiteHeader";

/**
 * 认证页的共用外框：纸张底色 + 居中单列卡片。
 * 手机宽度下保持 16px 以上侧边留白，桌面端限制阅读宽度。
 */
export function AuthShell({
  eyebrow,
  title,
  lede,
  footer,
  children,
}: {
  eyebrow: string;
  title: string;
  lede: string;
  footer: ReactNode;
  children: ReactNode;
}) {
  return (
    <>
      <SiteHeader />

      <main className="mx-auto w-full max-w-6xl px-5 py-12 sm:py-16">
        <div className="mx-auto flex w-full max-w-[420px] flex-col">
          <div className="mb-7">
            <p className="mb-3 font-mono text-[0.72rem] tracking-[0.16em] text-ink-3 uppercase">
              {eyebrow}
            </p>
            <h1 className="font-display text-[clamp(1.8rem,5vw,2.3rem)] leading-[1.08] font-semibold tracking-[-0.03em] text-balance">
              {title}
            </h1>
            <p className="mt-3 text-[0.95rem] leading-relaxed text-ink-2">{lede}</p>
          </div>

          <section className="rounded-[12px] border border-line bg-surface p-5 shadow-[var(--shadow-1)] sm:p-6">
            {children}
          </section>

          <div className="mt-5 text-center text-[0.82rem] text-ink-3">{footer}</div>

          <p className="mt-8 border-t border-line pt-4 font-mono text-[0.68rem] leading-relaxed tracking-[0.04em] text-ink-3">
            登录即表示你同意本站的社区规范 ·{" "}
            <Link href="/" className="text-ink-2 underline-offset-2 hover:text-ink hover:underline">
              返回首页
            </Link>
          </p>
        </div>
      </main>
    </>
  );
}
