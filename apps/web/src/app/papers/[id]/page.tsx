import type { Metadata } from "next";
import Link from "next/link";
import { notFound } from "next/navigation";
import { PaperFileSection } from "@/components/PaperFileSection";
import { SiteHeader } from "@/components/SiteHeader";
import { fetchPaper } from "@/lib/server-api";
import { absoluteUrl, jsonLd, truncate } from "@/lib/seo";

/**
 * 论文详情页（Server Component，PRD §7.4）。
 *
 * 页面骨架、标题、作者、摘要全部在服务端渲染，爬虫无需执行 JS 即可拿到正文；
 * 交互部分（登录态下的上传、PDF 预览）拆到 {@link PaperFileSection} 客户端组件。
 *
 * 元数据与 JSON-LD 让搜索引擎把本页识别为一篇 ScholarlyArticle。
 */

type PageProps = { params: Promise<{ id: string }> };

export async function generateMetadata({ params }: PageProps): Promise<Metadata> {
  const { id } = await params;
  const paper = await fetchPaper(id);

  // 论文不存在时交给页面 notFound()；这里只给一个兜底标题。
  if (!paper) {
    return { title: "论文", alternates: { canonical: absoluteUrl(`/papers/${id}`) } };
  }

  const title = paper.title || "论文";
  const description =
    truncate(paper.abstract, 160) ||
    `${paper.title}${paper.venue ? ` · ${paper.venue}` : ""} — 论文详情`;
  const canonical = absoluteUrl(`/papers/${paper.id}`);
  const authors = paper.authors.map((a) => a.name).filter(Boolean);

  return {
    title,
    description,
    alternates: { canonical },
    openGraph: {
      type: "article",
      title,
      description,
      url: canonical,
      authors: authors.length > 0 ? authors : undefined,
      publishedTime: paper.createdAt || undefined,
    },
    twitter: {
      card: "summary",
      title,
      description,
    },
  };
}

export default async function PaperDetailPage({ params }: PageProps) {
  const { id } = await params;
  const paper = await fetchPaper(id);
  if (!paper) notFound();

  const authorNames = paper.authors.map((a) => a.name).filter(Boolean);

  // JSON-LD：ScholarlyArticle。identifier 优先 DOI，其次 arXiv / OpenReview。
  const identifier = paper.doi
    ? { "@type": "PropertyValue", propertyID: "DOI", value: paper.doi }
    : paper.arxivId
      ? { "@type": "PropertyValue", propertyID: "arXiv", value: paper.arxivId }
      : paper.openreviewId
        ? { "@type": "PropertyValue", propertyID: "OpenReview", value: paper.openreviewId }
        : undefined;

  const structuredData = {
    "@context": "https://schema.org",
    "@type": "ScholarlyArticle",
    headline: paper.title,
    name: paper.title,
    abstract: paper.abstract || undefined,
    author:
      authorNames.length > 0
        ? authorNames.map((name) => ({ "@type": "Person", name }))
        : undefined,
    // 学术出版日期优先用发表年份；没有年份才退回入库时间，避免把「入库时刻」
    // 当成论文发表日。
    datePublished:
      paper.publishedYear > 0 ? String(paper.publishedYear) : paper.createdAt || undefined,
    identifier,
    isPartOf: paper.venue ? { "@type": "Periodical", name: paper.venue } : undefined,
    url: absoluteUrl(`/papers/${paper.id}`),
    ...(paper.createdAt ? { dateModified: paper.createdAt } : {}),
  };

  return (
    <>
      <SiteHeader />
      <main className="mx-auto w-full max-w-3xl px-5 py-10">
        <Link
          href="/papers"
          className="mb-6 inline-flex items-center gap-1.5 text-[0.82rem] text-ink-2 hover:text-ink"
        >
          ← 返回论文库
        </Link>

        <article>
          <p className="mb-3 font-mono text-[0.72rem] tracking-[0.16em] text-ink-3 uppercase">
            论文 · Paper
          </p>
          <h1 className="mb-3 font-display text-[clamp(1.6rem,4vw,2.2rem)] leading-[1.15] font-semibold tracking-[-0.02em] text-balance">
            {paper.title}
          </h1>

          <p className="mb-4 text-[0.9rem] text-ink-2">
            {authorNames.join(", ") || "未知作者"}
          </p>

          <dl className="mb-7 flex flex-wrap gap-x-6 gap-y-2 border-y border-line py-3 font-mono text-[0.74rem] text-ink-3">
            {paper.doi && (
              <div>
                <dt className="inline">DOI </dt>
                <dd className="inline text-ink-2">{paper.doi}</dd>
              </div>
            )}
            {paper.arxivId && (
              <div>
                <dt className="inline">arXiv </dt>
                <dd className="inline text-ink-2">{paper.arxivId}</dd>
              </div>
            )}
            {paper.venue && (
              <div>
                <dt className="inline">出处 </dt>
                <dd className="inline text-ink-2">{paper.venue}</dd>
              </div>
            )}
            {paper.publishedYear > 0 && (
              <div>
                <dt className="inline">年份 </dt>
                <dd className="inline text-ink-2">{paper.publishedYear}</dd>
              </div>
            )}
            {paper.citationCount > 0 && (
              <div>
                <dt className="inline">引用 </dt>
                <dd className="inline text-ink-2 tabular-nums">{paper.citationCount}</dd>
              </div>
            )}
          </dl>

          <PaperFileSection pdfFileId={paper.pdfFileId} />

          {paper.abstract ? (
            <section>
              <h2 className="mb-2.5 font-mono text-[0.7rem] font-semibold tracking-[0.1em] text-ink-3 uppercase">
                摘要
              </h2>
              <p className="text-[0.95rem] leading-[1.75] text-ink-2">{paper.abstract}</p>
            </section>
          ) : (
            <p className="text-[0.88rem] text-ink-3">该论文暂无摘要。</p>
          )}
        </article>
      </main>

      <script
        type="application/ld+json"
        dangerouslySetInnerHTML={jsonLd(structuredData)}
      />
    </>
  );
}
