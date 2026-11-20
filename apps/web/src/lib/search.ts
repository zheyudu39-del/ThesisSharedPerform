/**
 * 统一检索 API 客户端。
 *
 * 后端 GET /api/v1/search 是公开读接口（OptionalAuthenticate），
 * 匿名也能搜；结果只来自 search_documents 里已索引的实体。
 *
 * 返回的 snippet 已由服务端做 HTML 转义并插入 <mark> 高亮，
 * 因此前端只能用 dangerouslySetInnerHTML 渲染 snippet —— 这是安全的，
 * 因为唯一可能出现的标签就是 <mark>，其余字符均已转义（见后端 search/snippet.go）。
 */

import { ApiError, apiFetch } from "@/lib/http";

export type SearchEntityType = "paper" | "post" | "user" | "group";

export type SearchItem = {
  entityType: SearchEntityType;
  entityId: string;
  title: string;
  /** 已转义并带 <mark> 高亮的片段。 */
  snippet: string;
  rank: number;
};

export type SearchResult = {
  items: SearchItem[];
  total: number;
};

export const SEARCH_TYPES: { value: SearchEntityType; label: string }[] = [
  { value: "paper", label: "论文" },
  { value: "post", label: "帖子" },
  { value: "group", label: "群组" },
  { value: "user", label: "用户" },
];

const ENTITY_TYPES: SearchEntityType[] = ["paper", "post", "user", "group"];

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function str(value: unknown): string {
  return typeof value === "string" ? value : "";
}

function normalizeEntityType(value: unknown): SearchEntityType | null {
  return ENTITY_TYPES.includes(value as SearchEntityType) ? (value as SearchEntityType) : null;
}

function normalizeItem(value: unknown): SearchItem | null {
  if (!isRecord(value) || typeof value.entityId !== "string") return null;
  const entityType = normalizeEntityType(value.entityType);
  if (!entityType) return null;
  return {
    entityType,
    entityId: value.entityId,
    title: str(value.title),
    snippet: str(value.snippet),
    rank: typeof value.rank === "number" && Number.isFinite(value.rank) ? value.rank : 0,
  };
}

/**
 * 执行检索。
 *
 * @param query 关键词，为空时直接返回空结果（后端会 422，不必发请求）
 * @param types 限定的实体类型；空数组表示不限
 * @param limit 1–50，默认 20
 */
export async function search(
  query: string,
  types: SearchEntityType[] = [],
  limit = 20,
): Promise<SearchResult> {
  const text = query.trim();
  if (!text) return { items: [], total: 0 };

  const params = new URLSearchParams({
    q: text,
    limit: String(Math.min(Math.max(limit, 1), 50)),
  });
  if (types.length > 0) params.set("type", types.join(","));

  const body = await apiFetch<unknown>(`/search?${params.toString()}`);
  if (!isRecord(body) || !Array.isArray(body.items)) {
    throw new ApiError("invalid_response", "服务器返回了无法识别的响应。", 0);
  }
  const items = body.items
    .map(normalizeItem)
    .filter((item): item is SearchItem => item !== null);
  return {
    items,
    total: typeof body.total === "number" && Number.isFinite(body.total) ? body.total : items.length,
  };
}

/**
 * 结果的站内地址。
 *
 * 论文、帖子有直接详情路由；用户需要 handle、群组需要 slug，
 * 而检索结果只带实体 ID，因此这两类暂不生成链接（展示为纯文本卡片）。
 */
export function searchItemHref(item: SearchItem): string | null {
  switch (item.entityType) {
    case "paper":
      return `/papers/${encodeURIComponent(item.entityId)}`;
    case "post":
      return `/posts/${encodeURIComponent(item.entityId)}`;
    default:
      return null;
  }
}

/** 实体类型 → 中文标签。 */
export const SEARCH_TYPE_LABEL: Record<SearchEntityType, string> = {
  paper: "论文",
  post: "帖子",
  group: "群组",
  user: "用户",
};
