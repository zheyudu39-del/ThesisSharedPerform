import Link from "next/link";

/**
 * 站点顶栏：品牌字标 + 主导航 + 主题切换位。
 * 印字方块是「墨与印」的视觉锚点，全站唯一固定出现的高饱和色。
 */
export function SiteHeader() {
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
          <Link href="/" className="rounded-[6px] px-3 py-1.5 hover:bg-surface-2 hover:text-ink">
            热门
          </Link>
          <Link href="/" className="rounded-[6px] px-3 py-1.5 hover:bg-surface-2 hover:text-ink">
            群组
          </Link>
        </nav>

        <div className="ml-auto flex items-center gap-2">
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
        </div>
      </div>
    </header>
  );
}
