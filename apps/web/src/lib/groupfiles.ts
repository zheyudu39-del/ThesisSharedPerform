/**
 * 群文件库 API 客户端（PRD G-05）。
 *
 * 文件库保存的是「指向已有文件的引用」而不是副本：存入时只提交 fileId，
 * 文件本体仍在上传链路里管理。因此这里的类型既带文件元数据
 * （originalName / mime / sizeBytes / pageCount），也带文件库条目元数据
 * （title / description / tags / 存入者）。
 *
 * 权限：全部接口仅群成员可用；删除限存入者本人或 admin/owner。
 * 错误统一抛 {@link ApiError}，页面直接展示 message。
 */

import { ApiError, apiFetch } from "@/lib/http";

/* -------------------------------------------------------------------------- */
/* 类型                                                                        */
/* -------------------------------------------------------------------------- */

export type GroupFileAuthorDTO = {
  id: string;
  name: string;
  handle: string;
};

export type GroupFileDTO = {
  id: string;
  groupId: string;
  fileId: string;
  /** 来源帖子；不是从帖子存入时为 null。 */
  postId: string | null;
  title: string;
  description: string;
  tags: string[];
  originalName: string;
  mime: string;
  kind: string;
  sizeBytes: number;
  pageCount: number;
  addedBy: GroupFileAuthorDTO;
  createdAt: string;
};

export type GroupFilePage = {
  files: GroupFileDTO[];
  nextCursor: string | null;
};

export type SaveGroupFileInput = {
  fileId: string;
  title?: string;
  description?: string;
  tags?: string[];
};

export const GROUP_FILE_LIMITS = {
  titleMax: 255,
  descriptionMax: 500,
  tagMax: 32,
  tagsLimit: 10,
} as const;

/* -------------------------------------------------------------------------- */
/* 解析                                                                        */
/* -------------------------------------------------------------------------- */

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function str(value: unknown): string {
  return typeof value === "string" ? value : "";
}

function num(value: unknown): number {
  return typeof value === "number" && Number.isFinite(value) ? value : 0;
}

function invalidResponse(): ApiError {
  return new ApiError("invalid_response", "服务器返回了无法识别的响应。", 0);
}

function normalizeAuthor(raw: unknown): GroupFileAuthorDTO {
  if (!isRecord(raw)) return { id: "", name: "未知用户", handle: "" };
  return {
    id: str(raw.id),
    name: str(raw.name) || "未知用户",
    handle: str(raw.handle),
  };
}

function normalizeGroupFile(raw: unknown): GroupFileDTO | null {
  if (!isRecord(raw)) return null;
  const id = str(raw.id);
  const fileId = str(raw.fileId);
  if (!id || !fileId) return null;
  const tags = Array.isArray(raw.tags)
    ? raw.tags.filter((t): t is string => typeof t === "string" && t.trim() !== "")
    : [];
  const postId = str(raw.postId);
  return {
    id,
    groupId: str(raw.groupId),
    fileId,
    postId: postId || null,
    title: str(raw.title) || str(raw.originalName),
    description: str(raw.description),
    tags,
    originalName: str(raw.originalName),
    mime: str(raw.mime),
    kind: str(raw.kind),
    sizeBytes: num(raw.sizeBytes),
    pageCount: num(raw.pageCount),
    addedBy: normalizeAuthor(raw.addedBy),
    createdAt: str(raw.createdAt),
  };
}

/* -------------------------------------------------------------------------- */
/* 展示辅助                                                                    */
/* -------------------------------------------------------------------------- */

export function formatFileSize(bytes: number): string {
  if (!Number.isFinite(bytes) || bytes <= 0) return "—";
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  return `${(bytes / 1024 / 1024).toFixed(1)} MB`;
}

/** 文件类型图标用的短标签。 */
export function fileKindLabel(kind: string): string {
  const labels: Record<string, string> = {
    pdf: "PDF",
    office_doc: "DOC",
    office_slide: "PPT",
    office_sheet: "XLS",
    image: "图片",
    markdown: "MD",
    code: "代码",
    latex: "TeX",
    video: "视频",
    archive: "压缩包",
    other: "文件",
  };
  return labels[kind] ?? "文件";
}

/* -------------------------------------------------------------------------- */
/* API                                                                         */
/* -------------------------------------------------------------------------- */

/** GET /groups/{slug}/files?tag=&cursor=&limit= —— 列出文件库。 */
export async function listGroupFiles(
  slug: string,
  options: { tag?: string; cursor?: string | null; limit?: number } = {},
): Promise<GroupFilePage> {
  const params = new URLSearchParams({ limit: String(options.limit ?? 20) });
  if (options.tag) params.set("tag", options.tag);
  if (options.cursor) params.set("cursor", options.cursor);
  const data = await apiFetch<unknown>(
    `/groups/${encodeURIComponent(slug)}/files?${params.toString()}`,
  );
  if (!isRecord(data)) throw invalidResponse();
  const files = Array.isArray(data.files)
    ? data.files.map(normalizeGroupFile).filter((f): f is GroupFileDTO => f !== null)
    : [];
  const next = str(data.nextCursor);
  return { files, nextCursor: next || null };
}

/** POST /groups/{slug}/files —— 存入文件库。重复存入抛 409。 */
export async function saveGroupFile(
  slug: string,
  input: SaveGroupFileInput,
): Promise<GroupFileDTO> {
  const data = await apiFetch<unknown>(`/groups/${encodeURIComponent(slug)}/files`, {
    method: "POST",
    body: input,
  });
  if (!isRecord(data)) throw invalidResponse();
  const file = normalizeGroupFile(data.file);
  if (!file) throw invalidResponse();
  return file;
}

/** DELETE /groups/{slug}/files/{id} —— 删除文件库条目。 */
export async function deleteGroupFile(slug: string, id: string): Promise<void> {
  await apiFetch(`/groups/${encodeURIComponent(slug)}/files/${encodeURIComponent(id)}`, {
    method: "DELETE",
  });
}

/** 把用户输入的逗号/顿号分隔标签拆成数组，与后端上限对齐。 */
export function parseTags(raw: string): string[] {
  const seen = new Set<string>();
  const tags: string[] = [];
  for (const part of raw.split(/[,，、\n]/)) {
    const tag = part.trim();
    if (!tag) continue;
    const key = tag.toLowerCase();
    if (seen.has(key)) continue;
    seen.add(key);
    tags.push(tag);
    if (tags.length >= GROUP_FILE_LIMITS.tagsLimit) break;
  }
  return tags;
}
