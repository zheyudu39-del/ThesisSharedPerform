import type { Metadata } from "next";
import Link from "next/link";
import { SiteHeader } from "@/components/SiteHeader";
import { listTopTags, type TagDTO } from "@/lib/posts";
import { absoluteUrl } from "@/lib/seo";

/**
 * 话题列表页（PRD F-07）。
 *
 * 按公开帖子数排序，只列至少有一条公开帖子的标签——空标签列出来
 * 只会让人点进去看到「还没有帖子」。服务端直接渲染，便于索引。
 */

export const metadata: Metadata = {
  title: "话题",
  description: "按话题浏览平台上的公开讨论。",
  alternates: { canonical: absoluteUrl("/tags") },
  openGraph: { type: "website", title: "话题 · Thesis Shared", url: absoluteUrl("/tags") },
};

export default async function TagsPage() {
  let tags: TagDTO[] = [];
  try {
    tags = await listTopTags(60);
  } catch {
    // 服务端渲染时 API 不可用：显示空状态而不是整页 500。
    tags = [];
  }

  return (
    <>
      <SiteHeader />
      <main className="mx-auto w-full max-w-[760px] px-4 py-8 sm:px-6">
        <header className="mb-6">
          <h1 className="font-display text-[clamp(1.6rem,4vw,2.2rem)] leading-tight font-semibold tracking-[-0.02em]">
            话题
          </h1>
          <p className="mt-2 text-[0.9rem] text-ink-2">
            按公开帖子数排序。发帖时带上的标签会出现在这里。
          </p>
        </header>

        {tags.length === 0 ? (
          <p className="rounded-[12px] border border-dashed border-line-strong bg-surface px-5 py-8 text-center text-[0.86rem] leading-relaxed text-ink-2">
            还没有任何公开话题。发帖时带上标签即可创建话题。
          </p>
        ) : (
          <ul className="flex flex-wrap gap-2">
            {tags.map((tag) => (
              <li key={tag.slug}>
                <Link
                  href={`/tags/${encodeURIComponent(tag.slug)}`}
                  className="inline-flex items-baseline gap-1.5 rounded-full border border-line bg-surface px-3.5 py-1.5 text-[0.85rem] text-ink-2 transition-colors hover:border-seal/50 hover:bg-seal-wash hover:text-seal"
                >
                  <span className="font-mono text-seal">#</span>
                  {tag.name}
                  <span className="font-mono text-[0.7rem] text-ink-3 tabular-nums">
                    {tag.postCount}
                  </span>
                </Link>
              </li>
            ))}
          </ul>
        )}
      </main>
    </>
  );
}
