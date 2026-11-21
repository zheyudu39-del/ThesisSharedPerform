import type { Metadata } from "next";
import Link from "next/link";
import { notFound } from "next/navigation";
import { CollectionItems } from "@/app/collections/[slug]/CollectionItems";
import { SiteHeader } from "@/components/SiteHeader";
import { fetchCollection } from "@/lib/server-api";
import { absoluteUrl, truncate } from "@/lib/seo";

/**
 * 策展集详情（Server Component）。
 *
 * 标题、编者按、条目全部在服务端渲染，爬虫与分享预览无需执行 JS；
 * 编辑操作拆到 {@link CollectionItems} 客户端组件。
 *
 * 注意 canEdit：服务端根据会话算出「当前查看者能否编辑」，
 * 未上架/已下架的集合对非策展人返回 404，因此这里的 notFound() 同时
 * 覆盖「不存在」与「暂未公开」两种情况——不泄露选题是否已存在。
 */

type PageProps = { params: Promise<{ slug: string }> };

export async function generateMetadata({ params }: PageProps): Promise<Metadata> {
  const { slug } = await params;
  const detail = await fetchCollection(slug);

  if (!detail) {
    return { title: "精选", alternates: { canonical: absoluteUrl(`/collections/${slug}`) } };
  }

  const { collection } = detail;
  const description =
    truncate(collection.description, 160) ||
    `${collection.curator.name} 策展的 ${collection.itemCount} 篇内容`;
  const canonical = absoluteUrl(`/collections/${collection.slug}`);

  return {
    title: collection.title,
    description,
    alternates: { canonical },
    openGraph: {
      type: "article",
      title: collection.title,
      description,
      url: canonical,
      publishedTime: collection.startsAt || undefined,
    },
    twitter: { card: "summary", title: collection.title, description },
  };
}

export default async function CollectionPage({ params }: PageProps) {
  const { slug } = await params;
  const detail = await fetchCollection(slug);
  if (!detail) notFound();

  const { collection, items } = detail;

  return (
    <>
      <SiteHeader />

      <main className="mx-auto max-w-3xl px-5 py-10">
        <nav className="mb-6 font-mono text-[0.72rem] text-ink-3">
          <Link href="/collections" className="hover:text-seal">
            精选
          </Link>
          <span aria-hidden> / </span>
          <span className="text-ink-2">{collection.title}</span>
        </nav>

        <header className="mb-8 border-b border-line pb-7">
          <p className="mb-3 font-mono text-[0.72rem] tracking-[0.16em] text-ink-3 uppercase">
            策展集 · {collection.itemCount} 篇
          </p>
          <h1 className="font-display text-[clamp(1.7rem,3.8vw,2.4rem)] leading-[1.1] font-semibold tracking-[-0.03em] text-balance">
            {collection.title}
          </h1>

          {collection.description && (
            <p className="mt-3 max-w-[62ch] text-[0.98rem] leading-relaxed text-ink-2">
              {collection.description}
            </p>
          )}

          <div className="mt-4 flex flex-wrap items-center gap-2 font-mono text-[0.72rem] text-ink-3">
            <Link href={`/u/${collection.curator.handle}`} className="hover:text-seal">
              策展 · {collection.curator.name}
            </Link>
            {collection.curator.verifyType === "influencer" && (
              <span className="rounded-full bg-seal-wash px-1.5 py-px text-seal">大V</span>
            )}
            <span aria-hidden>·</span>
            <span>{formatDate(collection.startsAt)}</span>
            {collection.endsAt && (
              <>
                <span aria-hidden>→</span>
                <span>{formatDate(collection.endsAt)}</span>
              </>
            )}
          </div>
        </header>

        <CollectionItems slug={collection.slug} items={items} canEdit={collection.canEdit} />
      </main>
    </>
  );
}

function formatDate(iso: string): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return "";
  return `${d.getFullYear()}/${d.getMonth() + 1}/${d.getDate()}`;
}
