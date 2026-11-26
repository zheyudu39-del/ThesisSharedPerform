"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import type { FeedStream } from "@/lib/posts";

/**
 * 首页左栏「本期索引」。
 *
 * 存在的理由不是装饰：把横向的三档流切换挪成竖排之后，
 * 中栏就能专心放内容，同时让页面左侧有一个稳定的阅读锚点——
 * 报纸的栏目索引就是这个作用。
 *
 * 只在 lg 以上渲染；窄屏仍用横排 Tab（见 page.tsx）。
 */

const STREAM_ITEMS: { key: FeedStream; label: string; note: string }[] = [
  { key: "following", label: "关注流", note: "按人筛选" },
  { key: "all", label: "全站流", note: "全部公开" },
  { key: "hot", label: "热门流", note: "按热度" },
];

const SECTIONS: { href: string; label: string; note: string }[] = [
  { href: "/papers", label: "论文库", note: "DOI / arXiv" },
  { href: "/groups", label: "群组", note: "共读与协作" },
  { href: "/collections", label: "精选", note: "人工策展" },
  { href: "/tags", label: "话题", note: "按标签浏览" },
  { href: "/search", label: "搜索", note: "全站检索" },
];

export function HomeIndex({
  stream,
  onStreamChange,
}: {
  stream: FeedStream;
  onStreamChange: (next: FeedStream) => void;
}) {
  const pathname = usePathname();

  return (
    <nav aria-label="站点索引" className="flex flex-col gap-7">
      <div>
        <h2 className="mb-2.5 font-mono text-[0.66rem] font-semibold tracking-[0.12em] text-ink-3 uppercase">
          信息流
        </h2>
        <ul className="flex flex-col">
          {STREAM_ITEMS.map((item) => {
            const active = stream === item.key;
            return (
              <li key={item.key} className="relative">
                {/* 朱印刻度：全站唯一的「当前位置」标记，与后台侧栏同一套语言。 */}
                <span
                  aria-hidden
                  className={`absolute top-1.5 bottom-1.5 left-0 w-[2px] rounded-full bg-seal transition-transform duration-150 ${
                    active ? "scale-y-100" : "scale-y-0"
                  }`}
                  style={{ transformOrigin: "center" }}
                />
                <button
                  type="button"
                  onClick={() => onStreamChange(item.key)}
                  aria-current={active ? "true" : undefined}
                  className={`block w-full rounded-[4px] py-1.5 pr-2 pl-3 text-left transition-colors ${
                    active
                      ? "text-[0.88rem] font-semibold text-ink"
                      : "text-[0.86rem] text-ink-2 hover:bg-surface-2 hover:text-ink"
                  }`}
                >
                  {item.label}
                  <span className="mt-0.5 block font-mono text-[0.64rem] font-normal text-ink-3">
                    {item.note}
                  </span>
                </button>
              </li>
            );
          })}
        </ul>
      </div>

      <div>
        <h2 className="mb-2.5 font-mono text-[0.66rem] font-semibold tracking-[0.12em] text-ink-3 uppercase">
          分区
        </h2>
        <ul className="flex flex-col">
          {SECTIONS.map((item) => {
            const active = pathname === item.href;
            return (
              <li key={item.href}>
                <Link
                  href={item.href}
                  aria-current={active ? "page" : undefined}
                  className={`block rounded-[4px] px-3 py-1.5 transition-colors ${
                    active
                      ? "bg-surface-2 text-[0.86rem] font-semibold text-ink"
                      : "text-[0.86rem] text-ink-2 hover:bg-surface-2 hover:text-ink"
                  }`}
                >
                  {item.label}
                  <span className="mt-0.5 block font-mono text-[0.64rem] text-ink-3">
                    {item.note}
                  </span>
                </Link>
              </li>
            );
          })}
        </ul>
      </div>
    </nav>
  );
}
