/**
 * 文档 REST 客户端。
 * 走统一 apiFetch（Cookie 会话 + CSRF 头）。
 */

import { apiFetch, apiFetchRaw } from "@/lib/http";

export type DocRole = "owner" | "editor" | "viewer";

export type DocumentDTO = {
  id: string;
  groupId: string;
  groupSlug: string;
  title: string;
  status: "draft" | "active" | "archived";
  defaultRole: DocRole;
  wordCount: number;
  latestSnapshot: number;
  createdAt: string;
  updatedAt: string;
  createdBy: { id: string; name: string; handle: string };
};

export type SnapshotDTO = {
  version: number;
  label: string;
  createdBy: string;
  createdAt: string;
  sizeBytes: number;
};

export type AnnotationDTO = {
  id: string;
  authorId: string;
  authorName: string;
  anchor: unknown;
  quotedText: string;
  content: string;
  resolvedAt: string | null;
  createdAt: string;
};

export type CollabToken = {
  token: string;
  wsUrl: string;
};

/* -------------------------------------------------------------------------- */
/* 文档                                                                        */
/* -------------------------------------------------------------------------- */

export async function getDocument(id: string): Promise<{ document: DocumentDTO; myRole: DocRole | null }> {
  return apiFetch(`/documents/${encodeURIComponent(id)}`);
}

export async function listGroupDocuments(
  slug: string,
  cursor?: string | null,
  limit = 20,
): Promise<{ documents: DocumentDTO[]; nextCursor: string | null }> {
  const params = new URLSearchParams({ limit: String(limit) });
  if (cursor) params.set("cursor", cursor);
  return apiFetch(`/groups/${encodeURIComponent(slug)}/documents?${params}`);
}

export async function createDocument(slug: string, title: string): Promise<{ document: DocumentDTO }> {
  return apiFetch(`/groups/${encodeURIComponent(slug)}/documents`, {
    method: "POST",
    body: { title },
  });
}

export async function updateDocument(
  id: string,
  patch: { title?: string; status?: string },
): Promise<{ document: DocumentDTO }> {
  return apiFetch(`/documents/${encodeURIComponent(id)}`, { method: "PATCH", body: patch });
}

export async function deleteDocument(id: string): Promise<void> {
  await apiFetch(`/documents/${encodeURIComponent(id)}`, { method: "DELETE" });
}

/* -------------------------------------------------------------------------- */
/* 快照                                                                        */
/* -------------------------------------------------------------------------- */

export async function listSnapshots(id: string): Promise<{ snapshots: SnapshotDTO[] }> {
  return apiFetch(`/documents/${encodeURIComponent(id)}/snapshots`);
}

export async function createSnapshot(id: string, label?: string): Promise<{ snapshot: SnapshotDTO }> {
  return apiFetch(`/documents/${encodeURIComponent(id)}/snapshots`, {
    method: "POST",
    body: { label: label ?? "" },
  });
}

/* -------------------------------------------------------------------------- */
/* 批注                                                                        */
/* -------------------------------------------------------------------------- */

export async function listAnnotations(id: string): Promise<{ annotations: AnnotationDTO[] }> {
  return apiFetch(`/documents/${encodeURIComponent(id)}/annotations`);
}

export async function createAnnotation(
  id: string,
  input: { anchor: unknown; quotedText?: string; content: string },
): Promise<{ annotation: AnnotationDTO }> {
  return apiFetch(`/documents/${encodeURIComponent(id)}/annotations`, {
    method: "POST",
    body: input,
  });
}

export async function updateAnnotation(
  id: string,
  aid: string,
  patch: { content?: string; resolved?: boolean },
): Promise<{ annotation: AnnotationDTO }> {
  return apiFetch(`/documents/${encodeURIComponent(id)}/annotations/${encodeURIComponent(aid)}`, {
    method: "PATCH",
    body: patch,
  });
}

/* -------------------------------------------------------------------------- */
/* 协同令牌                                                                    */
/* -------------------------------------------------------------------------- */

/**
 * 取得连接协同服务所需的短期令牌。
 *
 * 会话本身在 HttpOnly Cookie 里，前端读不到，因此由服务端在确认
 * 「当前用户可访问该文档」后签发一个专用 JWT。
 */
export async function getCollabToken(id: string): Promise<CollabToken> {
  return apiFetch(`/documents/${encodeURIComponent(id)}/collab-token`);
}

/* -------------------------------------------------------------------------- */
/* 导出                                                                        */
/* -------------------------------------------------------------------------- */

export type ExportFormat = "docx" | "md" | "tex";

export const EXPORT_FORMAT_LABEL: Record<ExportFormat, string> = {
  docx: "Word (.docx)",
  md: "Markdown (.md)",
  tex: "LaTeX (.tex)",
};

/**
 * 导出文档。
 *
 * 正文由调用方从 TipTap 取出 HTML 后传入：正文是 Yjs 二进制（CRDT），
 * 服务端没有解析实现，浏览器里的编辑器实例是最可靠的序列化入口。
 * 服务端只做格式转换与权限校验（要求 editor 及以上）。
 *
 * 直接触发浏览器下载：用 Blob + 临时 <a download>，
 * 因为响应带 Content-Disposition，普通 fetch 拿不到浏览器的下载行为。
 */
export async function exportDocument(
  id: string,
  html: string,
  format: ExportFormat,
  title: string,
): Promise<void> {
  const response = await apiFetchRaw(`/documents/${encodeURIComponent(id)}/export`, {
    method: "POST",
    body: { html, format },
  });

  const blob = await response.blob();
  const filename = `${sanitizeFilename(title)}.${format}`;
  const url = URL.createObjectURL(blob);
  try {
    const link = document.createElement("a");
    link.href = url;
    link.download = filename;
    document.body.appendChild(link);
    link.click();
    link.remove();
  } finally {
    // 立刻回收：下载已由浏览器接管，保留 URL 只会泄漏内存。
    URL.revokeObjectURL(url);
  }
}

/** 与后端 safeFilename 保持一致的保守清洗，避免文件名里出现路径分隔符。 */
function sanitizeFilename(title: string): string {
  const cleaned = title
    .trim()
    .replace(/[\/:*?"<>|]/g, "")
    .slice(0, 60)
    .trim();
  return cleaned || "document";
}
