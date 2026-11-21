/**
 * 文档引用与参考文献 API 客户端（PRD D-06）。
 *
 * 与后端 `internal/citation` 的契约对齐，请求走同源 `/api/v1/*`，
 * 由 {@link apiFetch} 自动携带 HttpOnly 会话 Cookie 与 CSRF 双令牌。
 *
 * 约定：
 * - 引用格式是「渲染时」的选择：同一份条目可以按三种格式输出，
 *   因此格式切换不需要写库，只是重新请求 `/bibliography?format=`；
 * - 导入是幂等的：同一份 .bib 反复导入不会产生重复条目，
 *   响应里的 `imported` 是本次处理条数，不是新增条数；
 * - 所有响应都做归一化，缺字段时给出安全默认值。
 */

import { ApiError, apiFetch } from "@/lib/http";

/* -------------------------------------------------------------------------- */
/* 类型                                                                        */
/* -------------------------------------------------------------------------- */

/** 参考文献格式。 */
export type CitationFormat = "gbt7714" | "apa" | "ieee";

export const CITATION_FORMAT_LABEL: Record<CitationFormat, string> = {
  gbt7714: "GB/T 7714",
  apa: "APA 7th",
  ieee: "IEEE",
};

/** 与后端 Author 对齐：个人作者（family + given）或机构作者（literal）。 */
export type CitationAuthor = {
  family?: string;
  given?: string;
  literal?: string;
};

/** 与后端 Entry 对齐。year 为 0 表示未知年份。 */
export type ReferenceDTO = {
  id: string;
  docId: string;
  citeKey: string;
  entryType: string;
  title: string;
  authors: CitationAuthor[];
  year: number;
  container: string;
  volume: string;
  issue: string;
  pages: string;
  doi: string;
  url: string;
  publisher: string;
  createdAt: string;
};

/** 渲染后的一条参考文献。 */
export type BibliographyEntryDTO = {
  citeKey: string;
  text: string;
  sortKey: string;
};

export type BibliographyDTO = {
  format: CitationFormat;
  entries: BibliographyEntryDTO[];
};

export type ImportReferencesResult = {
  imported: number;
  entries: ReferenceDTO[];
};

export type AddReferenceInput = {
  citeKey: string;
  entryType?: string;
  title: string;
  authors?: CitationAuthor[];
  year?: number;
  container?: string;
  volume?: string;
  issue?: string;
  pages?: string;
  doi?: string;
  url?: string;
  publisher?: string;
};

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

function normalizeAuthor(raw: unknown): CitationAuthor | null {
  if (typeof raw === "string") {
    const literal = raw.trim();
    return literal ? { literal } : null;
  }
  if (!isRecord(raw)) return null;
  const author: CitationAuthor = {};
  const family = str(raw.family).trim();
  const given = str(raw.given).trim();
  const literal = str(raw.literal).trim();
  if (family) author.family = family;
  if (given) author.given = given;
  if (literal) author.literal = literal;
  if (!author.family && !author.given && !author.literal) return null;
  return author;
}

function normalizeReference(raw: unknown): ReferenceDTO | null {
  if (!isRecord(raw)) return null;
  const id = str(raw.id);
  const citeKey = str(raw.citeKey);
  if (!id || !citeKey) return null;
  const authors = Array.isArray(raw.authors)
    ? raw.authors.map(normalizeAuthor).filter((a): a is CitationAuthor => a !== null)
    : [];
  return {
    id,
    docId: str(raw.docId),
    citeKey,
    entryType: str(raw.entryType) || "misc",
    title: str(raw.title),
    authors,
    year: num(raw.year),
    container: str(raw.container),
    volume: str(raw.volume),
    issue: str(raw.issue),
    pages: str(raw.pages),
    doi: str(raw.doi),
    url: str(raw.url),
    publisher: str(raw.publisher),
    createdAt: str(raw.createdAt),
  };
}

function normalizeBibliography(raw: unknown): BibliographyDTO {
  if (!isRecord(raw)) throw invalidResponse();
  const format = normalizeFormat(raw.format);
  const entries = Array.isArray(raw.entries)
    ? raw.entries.flatMap((item): BibliographyEntryDTO[] => {
        if (!isRecord(item)) return [];
        const citeKey = str(item.citeKey);
        if (!citeKey) return [];
        return [{ citeKey, text: str(item.text), sortKey: str(item.sortKey) }];
      })
    : [];
  return { format, entries };
}

function normalizeFormat(raw: unknown): CitationFormat {
  return raw === "apa" || raw === "ieee" || raw === "gbt7714" ? raw : "gbt7714";
}

/* -------------------------------------------------------------------------- */
/* 展示辅助                                                                    */
/* -------------------------------------------------------------------------- */

/** 作者在列表里的短展示：优先「姓, 名」，机构作者直接输出。 */
export function authorLabel(author: CitationAuthor): string {
  if (author.literal) return author.literal;
  if (author.family && author.given) return `${author.family}, ${author.given}`;
  return author.family ?? author.given ?? "";
}

/** 条目作者的紧凑展示，超过三位加「等」。 */
export function authorsSummary(authors: CitationAuthor[], max = 3): string {
  const names = authors.map(authorLabel).filter(Boolean);
  if (names.length === 0) return "佚名";
  if (names.length <= max) return names.join("; ");
  return `${names.slice(0, max).join("; ")} 等`;
}

export const ENTRY_TYPE_LABEL: Record<string, string> = {
  article: "期刊论文",
  inproceedings: "会议论文",
  book: "专著",
  misc: "其他",
};

/* -------------------------------------------------------------------------- */
/* API                                                                         */
/* -------------------------------------------------------------------------- */

/** GET /documents/{id}/references —— 列出全部引用条目。 */
export async function listReferences(docId: string): Promise<ReferenceDTO[]> {
  const data = await apiFetch<unknown>(`/documents/${encodeURIComponent(docId)}/references`);
  if (!isRecord(data) || !Array.isArray(data.references)) throw invalidResponse();
  return data.references
    .map(normalizeReference)
    .filter((r): r is ReferenceDTO => r !== null);
}

/** POST /documents/{id}/references/import —— 导入 BibTeX（幂等）。 */
export async function importBibTeX(docId: string, bibtex: string): Promise<ImportReferencesResult> {
  const data = await apiFetch<unknown>(`/documents/${encodeURIComponent(docId)}/references/import`, {
    method: "POST",
    body: { bibtex },
  });
  if (!isRecord(data)) throw invalidResponse();
  const entries = Array.isArray(data.entries)
    ? data.entries.map(normalizeReference).filter((r): r is ReferenceDTO => r !== null)
    : [];
  return { imported: num(data.imported), entries };
}

/** POST /documents/{id}/references —— 手工添加一条引用。 */
export async function addReference(docId: string, input: AddReferenceInput): Promise<ReferenceDTO> {
  const data = await apiFetch<unknown>(`/documents/${encodeURIComponent(docId)}/references`, {
    method: "POST",
    body: input,
  });
  if (!isRecord(data)) throw invalidResponse();
  const reference = normalizeReference(data.reference);
  if (!reference) throw invalidResponse();
  return reference;
}

/** DELETE /documents/{id}/references/{refId}。 */
export async function deleteReference(docId: string, refId: string): Promise<void> {
  await apiFetch(`/documents/${encodeURIComponent(docId)}/references/${encodeURIComponent(refId)}`, {
    method: "DELETE",
  });
}

/** GET /documents/{id}/bibliography?format= —— 渲染指定格式的参考文献表。 */
export async function getBibliography(
  docId: string,
  format: CitationFormat,
): Promise<BibliographyDTO> {
  const data = await apiFetch<unknown>(
    `/documents/${encodeURIComponent(docId)}/bibliography?format=${encodeURIComponent(format)}`,
  );
  return normalizeBibliography(data);
}

/**
 * 把参考文献表复制成纯文本，供用户粘进 Word / LaTeX。
 *
 * 编号不由前端补：IEEE 的 "[1] " 已经由服务端写进 text，
 * GB/T 与 APA 本身不编号。前端再加一遍会得到 "[1] [1] …"。
 */
export function bibliographyToText(entries: BibliographyEntryDTO[]): string {
  return entries.map((entry) => entry.text).join("\n");
}
