/**
 * 文件上传 / 内联预览 API 客户端。
 *
 * 与后端 `internal/file` 的契约对齐（见任务约定的 API 契约），请求走同源
 * `/api/v1/*`，由 next.config.ts 的 rewrites 代理到 Go 服务。
 *
 * 上传是三步直传，文件本身**不经过** API 服务器：
 *   1. POST /files/presign      —— 校验类型 / 大小，拿到预签名 PUT URL 与 fileId；
 *   2. PUT  uploadUrl           —— 浏览器直接上传到对象存储（XMLHttpRequest，见下）；
 *   3. POST /files/{id}/complete —— 服务端核对对象真实存在与大小，登记文件并触发转码。
 *
 * ⚠️ 预签名 URL 的签名已经写进查询串，PUT 时**不能**再带 Authorization 头：
 * S3 会把它当成额外的签名头参与校验，直接返回 SignatureDoesNotMatch。因此
 * {@link uploadToS3} 只回放服务端下发的 headers，并显式过滤掉 Authorization。
 *
 * ⚠️ 上传进度只能用 XMLHttpRequest：fetch 没有上传进度事件（ReadableStream
 * 请求体在浏览器端支持也不完整），因此这里没有走 fetch。
 *
 * 错误统一抛 {@link ApiError}（code / message / status），与 lib/posts.ts 一致。
 */

import { ApiError, apiFetch } from "@/lib/http";

/* -------------------------------------------------------------------------- */
/* 常量与类型                                                                  */
/* -------------------------------------------------------------------------- */

/** 单文件上限 100MB（PRD 7.3 / 技术架构 4.1）。 */
export const MAX_FILE_BYTES = 100 * 1024 * 1024;

/** 服务端 MIME 白名单；前端只做即时反馈，最终裁决仍在服务端。 */
export const ACCEPTED_MIME_TYPES = [
  "application/pdf",
  "image/png",
  "image/jpeg",
  "image/webp",
  "application/msword",
  "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
  "application/vnd.ms-powerpoint",
  "application/vnd.openxmlformats-officedocument.presentationml.presentation",
  "application/vnd.ms-excel",
  "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
  "text/markdown",
  "text/plain",
  // LaTeX 源文件：浏览器给的 File.type 不稳定，前端按扩展名兜底成 text/x-tex。
  "text/x-tex",
  "application/x-tex",
  "application/x-latex",
] as const;

const ACCEPTED_MIME_SET = new Set<string>(ACCEPTED_MIME_TYPES);

/** 扩展名 → MIME：浏览器对 .md / .txt 常给出空 type，需要按扩展名兜底。 */
const EXTENSION_MIME: Record<string, string> = {
  pdf: "application/pdf",
  png: "image/png",
  jpg: "image/jpeg",
  jpeg: "image/jpeg",
  webp: "image/webp",
  doc: "application/msword",
  docx: "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
  ppt: "application/vnd.ms-powerpoint",
  pptx: "application/vnd.openxmlformats-officedocument.presentationml.presentation",
  xls: "application/vnd.ms-excel",
  xlsx: "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
  md: "text/markdown",
  markdown: "text/markdown",
  txt: "text/plain",
  tex: "text/x-tex",
  latex: "text/x-tex",
};

/** `<input type="file" accept>` 的值；与白名单一一对应。 */
export const ACCEPT_ATTRIBUTE =
  ".pdf,.png,.jpg,.jpeg,.webp,.doc,.docx,.ppt,.pptx,.xls,.xlsx,.md,.markdown,.txt,.tex,.latex";

export type FileStatus =
  | "pending"
  | "uploading"
  | "uploaded"
  | "scanning"
  | "transcoding"
  | "ready"
  | "rejected"
  | "failed";

export type FileKind =
  | "pdf"
  | "office_doc"
  | "office_slide"
  | "office_sheet"
  | "image"
  | "markdown"
  | "code"
  | "latex"
  | "video"
  | "archive"
  | "other";

export type FileDTO = {
  id: string;
  originalName: string;
  mime: string;
  kind: FileKind;
  sizeBytes: number;
  status: FileStatus;
  /** 页数；非分页文件或未转码完成时为 0。 */
  pageCount: number;
  createdAt: string;
  /** 首页预签名 URL；未生成时为 null。 */
  coverUrl: string | null;
  /**
   * status 为 failed / rejected 时的失败原因。
   * 例如「未找到 LibreOffice」「转换超时」「文件损坏」——让用户能分清是
   * 自己的文件有问题，还是服务端缺依赖。
   */
  errorReason: string | null;
};

export type PageDTO = {
  pageNo: number;
  /** 预签名 URL，15 分钟过期；过期后需要重新拉取。 */
  url: string;
  width: number;
  height: number;
};

export type FilePageList = {
  pages: PageDTO[];
  total: number;
  pageCount: number;
};

export type PresignResult = {
  fileId: string;
  uploadUrl: string;
  method: string;
  headers: Record<string, string>;
  expiresIn: number;
};

export type UploadProgressHandler = (percent: number) => void;

export type UploadFileOptions = {
  onProgress?: UploadProgressHandler;
  signal?: AbortSignal;
};

/* -------------------------------------------------------------------------- */
/* 请求与解析                                                                  */
/* -------------------------------------------------------------------------- */

/** required：无令牌直接失败；optional：有令牌就带上（公开预览可匿名尝试）。 */

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function str(value: unknown): string {
  return typeof value === "string" ? value : "";
}

function num(value: unknown): number {
  return typeof value === "number" && Number.isFinite(value) ? value : 0;
}

function fallbackMessage(status: number): string {
  switch (status) {
    case 401:
      return "请先登录后再继续。";
    case 403:
      return "没有权限访问该文件。";
    case 404:
      return "文件不存在，或你无权查看。";
    case 413:
      return "文件超过服务器允许的大小。";
    case 422:
      return "文件未通过校验（类型或大小不符合要求）。";
    case 429:
      return "操作过于频繁，请稍后再试。";
    default:
      return status >= 500 ? "服务器暂时不可用，请稍后重试。" : "请求失败，请稍后重试。";
  }
}

function extractError(body: unknown, status: number): { code: string; message: string } {
  if (isRecord(body) && isRecord(body.error)) {
    const code = typeof body.error.code === "string" ? body.error.code : "request_failed";
    const message =
      typeof body.error.message === "string" && body.error.message.trim().length > 0
        ? body.error.message
        : fallbackMessage(status);
    return { code, message };
  }
  return { code: "request_failed", message: fallbackMessage(status) };
}

function invalidResponse(): ApiError {
  return new ApiError("invalid_response", "服务器返回了无法识别的响应。", 0);
}

/* -------------------------------------------------------------------------- */
/* 归一化                                                                      */
/* -------------------------------------------------------------------------- */

const FILE_KINDS: FileKind[] = [
  "pdf",
  "office_doc",
  "office_slide",
  "office_sheet",
  "image",
  "markdown",
  "code",
  "latex",
  "video",
  "archive",
  "other",
];

const FILE_STATUSES: FileStatus[] = [
  "pending",
  "uploading",
  "uploaded",
  "scanning",
  "transcoding",
  "ready",
  "rejected",
  "failed",
];

function toKind(value: unknown): FileKind {
  return typeof value === "string" && (FILE_KINDS as string[]).includes(value)
    ? (value as FileKind)
    : "other";
}

function toStatus(value: unknown): FileStatus {
  return typeof value === "string" && (FILE_STATUSES as string[]).includes(value)
    ? (value as FileStatus)
    : "pending";
}

export function normalizeFile(value: unknown): FileDTO {
  if (!isRecord(value) || typeof value.id !== "string") throw invalidResponse();
  return {
    id: value.id,
    originalName: str(value.originalName) || "未命名文件",
    mime: str(value.mime),
    kind: toKind(value.kind),
    sizeBytes: num(value.sizeBytes),
    status: toStatus(value.status),
    pageCount: num(value.pageCount),
    createdAt: str(value.createdAt),
    coverUrl: typeof value.coverUrl === "string" && value.coverUrl ? value.coverUrl : null,
    errorReason:
      typeof value.errorReason === "string" && value.errorReason.trim().length > 0
        ? value.errorReason
        : null,
  };
}

function normalizePage(value: unknown): PageDTO | null {
  if (!isRecord(value)) return null;
  const pageNo = num(value.pageNo);
  const url = str(value.url);
  if (pageNo <= 0 || !url) return null;
  return {
    pageNo,
    url,
    width: num(value.width) || 1200,
    height: num(value.height) || 1697,
  };
}

function normalizePresign(body: unknown): PresignResult {
  if (!isRecord(body)) throw invalidResponse();
  const fileId = str(body.fileId);
  const uploadUrl = str(body.uploadUrl);
  if (!fileId || !uploadUrl) throw invalidResponse();

  const headers: Record<string, string> = {};
  if (isRecord(body.headers)) {
    for (const [name, value] of Object.entries(body.headers)) {
      if (typeof value === "string") headers[name] = value;
    }
  }

  return {
    fileId,
    uploadUrl,
    method: str(body.method) || "PUT",
    headers,
    expiresIn: num(body.expiresIn) || 900,
  };
}

/* -------------------------------------------------------------------------- */
/* 上传                                                                        */
/* -------------------------------------------------------------------------- */

/** 第 1 步：申请预签名上传地址。 */
export async function presignFile(
  filename: string,
  mime: string,
  sizeBytes: number,
): Promise<PresignResult> {
  const body = await apiFetch("/files/presign", {
      method: "POST",
      body: JSON.stringify({ filename, mime, sizeBytes }),
    });
  return normalizePresign(body);
}

/**
 * 第 2 步：浏览器直传对象存储。
 *
 * 用 XMLHttpRequest 而不是 fetch，因为只有 XHR 提供 `upload.onprogress`。
 * 返回的 headers 原样回放（Content-Type 必须与预签名时一致），但**绝不**
 * 附加 Authorization —— 否则 S3 签名校验失败。
 */
export function uploadToS3(
  url: string,
  file: File,
  headers: Record<string, string>,
  options: UploadFileOptions = {},
): Promise<void> {
  return new Promise<void>((resolve, reject) => {
    const { signal, onProgress } = options;

    if (signal?.aborted) {
      reject(new ApiError("upload_aborted", "已取消上传。", 0));
      return;
    }

    const xhr = new XMLHttpRequest();
    xhr.open("PUT", url, true);

    for (const [name, value] of Object.entries(headers)) {
      if (name.toLowerCase() === "authorization") continue;
      try {
        xhr.setRequestHeader(name, value);
      } catch {
        // 浏览器禁止脚本设置的头（如 Host）忽略即可，预签名 URL 不依赖它们。
      }
    }

    let settled = false;
    const onAbort = () => xhr.abort();
    const finish = (action: () => void) => {
      if (settled) return;
      settled = true;
      signal?.removeEventListener("abort", onAbort);
      action();
    };

    xhr.upload.onprogress = (event) => {
      if (!event.lengthComputable || event.total <= 0) return;
      const percent = Math.min(99, Math.round((event.loaded / event.total) * 100));
      onProgress?.(percent);
    };

    xhr.onload = () => {
      if (xhr.status >= 200 && xhr.status < 300) {
        onProgress?.(100);
        finish(resolve);
        return;
      }
      finish(() => reject(uploadHttpError(xhr.status)));
    };

    xhr.onerror = () =>
      finish(() =>
        reject(new ApiError("upload_failed", "上传失败：无法连接对象存储。", 0)),
      );

    xhr.ontimeout = () =>
      finish(() => reject(new ApiError("upload_timeout", "上传超时，请重试。", 0)));

    xhr.onabort = () =>
      finish(() => reject(new ApiError("upload_aborted", "已取消上传。", 0)));

    signal?.addEventListener("abort", onAbort, { once: true });

    try {
      xhr.send(file);
    } catch {
      finish(() => reject(new ApiError("upload_failed", "上传失败，请重试。", 0)));
    }
  });
}

function uploadHttpError(status: number): ApiError {
  switch (status) {
    case 400:
      return new ApiError("upload_failed", "对象存储拒绝了本次上传（请求不合法）。", 400);
    case 403:
      return new ApiError(
        "upload_expired",
        "上传链接已过期或签名不匹配，请重新上传。",
        403,
      );
    case 413:
      return new ApiError("upload_too_large", "文件超过对象存储的大小限制。", 413);
    default:
      return new ApiError("upload_failed", `上传失败（HTTP ${status}）。`, status);
  }
}

/** 第 3 步：通知服务端上传完成，服务端会核对对象真实存在与大小并触发转码。 */
export async function completeUpload(fileId: string, sizeBytes: number): Promise<FileDTO> {
  const body = await apiFetch(`/files/${encodeURIComponent(fileId)}/complete`, { method: "POST", body: JSON.stringify({ sizeBytes }) });
  if (!isRecord(body) || !("file" in body)) throw invalidResponse();
  return normalizeFile(body.file);
}

/** 三步上传的编排：校验 → presign → 直传 → complete。 */
export async function uploadFile(
  file: File,
  options: UploadFileOptions = {},
): Promise<FileDTO> {
  const validation = validateUpload(file);
  if (!validation.ok) {
    throw new ApiError("validation_error", validation.message, 422);
  }

  const presign = await presignFile(file.name, validation.mime, file.size);
  await uploadToS3(presign.uploadUrl, file, presign.headers, options);
  return completeUpload(presign.fileId, file.size);
}

/* -------------------------------------------------------------------------- */
/* 查询                                                                        */
/* -------------------------------------------------------------------------- */

/** 查询文件元信息；不存在或无权查看时服务端统一返回 404。 */
export async function getFile(id: string): Promise<FileDTO> {
  const body = await apiFetch(`/files/${encodeURIComponent(id)}`, { method: "GET" });
  if (!isRecord(body) || !("file" in body)) throw invalidResponse();
  return normalizeFile(body.file);
}

/**
 * 分页拉取页图（预签名 URL，15 分钟有效）。
 *
 * `page` 是 1 起始的页码，`size` 是本批页数；返回里同时带总页数，便于虚拟滚动
 * 在只知道文件 id 时也能算出滚动高度。
 */
export async function getFilePages(
  id: string,
  page = 1,
  size = 10,
): Promise<FilePageList> {
  const params = new URLSearchParams({
    page: String(Math.max(1, Math.trunc(page))),
    size: String(Math.min(Math.max(Math.trunc(size), 1), 50)),
  });

  const body = await apiFetch(`/files/${encodeURIComponent(id)}/pages?${params.toString()}`, { method: "GET" });
  if (!isRecord(body) || !Array.isArray(body.pages)) throw invalidResponse();

  const pages = body.pages
    .map(normalizePage)
    .filter((p): p is PageDTO => p !== null)
    .sort((a, b) => a.pageNo - b.pageNo);

  const lastPage = pages.reduce((max, p) => Math.max(max, p.pageNo), 0);
  const pageCount = Math.max(num(body.pageCount), num(body.total), lastPage);

  return {
    pages,
    total: num(body.total) || pageCount,
    pageCount,
  };
}

/* -------------------------------------------------------------------------- */
/* 客户端校验与展示辅助                                                        */
/* -------------------------------------------------------------------------- */

export type UploadValidation =
  | { ok: true; mime: string }
  | { ok: false; message: string };

/** 浏览器对部分扩展名给空 type，这里按扩展名补齐。 */
export function resolveMime(file: File): string {
  const type = (file.type || "").toLowerCase();
  if (type === "text/x-markdown") return "text/markdown";

  // LaTeX 优先按扩展名判定：浏览器对 .tex 常报 text/plain，
  // 若先信任 type 会被归成 markdown，之后在文件库里就找不回「这是 LaTeX 源文件」。
  const extension = file.name.includes(".")
    ? file.name.slice(file.name.lastIndexOf(".") + 1).toLowerCase()
    : "";
  if (extension === "tex" || extension === "latex") {
    return "text/x-tex";
  }

  if (type && ACCEPTED_MIME_SET.has(type)) return type;

  const fromExtension = EXTENSION_MIME[extension];
  if (fromExtension) return fromExtension;

  return type || "application/octet-stream";
}

/** 客户端预检：类型白名单 + 非空 + 100MB 上限。 */
export function validateUpload(file: File): UploadValidation {
  if (file.size <= 0) {
    return { ok: false, message: "文件为空，请重新选择。" };
  }
  if (file.size > MAX_FILE_BYTES) {
    return {
      ok: false,
      message: `文件大小 ${formatBytes(file.size)}，超过上限 ${formatBytes(MAX_FILE_BYTES)}。`,
    };
  }

  const mime = resolveMime(file);
  if (!ACCEPTED_MIME_SET.has(mime)) {
    return {
      ok: false,
      message:
        "不支持这种文件类型。支持 PDF、图片、Word / PPT / Excel、Markdown、纯文本与 LaTeX（.tex）。",
    };
  }

  return { ok: true, mime };
}

/** 原文件直链（图片 / Markdown / 代码 / LaTeX 预览用）。 */
export type OriginalFile = {
  id: string;
  originalName: string;
  mime: string;
  kind: FileKind;
  sizeBytes: number;
  /** 短时预签名地址，过期后需要重新拉取。 */
  url: string;
  expiresIn: number;
};

/**
 * 取原文件的预签名直链。
 *
 * 页图（/files/{id}/pages）只对 PDF 与 Office 有意义；图片与文本类文件
 * 需要原文件本身才能预览（PRD V-04）。
 */
export async function getOriginalFile(fileId: string): Promise<OriginalFile> {
  const body = await apiFetch(`/files/${encodeURIComponent(fileId)}/original`);
  if (!isRecord(body) || typeof body.id !== "string" || typeof body.url !== "string") {
    throw invalidResponse();
  }
  return {
    id: body.id,
    originalName: str(body.originalName),
    mime: str(body.mime),
    kind: toKind(body.kind),
    sizeBytes: num(body.sizeBytes),
    url: body.url,
    expiresIn: num(body.expiresIn),
  };
}

export function formatBytes(bytes: number): string {
  if (!Number.isFinite(bytes) || bytes <= 0) return "0 B";
  const units = ["B", "KB", "MB", "GB"];
  let value = bytes;
  let unit = 0;
  while (value >= 1024 && unit < units.length - 1) {
    value /= 1024;
    unit += 1;
  }
  const rounded = unit === 0 || value >= 10 ? Math.round(value) : Number(value.toFixed(1));
  return `${rounded} ${units[unit]}`;
}

export const FILE_STATUS_LABEL: Record<FileStatus, string> = {
  pending: "等待上传",
  uploading: "上传中",
  uploaded: "已上传",
  scanning: "安全扫描中",
  transcoding: "转码中",
  ready: "可预览",
  rejected: "已拒绝",
  failed: "处理失败",
};

export function fileStatusLabel(status: FileStatus): string {
  return FILE_STATUS_LABEL[status] ?? "未知状态";
}

export const FILE_KIND_LABEL: Record<FileKind, string> = {
  pdf: "PDF",
  office_doc: "Word",
  office_slide: "PowerPoint",
  office_sheet: "Excel",
  image: "图片",
  markdown: "Markdown",
  code: "代码",
  latex: "LaTeX",
  video: "视频",
  archive: "压缩包",
  other: "文件",
};

export function fileKindLabel(kind: FileKind): string {
  return FILE_KIND_LABEL[kind] ?? "文件";
}

/** 转码流水线中的中间态：需要轮询等待。 */
export function isProcessingStatus(status: FileStatus): boolean {
  return (
    status === "pending" ||
    status === "uploading" ||
    status === "uploaded" ||
    status === "scanning" ||
    status === "transcoding"
  );
}

/** 终态失败：不再轮询，直接降级。 */
export function isFailedStatus(status: FileStatus): boolean {
  return status === "rejected" || status === "failed";
}

/**
 * 报告该文件是否由 PDF 渲染而来（原生 PDF 或 Office 转 PDF 的产物）。
 *
 * 注意：**不要**用它决定要不要走 {@link PdfPreview}。Office 文档转码后
 * 同样有页图，能否预览取决于 status 是否 ready（见 PdfPreview 内部判定），
 * 取决于 kind 会把这些文件误判成「无预览」。此函数仅用于展示层面的分类。
 */
export function isPdfFile(file: FileDTO): boolean {
  return file.kind === "pdf" || file.mime === "application/pdf";
}
