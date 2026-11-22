import type { Metadata } from "next";
import { notFound } from "next/navigation";
import { SiteHeader } from "@/components/SiteHeader";
import { TagFeed } from "@/app/tags/[slug]/TagFeed";
import { getTag } from "@/lib/posts";
import { absoluteUrl } from "@/lib/seo";

/**
 * 话题页（PRD F-07）。
 *
 * 标签此前在帖子卡片上只是不可点击的文字——发帖时能带标签，但没有任何
 * 页面能按标签浏览。这里补上：按标签聚合公开帖子，SSR 输出标题与描述
 * 供搜索引擎索引，列表本身交给客户端组件分页。
 */

type PageProps = { params: Promise<{ slug: string }> };

function normalizeSlug(raw: string): string {
  try {
    return decodeURIComponent(raw).toLowerCase();
  } catch {
    return raw.toLowerCase();
  }
}

export async function generateMetadata({ params }: PageProps): Promise<Metadata> {
  const slug = normalizeSlug((await params).slug);
  const canonical = absoluteUrl(`/tags/${encodeURIComponent(slug)}`);
  try {
    const tag = await getTag(slug);
    const title = `#${tag.name}`;
    const description = `「${tag.name}」话题下有 ${tag.postCount} 条公开讨论。`;
    return {
      title,
      description,
      alternates: { canonical },
      openGraph: { type: "website", title: `${title} · Thesis Shared`, description, url: canonical },
      twitter: { card: "summary", title, description },
    };
  } catch {
    return { title: `#${slug}`, alternates: { canonical } };
  }
}

export default async function TagPage({ params }: PageProps) {
  const slug = normalizeSlug((await params).slug);
  let tag;
  try {
    tag = await getTag(slug);
  } catch {
    notFound();
  }

  return (
    <>
      <SiteHeader />
      <main className="mx-auto w-full max-w-[760px] px-4 py-8 sm:px-6">
        <header className="mb-6">
          <p className="mb-1 font-mono text-[0.7rem] tracking-[0.1em] text-ink-3 uppercase">
            话题
          </p>
          <h1 className="font-display text-[clamp(1.6rem,4vw,2.2rem)] leading-tight font-semibold tracking-[-0.02em]">
            <span className="font-mono text-seal">#</span>
            {tag.name}
          </h1>
          <p className="mt-2 text-[0.88rem] text-ink-2">
            {tag.postCount > 0 ? `共 ${tag.postCount} 条公开讨论` : "这个话题下还没有公开帖子"}
          </p>
        </header>

        <TagFeed slug={slug} />
      </main>
    </>
  );
}
