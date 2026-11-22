"use client";

import { useId, useState } from "react";
import { formatBytes, type FileDTO, type FileKind } from "@/lib/files";
import { FilePreview } from "@/components/FilePreview";
import { SaveToGroupFile } from "@/components/SaveToGroupFile";
import type { PostAttachment } from "@/lib/posts";

/**
 * 帖子附件的展示块：文件名 + 大小 + 可展开的站内预览（PRD V-04）。
 *
 * 为什么默认折叠：信息流里一张 PDF 完整展开会占掉整屏，滚动体验会被打断。
 * 折叠状态下面板不挂载，也就不会去取预签名地址或拉文件内容。
 */

const FILE_KINDS: FileKind[] = [
  "pdf", "office_doc", "office_slide", "office_sheet", "image",
  "markdown", "code", "latex", "video", "archive", "other",
];

function toFileKind(kind: string): FileKind {
  return (FILE_KINDS as string[]).includes(kind) ? (kind as FileKind) : "other";
}

/** 附件 → FileDTO：FilePreview 需要完整字段，附件只带了一部分。 */
function toFileDTO(attachment: PostAttachment): FileDTO {
  return {
    id: attachment.fileId,
    originalName: attachment.name,
    mime: attachment.mime,
    kind: toFileKind(attachment.kind),
    sizeBytes: attachment.sizeBytes,
    status: "ready",
    pageCount: attachment.pageCount,
    createdAt: "",
    coverUrl: null,
    errorReason: null,
  };
}

/** 只有这几类值得展开预览；压缩包、视频等仍保持「只列文件名」。 */
function canPreview(kind: string, mime: string): boolean {
  if (kind === "pdf" || kind === "image" || kind === "markdown" || kind === "latex" || kind === "code") {
    return true;
  }
  if (kind === "office_doc" || kind === "office_slide" || kind === "office_sheet") return true;
  return mime.startsWith("text/") || mime.startsWith("image/");
}

export function AttachmentBlock({
  file,
  groupSlug,
}: {
  file: PostAttachment;
  groupSlug?: string;
}) {
  const [open, setOpen] = useState(false);
  const panelId = useId();
  const previewable = canPreview(file.kind, file.mime);

  return (
    <div className="rounded-[6px] border border-line bg-surface-2">
      <div className="flex flex-wrap items-center gap-2 px-3 py-1.5">
        {previewable ? (
          <button
            type="button"
            onClick={() => setOpen((v) => !v)}
            aria-expanded={open}
            aria-controls={panelId}
            className="min-w-0 flex-1 truncate text-left text-[0.82rem] text-ink-2 hover:text-seal hover:underline"
          >
            {file.name}
          </button>
        ) : (
          <span className="min-w-0 flex-1 truncate text-[0.82rem] text-ink-2">{file.name}</span>
        )}
        <span className="shrink-0 font-mono text-[0.68rem] text-ink-3">
          {formatBytes(file.sizeBytes)}
          {file.pageCount > 0 ? ` · ${file.pageCount} 页` : ""}
        </span>
        {previewable && (
          <button
            type="button"
            onClick={() => setOpen((v) => !v)}
            aria-expanded={open}
            aria-controls={panelId}
            className="shrink-0 rounded-[3px] px-1.5 py-0.5 text-[0.7rem] font-semibold text-seal hover:bg-seal-wash"
          >
            {open ? "收起" : "预览"}
          </button>
        )}
        {/* 「存入文件库」只在群组页出现：公开时间线的帖子不属于任何群。 */}
        {groupSlug && <SaveToGroupFile groupSlug={groupSlug} fileId={file.fileId} />}
      </div>

      {open && previewable && (
        <div id={panelId} className="border-t border-line p-2">
          <FilePreview file={toFileDTO(file)} maxHeight={420} />
        </div>
      )}
    </div>
  );
}
