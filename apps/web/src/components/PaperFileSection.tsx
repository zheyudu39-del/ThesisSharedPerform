"use client";

import Link from "next/link";
import { useCallback, useState } from "react";
import { useAuth } from "@/components/AuthProvider";
import { FileUploader } from "@/components/FileUploader";
import { PdfPreview } from "@/components/PdfPreview";
import type { FileDTO } from "@/lib/files";

/**
 * 论文详情页的「全文预览 / 上传」区块。
 *
 * 从页面里拆出来，是因为它依赖登录态（useAuth）与文件上传进度，
 * 必须留在客户端；论文标题、作者、摘要等静态内容则由 Server Component 直出。
 *
 * 说明：本期没有「把文件挂到 Paper 上」的接口（契约明确不新增 post_attachments），
 * 因此上传成功后只在本地展示预览，并明确提示文件已上传但尚未写入 Paper。
 */
export function PaperFileSection({ pdfFileId }: { pdfFileId: string | null }) {
  const { status } = useAuth();
  const [localFile, setLocalFile] = useState<FileDTO | null>(null);

  const handleUploaded = useCallback((file: FileDTO) => {
    // 本地挂上预览，让用户立刻看到效果；刷新后是否仍关联取决于后端是否落库。
    setLocalFile(file);
  }, []);

  const fileId = pdfFileId ?? (localFile ? localFile.id : null);

  return (
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

      {localFile && !pdfFileId && (
        <p className="mt-2 rounded-[6px] border border-warn/40 bg-warn-wash px-3 py-2 text-[0.78rem] leading-relaxed text-warn">
          文件已上传（{localFile.originalName}），但本期还没有把文件写回论文记录的接口，
          因此刷新后需要重新上传。
        </p>
      )}
    </section>
  );
}
