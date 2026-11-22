/**
 * 论文 API 客户端。
 * 与后端 `internal/paper` 的 DTO 对齐；请求走同源 /api/v1/*。
 */

import { ApiError, apiFetch } from "@/lib/http";

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
  const body = await apiFetch("/papers/resolve", {
    method: "POST",
    body: JSON.stringify({ input: input.trim() }),
  });

  if (
    typeof body !== "object" ||
    body === null ||
    typeof (body as Record<string, unknown>).paper !== "object"
  ) {
    throw new ApiError("invalid_response", "服务器返回了无法识别的响应。", 0);
  }
  return body as ResolveResult;
}

/** 元数据局部更新入参（PRD P-02）。省略的字段保持不变。 */
export type UpdatePaperInput = {
  title?: string;
  authors?: PaperAuthor[];
  abstract?: string;
  venue?: string;
  publishedYear?: number;
  citationCount?: number;
};

/**
 * 局部更新论文元数据。
 *
 * 只有创建者与管理员能改；改了标题或作者后，服务端会重算归一化标题
 * 与软去重哈希，保证手动补录的去重仍然有效。
 */
export async function updatePaper(id: string, input: UpdatePaperInput): Promise<PaperDTO> {
  const body = await apiFetch(`/papers/${encodeURIComponent(id)}`, {
    method: "PATCH",
    body: JSON.stringify(input),
  });
  if (typeof body !== "object" || body === null) {
    throw new ApiError("invalid_response", "服务器返回了无法识别的响应。", 0);
  }
  return normalizePaper((body as { paper: unknown }).paper);
}

export type MergeResult = {
  paper: PaperDTO;
  moved: {
    posts: number;
    tags: number;
    libraryItems: number;
    collectionItems: number;
    docReferences: number;
  };
};

/**
 * 把重复论文合并进保留的论文（PRD P-02）。
 *
 * 帖子、标签、群资料库、策展项、文档引用都会迁到保留方，
 * 重复记录随后被删除。两篇论文都必须是当前用户创建的（或管理员）。
 */
export async function mergePapers(keepId: string, duplicateId: string): Promise<MergeResult> {
  const body = await apiFetch(`/papers/${encodeURIComponent(keepId)}/merge`, {
    method: "POST",
    body: JSON.stringify({ duplicateId }),
  });
  if (typeof body !== "object" || body === null) {
    throw new ApiError("invalid_response", "服务器返回了无法识别的响应。", 0);
  }
  const record = body as Record<string, unknown>;
  return {
    paper: normalizePaper(record.paper),
    moved: (record.moved ?? {}) as MergeResult["moved"],
  };
}

/** 手动补录入参（PRD P-04）。标题与至少一位作者必填。 */
export type CreateManualPaperInput = {
  title: string;
  authors: PaperAuthor[];
  abstract?: string;
  venue?: string;
  publishedYear?: number;
  doi?: string;
  arxivId?: string;
};

/**
 * 手动补录论文（无 DOI / arXiv 编号的场景）。
 *
 * 不访问任何外部源，元数据完全来自用户。服务端按 (标题, 第一作者, 年份)
 * 做软去重，重复补录同一篇会返回已有记录（existing=true）。
 */
export async function createManualPaper(
  input: CreateManualPaperInput,
): Promise<ResolveResult> {
  const body = await apiFetch("/papers", {
    method: "POST",
    body: JSON.stringify(input),
  });
  if (
    typeof body !== "object" ||
    body === null ||
    typeof (body as Record<string, unknown>).paper !== "object"
  ) {
    throw new ApiError("invalid_response", "服务器返回了无法识别的响应。", 0);
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
  const body = await apiFetch(`/papers?limit=${limit}&offset=${offset}`);
  return body as PaperList;
}

/**
 * 把已上传的文件挂到论文上（PATCH /papers/{id}/pdf）。
 *
 * 上传本身只写 files 表；不调用这个接口，刷新后论文页又会显示「还没有全文」。
 * 传 null 表示卸载全文。
 */
export async function setPaperPDFFile(paperId: string, fileId: string | null): Promise<PaperDTO> {
  const body = await apiFetch(`/papers/${encodeURIComponent(paperId)}/pdf`, {
    method: "PATCH",
    body: JSON.stringify({ fileId }),
  });
  if (typeof body !== "object" || body === null) {
    throw new ApiError("invalid_response", "服务器返回了无法识别的响应。", 0);
  }
  return normalizePaper((body as { paper: unknown }).paper);
}

/** 按 ID 查询单篇论文。 */
export async function getPaper(id: string): Promise<PaperDTO> {
  const body = await apiFetch(`/papers/${encodeURIComponent(id)}`);
  if (typeof body !== "object" || body === null) {
    throw new ApiError("invalid_response", "服务器返回了无法识别的响应。", 0);
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
    throw new ApiError("invalid_response", "服务器返回了无法识别的响应。", 0);
  }
  const p = value as Record<string, unknown>;
  if (typeof p.id !== "string") {
    throw new ApiError("invalid_response", "服务器返回的论文数据不完整。", 0);
  }

  const rawFileId = p.pdfFileId ?? p.pdf_file_id;
  const pdfFileId =
    typeof rawFileId === "string" && rawFileId.trim().length > 0 ? rawFileId : null;

  return {
    ...(value as PaperDTO),
    pdfFileId,
  };
}
