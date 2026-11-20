/**
 * 论文 API 客户端。
 * 与后端 `internal/paper` 的 DTO 对齐；请求走同源 /api/v1/*。
 */

import { AuthError, getToken } from "@/lib/auth";

export type PaperAuthor = {
  name: string;
  affiliation?: string;
  orcid?: string;
};

export type PaperDTO = {
  id: string;
  doi: string;
  arxivId: string;
  openreviewId: string;
  title: string;
  authors: PaperAuthor[];
  abstract: string;
  venue: string;
  publishedYear: number;
  citationCount: number;
  metaSource: string;
  createdAt: string;
  /**
   * 关联的 PDF 文件 id（papers.pdf_file_id）。为空表示这篇论文还没有上传全文，
   * 卡片只做文本预览；有值时由 PdfPreview 渲染服务端生成的页图。
   */
  pdfFileId: string | null;
};

export type ResolveResult = {
  paper: PaperDTO;
  /** true 表示库中已存在，本次未新建。 */
  existing: boolean;
};

async function authorizedFetch(path: string, init: RequestInit = {}): Promise<unknown> {
  const token = getToken();
  if (!token) {
    throw new AuthError("unauthorized", "请先登录后再解析论文。", 401);
  }

  const headers = new Headers(init.headers);
  headers.set("Accept", "application/json");
  headers.set("Authorization", `Bearer ${token}`);
  if (init.body !== undefined) headers.set("Content-Type", "application/json");

  let response: Response;
  try {
    response = await fetch(`/api/v1${path}`, { ...init, headers });
  } catch {
    throw new AuthError("network_error", "无法连接服务器，请检查网络后重试。", 0);
  }

  const text = await response.text();
  let body: unknown = null;
  if (text) {
    try {
      body = JSON.parse(text) as unknown;
    } catch {
      body = null;
    }
  }

  if (!response.ok) {
    const error = extractError(body);
    throw new AuthError(error.code, error.message, response.status);
  }
  return body;
}

function extractError(body: unknown): { code: string; message: string } {
  if (typeof body === "object" && body !== null && !Array.isArray(body)) {
    const record = body as Record<string, unknown>;
    const err = record.error;
    if (typeof err === "object" && err !== null) {
      const e = err as Record<string, unknown>;
      return {
        code: typeof e.code === "string" ? e.code : "request_failed",
        message: typeof e.message === "string" ? e.message : "请求失败，请稍后重试。",
      };
    }
  }
  return { code: "request_failed", message: "请求失败，请稍后重试。" };
}

/**
 * 解析论文：接收 DOI / arXiv 编号 / 论文链接。
 * 后端会识别标识、抓取元数据并入库；同一篇论文重复解析返回 existing=true。
 */
export async function resolvePaper(input: string): Promise<ResolveResult> {
  const body = await authorizedFetch("/papers/resolve", {
    method: "POST",
    body: JSON.stringify({ input: input.trim() }),
  });

  if (
    typeof body !== "object" ||
    body === null ||
    typeof (body as Record<string, unknown>).paper !== "object"
  ) {
    throw new AuthError("invalid_response", "服务器返回了无法识别的响应。", 0);
  }
  return body as ResolveResult;
}

export type PaperList = {
  papers: PaperDTO[];
  total: number;
  limit: number;
  offset: number;
};

/** 分页列出论文。 */
export async function listPapers(limit = 20, offset = 0): Promise<PaperList> {
  const body = await authorizedFetch(`/papers?limit=${limit}&offset=${offset}`);
  return body as PaperList;
}

/** 按 ID 查询单篇论文。 */
export async function getPaper(id: string): Promise<PaperDTO> {
  const body = await authorizedFetch(`/papers/${encodeURIComponent(id)}`);
  if (typeof body !== "object" || body === null) {
    throw new AuthError("invalid_response", "服务器返回了无法识别的响应。", 0);
  }
  return normalizePaper((body as { paper: unknown }).paper);
}

/**
 * 归一化 PaperDTO。
 *
 * 后端目前还没把 `pdf_file_id` 序列化出来，因此同时接受 `pdfFileId` 与
 * `pdf_file_id` 两种写法；两者都没有时为 null（走文本预览）。
 */
export function normalizePaper(value: unknown): PaperDTO {
  if (typeof value !== "object" || value === null || Array.isArray(value)) {
    throw new AuthError("invalid_response", "服务器返回了无法识别的响应。", 0);
  }
  const p = value as Record<string, unknown>;
  if (typeof p.id !== "string") {
    throw new AuthError("invalid_response", "服务器返回的论文数据不完整。", 0);
  }

  const rawFileId = p.pdfFileId ?? p.pdf_file_id;
  const pdfFileId =
    typeof rawFileId === "string" && rawFileId.trim().length > 0 ? rawFileId : null;

  return {
    ...(value as PaperDTO),
    pdfFileId,
  };
}
