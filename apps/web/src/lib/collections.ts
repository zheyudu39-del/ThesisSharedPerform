/**
 * 精选策展 API 客户端（M4）。
 *
 * 产品背景：平台不做个性化推荐（PRD §1.2），精选流由运营与认证大V
 * 用人工判断策展。策展以「集合」为单位——一个有标题、有编辑者、
 * 有上下架时间的栏目，同一篇帖子可以同时出现在多个集合里。
 *
 * 请求走同源 `/api/v1/*`，由 {@link apiFetch} 自动携带会话 Cookie 与 CSRF 令牌。
 * 读接口匿名可用；写接口需要登录，且服务端会再校验策展资格。
 */

import { ApiError, apiFetch } from "@/lib/http";
import { normalizePaper, type PaperDTO } from "@/lib/papers";
import { normalizePost, type PostDTO } from "@/lib/posts";

/* -------------------------------------------------------------------------- */
/* 类型                                                                        */
/* -------------------------------------------------------------------------- */

export type CuratorRef = {
  id: string;
  name: string;
  handle: string;
  verifyType: string;
};

export type CollectionDTO = {
  id: string;
  slug: string;
  title: string;
  description: string;
  coverUrl: string | null;
  curator: CuratorRef;
  itemCount: number;
  startsAt: string;
  endsAt: string | null;
  createdAt: string;
  /** 当前查看者能否编辑。服务端已算好，前端不必再探测权限。 */
  canEdit: boolean;
};

export type CollectionItemDTO = {
  id: string;
  position: number;
  note: string;
  /** 帖子与论文恰好有一个非 null。 */
  post: PostDTO | null;
  paper: PaperDTO | null;
  addedBy: { id: string; name: string; handle: string };
  createdAt: string;
};

export type CollectionList = {
  collections: CollectionDTO[];
  nextCursor: string | null;
};

export type CollectionDetail = {
  collection: CollectionDTO;
  items: CollectionItemDTO[];
};

/* -------------------------------------------------------------------------- */
/* 归一化                                                                      */
/* -------------------------------------------------------------------------- */

function str(value: unknown, fallback = ""): string {
  return typeof value === "string" ? value : fallback;
}

function num(value: unknown, fallback = 0): number {
  return typeof value === "number" && Number.isFinite(value) ? value : fallback;
}

function normalizeCuratorRef(raw: unknown): CuratorRef {
  const r = (raw ?? {}) as Record<string, unknown>;
  return {
    id: str(r.id),
    name: str(r.name, "匿名用户"),
    handle: str(r.handle),
    verifyType: str(r.verifyType, "none"),
  };
}

export function normalizeCollection(raw: unknown): CollectionDTO {
  const r = (raw ?? {}) as Record<string, unknown>;
  return {
    id: str(r.id),
    slug: str(r.slug),
    title: str(r.title, "未命名策展集"),
    description: str(r.description),
    coverUrl: typeof r.coverUrl === "string" ? r.coverUrl : null,
    curator: normalizeCuratorRef(r.curator),
    itemCount: num(r.itemCount),
    startsAt: str(r.startsAt),
    endsAt: typeof r.endsAt === "string" ? r.endsAt : null,
    createdAt: str(r.createdAt),
    canEdit: r.canEdit === true,
  };
}

function normalizeItem(raw: unknown): CollectionItemDTO {
  const r = (raw ?? {}) as Record<string, unknown>;
  return {
    id: str(r.id),
    position: num(r.position),
    note: str(r.note),
    // 帖子/论文可能因可见性变化而被服务端过滤掉，这里保持 null 语义。
    post: r.post ? normalizePost(r.post) : null,
    paper: r.paper ? normalizePaper(r.paper) : null,
    addedBy: {
      id: str((r.addedBy as Record<string, unknown>)?.id),
      name: str((r.addedBy as Record<string, unknown>)?.name, "匿名用户"),
      handle: str((r.addedBy as Record<string, unknown>)?.handle),
    },
    createdAt: str(r.createdAt),
  };
}

/* -------------------------------------------------------------------------- */
/* 读接口                                                                      */
/* -------------------------------------------------------------------------- */

/** 列出在架策展集。匿名可读。 */
export async function listCollections(cursor?: string | null, limit = 20): Promise<CollectionList> {
  const params = new URLSearchParams({ limit: String(limit) });
  if (cursor) params.set("cursor", cursor);
  const body = (await apiFetch(`/collections?${params.toString()}`)) as Record<string, unknown>;
  const list = Array.isArray(body.collections) ? body.collections : [];
  return {
    collections: list.map(normalizeCollection),
    nextCursor: typeof body.nextCursor === "string" ? body.nextCursor : null,
  };
}

/** 读取策展集详情与条目。匿名可读，但未上架/已下架集合对非策展人返回 404。 */
export async function getCollection(slug: string): Promise<CollectionDetail> {
  const body = (await apiFetch(`/collections/${encodeURIComponent(slug)}`)) as Record<string, unknown>;
  const items = Array.isArray(body.items) ? body.items : [];
  return {
    collection: normalizeCollection(body.collection),
    items: items.map(normalizeItem),
  };
}

/* -------------------------------------------------------------------------- */
/* 写接口                                                                      */
/* -------------------------------------------------------------------------- */

export type CreateCollectionInput = {
  title: string;
  description?: string;
  startsAt?: string | null;
  endsAt?: string | null;
};

/** 新建策展集。仅运营与认证大V 可调用（服务端校验）。 */
export async function createCollection(input: CreateCollectionInput): Promise<CollectionDTO> {
  const body = (await apiFetch("/collections", {
    method: "POST",
    body: input,
  })) as Record<string, unknown>;
  return normalizeCollection(body.collection);
}

export type UpdateCollectionInput = {
  title?: string;
  description?: string;
  startsAt?: string | null;
  endsAt?: string | null;
};

/** 局部更新策展集。 */
export async function updateCollection(
  slug: string,
  input: UpdateCollectionInput,
): Promise<CollectionDTO> {
  const body = (await apiFetch(`/collections/${encodeURIComponent(slug)}`, {
    method: "PATCH",
    body: input,
  })) as Record<string, unknown>;
  return normalizeCollection(body.collection);
}

/** 删除策展集（软删除）。 */
export async function deleteCollection(slug: string): Promise<void> {
  await apiFetch(`/collections/${encodeURIComponent(slug)}`, { method: "DELETE" });
}

export type AddItemInput = {
  postId?: string;
  paperId?: string;
  note?: string;
  position?: number;
};

/** 往集合里加一条帖子或论文。两者必须且只能提供一个。 */
export async function addCollectionItem(
  slug: string,
  input: AddItemInput,
): Promise<CollectionItemDTO> {
  const body = (await apiFetch(`/collections/${encodeURIComponent(slug)}/items`, {
    method: "POST",
    body: input,
  })) as Record<string, unknown>;
  return normalizeItem(body.item);
}

/** 移除一条策展项。 */
export async function removeCollectionItem(slug: string, itemId: string): Promise<void> {
  await apiFetch(`/collections/${encodeURIComponent(slug)}/items/${itemId}`, {
    method: "DELETE",
  });
}

/** 调整策展项顺序。 */
export async function reorderCollectionItem(
  slug: string,
  itemId: string,
  position: number,
): Promise<void> {
  await apiFetch(`/collections/${encodeURIComponent(slug)}/items/${itemId}`, {
    method: "PATCH",
    body: { position },
  });
}

export { ApiError };
