"use client";

import Link from "next/link";
import { useCallback, useState } from "react";
import { useRouter } from "next/navigation";
import { useAuth } from "@/components/AuthProvider";
import { FileUploader } from "@/components/FileUploader";
import { PdfPreview } from "@/components/PdfPreview";
import type { FileDTO } from "@/lib/files";
import { setPaperPDFFile } from "@/lib/papers";

/**
 * 论文详情页的「全文预览 / 上传」区块。
 *
 * 从页面里拆出来，是因为它依赖登录态（useAuth）与文件上传进度，
 * 必须留在客户端；论文标题、作者、摘要等静态内容则由 Server Component 直出。
 *
 * 上传是两步：先把文件写进 files 表（FileUploader 内部完成三步直传），
 * 再用 PATCH /papers/{id}/pdf 把它挂到论文记录上。少了第二步，刷新后
 * pdf_file_id 仍为空，页面又变回「还没有全文」——那正是之前的 bug。
 */
export function PaperFileSection({
  paperId,
  pdfFileId,
}: {
  paperId: string;
  pdfFileId: string | null;
}) {
  const { status } = useAuth();
  const router = useRouter();
  const [localFile, setLocalFile] = useState<FileDTO | null>(null);
  const [attached, setAttached] = useState<string | null>(pdfFileId);
  const [attachError, setAttachError] = useState<string | null>(null);
  const [attaching, setAttaching] = useState(false);

  const handleUploaded = useCallback(
    async (file: FileDTO) => {
      // 先本地展示预览：挂载请求失败时用户至少能看见刚传上去的内容。
      setLocalFile(file);
      setAttachError(null);
      setAttaching(true);
      try {
        await setPaperPDFFile(paperId, file.id);
        setAttached(file.id);
        // 服务端数据已变：刷新让论文页重新渲染，标题/摘要区也能反映新状态。
        router.refresh();
      } catch (err) {
        // 如实告诉用户文件传上去了但没挂上，而不是假装成功。
        setAttachError(
          err instanceof Error && err.message
            ? `文件已上传，但关联到论文失败：${err.message}`
            : "文件已上传，但关联到论文失败，请稍后重试。",
        );
      } finally {
        setAttaching(false);
      }
    },
    [paperId, router],
  );

  const fileId = attached ?? (localFile ? localFile.id : null);

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
            disabled={attaching}
            hint="PDF 最佳；Word / LaTeX 也可以挂在这里 · 单文件 ≤ 100MB"
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

      {attaching && (
        <p className="mt-2 text-[0.78rem] text-ink-3" role="status">
          正在把文件关联到论文…
        </p>
      )}

      {attachError && (
        <p className="mt-2 rounded-[6px] border border-warn/40 bg-warn-wash px-3 py-2 text-[0.78rem] leading-relaxed text-warn">
          {attachError}
        </p>
      )}
    </section>
  );
}
