import Link from "next/link";
import { SiteHeader } from "@/components/SiteHeader";

/**
 * 全局 404。
 *
 * 论文 / 用户 / 帖子详情在数据为 null 时调用 notFound()，渲染本页。
 * 必须是 Server Component：notFound() 需要在渲染阶段抛错并由 Next 处理。
 */
export default function NotFound() {
  return (
    <>
      <SiteHeader />
      <main className="mx-auto w-full max-w-3xl px-5 py-16">
        <div className="rounded-[12px] border border-line bg-surface p-8 text-center shadow-[var(--shadow-1)]">
          <p className="font-mono text-[0.68rem] tracking-[0.12em] text-ink-3 uppercase">
            404 · 未找到
          </p>
          <h1 className="mt-3 font-display text-[clamp(1.5rem,4vw,2rem)] leading-tight font-semibold tracking-[-0.02em]">
            这个页面不存在
          </h1>
          <p className="mx-auto mt-3 max-w-[42ch] text-[0.9rem] leading-relaxed text-ink-2">
            链接可能已失效，或者内容已被删除、设为私密。
          </p>
          <div className="mt-6 flex flex-wrap items-center justify-center gap-2">
            <Link
              href="/"
              className="rounded-[6px] bg-seal px-4 py-1.5 text-[0.85rem] font-semibold text-white hover:bg-seal-deep"
            >
              返回信息流
            </Link>
            <Link
              href="/papers"
              className="rounded-[6px] border border-line-strong px-4 py-1.5 text-[0.85rem] font-semibold text-ink hover:border-ink-3"
            >
              论文库
            </Link>
          </div>
        </div>
      </main>
    </>
  );
}
