"use client";

import Link from "next/link";
import { useParams } from "next/navigation";
import { useEffect, useState } from "react";
import { SiteHeader } from "@/components/SiteHeader";
import { getPaper, type PaperDTO } from "@/lib/papers";

export default function PaperDetailPage() {
  const params = useParams<{ id: string }>();
  const [paper, setPaper] = useState<PaperDTO | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    let cancelled = false;
    async function load() {
      try {
        const data = await getPaper(params.id);
        if (!cancelled) setPaper(data);
      } catch (err) {
        if (!cancelled) setError(err instanceof Error ? err.message : "加载失败");
      } finally {
        if (!cancelled) setLoading(false);
      }
    }
    void load();
    return () => {
      cancelled = true;
    };
  }, [params.id]);

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

        {loading && (
          <div className="flex flex-col gap-4" aria-hidden>
            <div className="h-8 w-3/4 animate-pulse rounded-[6px] bg-surface-2" />
            <div className="h-4 w-1/2 animate-pulse rounded-[6px] bg-surface-2" />
            <div className="h-40 animate-pulse rounded-[12px] bg-surface-2" />
          </div>
        )}

        {error && (
          <div className="rounded-[12px] border border-line bg-surface p-6">
            <p className="text-[0.9rem] text-ink-2">{error}</p>
          </div>
        )}

        {paper && (
          <article>
            <p className="mb-3 font-mono text-[0.72rem] tracking-[0.16em] text-ink-3 uppercase">
              论文 · Paper
            </p>
            <h1 className="mb-3 font-display text-[clamp(1.6rem,4vw,2.2rem)] leading-[1.15] font-semibold tracking-[-0.02em] text-balance">
              {paper.title}
            </h1>

            <p className="mb-4 text-[0.9rem] text-ink-2">
              {paper.authors.map((a) => a.name).join(", ") || "未知作者"}
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
        )}
      </main>
    </>
  );
}
