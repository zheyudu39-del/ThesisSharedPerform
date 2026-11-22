"use client";

import { useId, useState } from "react";
import { FilePreview } from "@/components/FilePreview";
import { formatBytes, type FileDTO, type FileKind } from "@/lib/files";
import type { GroupFileDTO } from "@/lib/groupfiles";

/**
 * 群文件库条目的预览容器（PRD V-04 / G-05）。
 *
 * 与 {@link AttachmentBlock} 分开：群文件库多一层 title / 描述 / 标签，
 * 复用同一个 FilePreview，但折叠按钮挂在条目的操作区，
 * 不能把整张条目变成一个按钮。
 */

const FILE_KINDS: FileKind[] = [
  "pdf", "office_doc", "office_slide", "office_sheet", "image",
  "markdown", "code", "latex", "video", "archive", "other",
];

function toFileKind(kind: string): FileKind {
  return (FILE_KINDS as string[]).includes(kind) ? (kind as FileKind) : "other";
}

function canPreview(kind: string, mime: string): boolean {
  if (["pdf", "image", "markdown", "latex", "code", "office_doc", "office_slide", "office_sheet"].includes(kind)) {
    return true;
  }
  return mime.startsWith("text/") || mime.startsWith("image/");
}

export function GroupFilePreview({
  file,
  pageCount,
}: {
  file: GroupFileDTO;
  pageCount: number;
}) {
  const [open, setOpen] = useState(false);
  const panelId = useId();

  if (!canPreview(file.kind, file.mime)) return null;

  const dto: FileDTO = {
    id: file.fileId,
    originalName: file.originalName,
    mime: file.mime,
    kind: toFileKind(file.kind),
    sizeBytes: file.sizeBytes,
    status: "ready",
    pageCount,
    createdAt: "",
    coverUrl: null,
    errorReason: null,
  };

  return (
    <>
      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        aria-expanded={open}
        aria-controls={panelId}
        className="rounded-[3px] border border-line-strong px-2 py-0.5 text-[0.72rem] text-ink-2 hover:border-ink-3 hover:text-seal"
      >
        {open ? "收起预览" : "预览"}
        <span className="sr-only">
          （{formatBytes(file.sizeBytes)}）
        </span>
      </button>
      {open && (
        <div id={panelId} className="mt-3">
          <FilePreview file={dto} maxHeight={520} />
        </div>
      )}
    </>
  );
}
