"use client";

import Link from "next/link";
import { useParams } from "next/navigation";
import { useCallback, useEffect, useState } from "react";
import { FileUploader } from "@/components/FileUploader";
import { PdfPreview } from "@/components/PdfPreview";
import { SiteHeader } from "@/components/SiteHeader";
import { useAuth } from "@/components/AuthProvider";
import { getPaper, type PaperDTO } from "@/lib/papers";
import type { FileDTO } from "@/lib/files";

/**
 * 论文详情页。
 *
 * 布局：
 * - 标题 / 作者 / 元数据 / 摘要（左栏，窄屏单列）；
 * - 关联了 PDF 时，正文上方给一个大尺寸内联预览（maxHeight 720），
 *   并保留「在独立页打开」入口——这里的独立页就是本页自身，所以改成
 *   锚点定位到预览区；
 * - 未关联时，登录用户可上传 PDF 作为该论文的全文。
 *
 * 说明：本期没有「把文件挂到 Paper 上」的接口（契约明确不新增 post_attachments），
 * 因此上传成功后只在本地展示预览，并明确提示文件已上传但尚未写入 Paper。
 * 待后端补 `PATCH /papers/{id}` 或 `POST /papers/{id}/file` 后，把
 * {@link handleUploaded} 里的本地状态替换成接口调用即可。
 */
export default function PaperDetailPage() {
  const params = useParams<{ id: string }>();
  const { status } = useAuth();
  const [paper, setPaper] = useState<PaperDTO | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const [localFile, setLocalFile] = useState<FileDTO | null>(null);

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

  const handleUploaded = useCallback((file: FileDTO) => {
    // 本地挂上预览，让用户立刻看到效果；刷新后是否仍关联取决于后端是否落库。
    setLocalFile(file);
  }, []);

  const fileId = paper?.pdfFileId ?? (localFile ? localFile.id : null);

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

            {/* 全文预览 */}
            <section id="preview" className="mb-8">
              <h2 className="mb-2.5 font-mono text-[0.7rem] font-semibold tracking-[0.1em] text-ink-3 uppercase">
                全文预览
              </h2>

              {fileId ? (
                <PdfPreview
                  key={fileId}
                  fileId={fileId}
                  initialFile={localFile && localFile.id === fileId ? localFile : null}
                  maxHeight={720}
                />
              ) : status === "authenticated" ? (
                <div className="rounded-[12px] border border-dashed border-line-strong bg-surface p-4">
                  <p className="mb-3 text-[0.84rem] leading-relaxed text-ink-2">
                    这篇论文还没有全文。上传 PDF 后即可在卡片与信息流内联预览。
                  </p>
                  <FileUploader
                    onUploaded={handleUploaded}
                    hint="PDF 最佳；Word / PPT 会先转码再预览 · 单文件 ≤ 100MB"
                  />
                </div>
              ) : (
                <p className="rounded-[12px] border border-dashed border-line-strong bg-surface px-5 py-6 text-center text-[0.86rem] text-ink-2">
                  <Link href="/login" className="font-semibold text-seal hover:underline">
                    登录
                  </Link>
                  后上传全文并预览。
                </p>
              )}

              {localFile && !paper.pdfFileId && (
                <p className="mt-2 rounded-[6px] border border-warn/40 bg-warn-wash px-3 py-2 text-[0.78rem] leading-relaxed text-warn">
                  文件已上传（{localFile.originalName}），但本期还没有把文件写回论文记录的接口，
                  因此刷新后需要重新上传。
                </p>
              )}
            </section>

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
