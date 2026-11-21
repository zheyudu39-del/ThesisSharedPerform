/**
 * 服务端专用 API 客户端（RSC / generateMetadata / sitemap）。
 *
 * 为什么不能复用 lib/papers.ts、lib/posts.ts、lib/users.ts：
 * 它们通过 `getToken()` 读取 localStorage，而 localStorage 在服务端不存在；
 * 并且它们的请求地址是相对路径 `/api/v1/*`，服务端 fetch 必须用绝对地址。
 *
 * 约定：
 * - 只读请求；公开读接口后端允许匿名访问，若浏览器带了会话 Cookie，
 *   则转发给 Go 服务，让 SSR 能渲染出 liked / bookmarked / following 等个性化字段；
 * - 任何失败（404、网络错误、超时、5xx）都返回 null / []，由调用方决定
 *   notFound() 或降级渲染，绝不让一个坏掉的接口拖垮整页；
 * - 用 React `cache` 在单次请求内去重：generateMetadata 与页面各调用一次，
 *   实际只发一次网络请求。
 */

import { cache } from "react";
import { cookies } from "next/headers";
import type { PaperAuthor, PaperDTO } from "@/lib/papers";
import type {
  CommentDTO,
  CommentPage,
  PostAuthor,
  PostDTO,
  PostVisibility,
  RepostRef,
} from "@/lib/posts";
import type { GroupDTO, GroupRole, JoinPolicy } from "@/lib/groups";
import type { PublicProfile, PublicUserDTO, PublicUserStats } from "@/lib/users";

/** 直连 Go API 的绝对地址；与 lib/api.ts 保持一致。 */
const API_BASE = (
  process.env.API_INTERNAL_URL ??
  process.env.NEXT_PUBLIC_API_URL ??
  "http://localhost:8080"
).replace(/\/+$/, "");

/** 公开内容变化不频繁，60 秒的 Data Cache 足以吸收爬虫与分享链接的重复请求。 */
const REVALIDATE_SECONDS = 60;

/** 单次请求超时，避免上游卡住时把渲染也一起挂住。 */
const TIMEOUT_MS = 8000;

/* -------------------------------------------------------------------------- */
/* 请求                                                                        */
/* -------------------------------------------------------------------------- */

async function fetchJSON(path: string, revalidate: number | false = REVALIDATE_SECONDS): Promise<unknown | null> {
  // 把浏览器的 Cookie 转发给 Go 服务：会话是 HttpOnly 的，服务端渲染时
  // 只有通过 Cookie 才能拿到登录态，进而渲染正确的 liked / following 状态。
  // cookies() 只在请求上下文中可用（RSC / Route Handler），构建期会抛错，故用 try 包裹。
  let cookieHeader = "";
  try {
    const store = await cookies();
    cookieHeader = store.toString();
  } catch {
    // 无请求上下文（例如构建期静态分析）：按匿名处理。
  }

  try {
    const headers: Record<string, string> = { Accept: "application/json" };
    if (cookieHeader) headers.Cookie = cookieHeader;

    const response = await fetch(`${API_BASE}/api/v1${path}`, {
      headers,
      // 需要身份区分的响应（如群内动态）不能进共享 Data Cache：
      // Next 的缓存键只有 URL，不同用户的 Cookie 会命中同一份缓存。
      next: revalidate === false ? { revalidate: 0 } : { revalidate },
      cache: revalidate === false ? "no-store" : undefined,
      signal: AbortSignal.timeout(TIMEOUT_MS),
    });
    if (!response.ok) return null;

    const text = await response.text();
    if (!text) return null;
    try {
      return JSON.parse(text) as unknown;
    } catch {
      return null;
    }
  } catch {
    // 连接失败 / 超时 / 非法响应：一律视为「这次拿不到」。
    return null;
  }
}

/* -------------------------------------------------------------------------- */
/* 归一化（与 lib/*.ts 的解析规则保持一致，但不依赖任何浏览器 API）            */
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

function strList(value: unknown): string[] {
  return Array.isArray(value)
    ? value.filter((item): item is string => typeof item === "string" && item.trim().length > 0)
    : [];
}

function normalizePaper(value: unknown): PaperDTO | null {
  if (!isRecord(value) || typeof value.id !== "string") return null;

  const authors: PaperAuthor[] = Array.isArray(value.authors)
    ? value.authors.filter(isRecord).map((a) => ({
        name: str(a.name),
        affiliation: typeof a.affiliation === "string" ? a.affiliation : undefined,
        orcid: typeof a.orcid === "string" ? a.orcid : undefined,
      }))
    : [];

  const rawFileId = value.pdfFileId ?? value.pdf_file_id;

  return {
    id: value.id,
    doi: str(value.doi),
    arxivId: str(value.arxivId),
    openreviewId: str(value.openreviewId),
    title: str(value.title),
    authors,
    abstract: str(value.abstract),
    venue: str(value.venue),
    publishedYear: num(value.publishedYear),
    citationCount: num(value.citationCount),
    metaSource: str(value.metaSource),
    createdAt: str(value.createdAt),
    pdfFileId:
      typeof rawFileId === "string" && rawFileId.trim().length > 0 ? rawFileId : null,
  };
}

function normalizePostAuthor(value: unknown): PostAuthor {
  if (!isRecord(value)) return { id: "", name: "未知用户", handle: "", verifyType: "" };
  return {
    id: str(value.id),
    name: str(value.name) || "未知用户",
    handle: str(value.handle),
    verifyType: str(value.verifyType),
  };
}

function normalizeRepost(value: unknown): RepostRef | null {
  if (!isRecord(value) || typeof value.id !== "string") return null;
  return {
    id: value.id,
    authorName: str(value.authorName) || "未知用户",
    contentText: str(value.contentText),
    paper: normalizePaper(value.paper),
  };
}

function normalizePost(value: unknown): PostDTO | null {
  if (!isRecord(value) || typeof value.id !== "string") return null;

  const visibility: PostVisibility =
    value.visibility === "followers" ||
    value.visibility === "group" ||
    value.visibility === "private"
      ? value.visibility
      : "public";

  return {
    id: value.id,
    type: value.type === "repost" ? "repost" : "original",
    author: normalizePostAuthor(value.author),
    contentHtml: str(value.contentHtml),
    contentText: str(value.contentText),
    paper: normalizePaper(value.paper),
    repostOf: normalizeRepost(value.repostOf),
    groupId: typeof value.groupId === "string" ? value.groupId : null,
    visibility,
    likeCount: num(value.likeCount),
    commentCount: num(value.commentCount),
    repostCount: num(value.repostCount),
    hotScore: num(value.hotScore),
    createdAt: str(value.createdAt),
    liked: value.liked === true,
    bookmarked: value.bookmarked === true,
    tags: strList(value.tags),
    attachments: Array.isArray(value.attachments)
      ? value.attachments
          .filter(isRecord)
          .map((a) => ({
            fileId: str(a.fileId),
            name: str(a.name) || "附件",
            mime: str(a.mime),
            kind: str(a.kind),
            sizeBytes: num(a.sizeBytes),
            pageCount: num(a.pageCount),
          }))
          .filter((a) => a.fileId !== "")
      : [],
  };
}

function normalizeComment(value: unknown): CommentDTO | null {
  if (!isRecord(value) || typeof value.id !== "string") return null;
  const author = isRecord(value.author) ? value.author : {};
  return {
    id: value.id,
    postId: str(value.postId),
    parentId: typeof value.parentId === "string" ? value.parentId : null,
    author: {
      id: str(author.id),
      name: str(author.name) || "未知用户",
      handle: str(author.handle),
    },
    contentHtml: str(value.contentHtml),
    contentText: str(value.contentText),
    likeCount: num(value.likeCount),
    createdAt: str(value.createdAt),
  };
}

function normalizePublicProfile(value: unknown): PublicProfile | null {
  if (!isRecord(value) || !isRecord(value.user)) return null;
  const u = value.user;
  if (typeof u.id !== "string" || typeof u.handle !== "string") return null;

  const user: PublicUserDTO = {
    id: u.id,
    name: str(u.name) || u.handle,
    handle: u.handle,
    bio: str(u.bio),
    institution: str(u.institution),
    researchFields: strList(u.researchFields),
    verifyType: str(u.verifyType),
    createdAt: str(u.createdAt),
  };

  const statsRecord = isRecord(value.stats) ? value.stats : {};
  const stats: PublicUserStats = {
    postCount: num(statsRecord.postCount),
    followerCount: num(statsRecord.followerCount),
    followingCount: num(statsRecord.followingCount),
  };

  // 匿名请求时后端不返回 following，此时不能断言「未关注」。
  const followingKnown =
    typeof value.following === "boolean" || typeof statsRecord.following === "boolean";

  return {
    user,
    stats,
    following: value.following === true || statsRecord.following === true,
    followingKnown,
  };
}

function normalizeJoinPolicy(value: unknown): JoinPolicy {
  return value === "public" || value === "request" || value === "invite" ? value : "invite";
}

function normalizeRole(value: unknown): GroupRole | null {
  return value === "owner" || value === "admin" || value === "member" ? value : null;
}

function normalizeGroup(value: unknown): GroupDTO | null {
  if (!isRecord(value)) return null;
  const id = str(value.id);
  if (!id) return null;
  return {
    id,
    name: str(value.name) || "未命名群组",
    slug: str(value.slug) || id,
    description: str(value.description),
    coverUrl: typeof value.coverUrl === "string" && value.coverUrl ? value.coverUrl : null,
    ownerId: str(value.ownerId),
    ownerName: str(value.ownerName),
    joinPolicy: normalizeJoinPolicy(value.joinPolicy),
    memberCount: num(value.memberCount),
    myRole: normalizeRole(value.myRole),
    createdAt: str(value.createdAt),
  };
}

export type GroupDetailServer = {
  group: GroupDTO;
  myRole: GroupRole | null;
  memberCount: number;
};

/* -------------------------------------------------------------------------- */
/* 对外 API（React cache 去重：同一次渲染里 generateMetadata 与页面共享结果）   */
/* -------------------------------------------------------------------------- */

/** 按 ID 读取论文；不存在或接口不可用时返回 null。 */
export const fetchPaper = cache(async (id: string): Promise<PaperDTO | null> => {
  const body = await fetchJSON(`/papers/${encodeURIComponent(id)}`);
  if (!isRecord(body)) return null;
  return normalizePaper(body.paper);
});

/** 读取公开个人主页；不存在或接口不可用时返回 null。 */
export const fetchPublicUser = cache(async (handle: string): Promise<PublicProfile | null> => {
  const body = await fetchJSON(`/users/${encodeURIComponent(handle)}`);
  return normalizePublicProfile(body);
});

/** 读取帖子详情；不存在（含无权查看）或接口不可用时返回 null。 */
export const fetchPost = cache(async (id: string): Promise<PostDTO | null> => {
  const body = await fetchJSON(`/posts/${encodeURIComponent(id)}`);
  if (!isRecord(body)) return null;
  return normalizePost(body.post);
});

/** 读取群组详情（公开读）；不存在或接口不可用时返回 null。 */
export const fetchGroup = cache(async (slug: string): Promise<GroupDetailServer | null> => {
  // myRole 随会话变化：不能进共享 Data Cache，否则会把 A 的成员身份渲染给 B。
  const body = await fetchJSON(`/groups/${encodeURIComponent(slug)}`, false);
  if (!isRecord(body)) return null;

  const group = normalizeGroup(body.group);
  if (!group) return null;

  const myRole = normalizeRole(body.myRole) ?? group.myRole;
  const memberCount =
    typeof body.memberCount === "number" && Number.isFinite(body.memberCount)
      ? num(body.memberCount)
      : group.memberCount;

  return { group: { ...group, myRole, memberCount }, myRole, memberCount };
});

/**
 * 读取一页群内动态（默认 20 条）；失败时返回 null。
 * 群内动态仅成员可读，匿名访问会得到 401，页面按「登录后查看」降级。
 */
export const fetchGroupFeed = cache(
  async (slug: string, limit = 20): Promise<{ posts: PostDTO[]; nextCursor: string | null } | null> => {
    const params = new URLSearchParams({ limit: String(Math.min(Math.max(limit, 1), 50)) });
    const body = await fetchJSON(
      `/groups/${encodeURIComponent(slug)}/feed?${params.toString()}`,
      false,
    );
    if (!isRecord(body) || !Array.isArray(body.posts)) return null;
    return {
      posts: body.posts.map(normalizePost).filter((p): p is PostDTO => p !== null),
      nextCursor:
        typeof body.nextCursor === "string" && body.nextCursor ? body.nextCursor : null,
    };
  },
);

/** 读取一页评论（默认 20 条）；失败时返回 null，页面按「暂无评论」降级。 */
export const fetchComments = cache(
  async (postId: string, limit = 20): Promise<CommentPage | null> => {
    const params = new URLSearchParams({ limit: String(Math.min(Math.max(limit, 1), 50)) });
    const body = await fetchJSON(
      `/posts/${encodeURIComponent(postId)}/comments?${params.toString()}`,
    );
    if (!isRecord(body) || !Array.isArray(body.comments)) return null;
    return {
      comments: body.comments
        .map(normalizeComment)
        .filter((c): c is CommentDTO => c !== null),
      nextCursor:
        typeof body.nextCursor === "string" && body.nextCursor ? body.nextCursor : null,
    };
  },
);

/**
 * 列出论文（sitemap 用）。失败时返回空数组，让 sitemap 至少保留静态页。
 * 不包 React cache：sitemap 在渲染管线之外调用。
 */
export async function fetchPaperList(limit = 100): Promise<PaperDTO[]> {
  const safeLimit = Math.min(Math.max(limit, 1), 100);
  const body = await fetchJSON(`/papers?limit=${safeLimit}&offset=0`);
  if (!isRecord(body) || !Array.isArray(body.papers)) return [];
  return body.papers
    .map(normalizePaper)
    .filter((p): p is PaperDTO => p !== null);
}

/* -------------------------------------------------------------------------- */
/* 文档                                                                        */
/* -------------------------------------------------------------------------- */

export type ServerDocument = {
  id: string;
  groupId: string;
  groupSlug: string;
  title: string;
  status: "draft" | "active" | "archived";
  defaultRole: "owner" | "editor" | "viewer";
  wordCount: number;
  latestSnapshot: number;
  createdAt: string;
  updatedAt: string;
  createdBy: { id: string; name: string; handle: string };
};

/**
 * 服务端读取文档元数据与当前用户角色。
 *
 * 必须 no-store：响应里的 myRole 因人而异，若进入 Next 的共享 Data Cache
 * 会让不同用户看到彼此的角色。
 */
export const fetchDocument = cache(
  async (id: string): Promise<{ document: ServerDocument; myRole: "owner" | "editor" | "viewer" | null } | null> => {
    const body = await fetchJSON(`/documents/${encodeURIComponent(id)}`, false);
    if (typeof body !== "object" || body === null) return null;
    const record = body as Record<string, unknown>;
    if (typeof record.document !== "object" || record.document === null) return null;
    return {
      document: record.document as ServerDocument,
      myRole: (record.myRole as "owner" | "editor" | "viewer" | null) ?? null,
    };
  },
);

/* -------------------------------------------------------------------------- */
/* 策展                                                                        */
/* -------------------------------------------------------------------------- */

export type ServerCollectionItem = {
  id: string;
  position: number;
  note: string;
  post: PostDTO | null;
  paper: PaperDTO | null;
  addedBy: { id: string; name: string; handle: string };
  createdAt: string;
};

export type ServerCollection = {
  id: string;
  slug: string;
  title: string;
  description: string;
  coverUrl: string | null;
  curator: { id: string; name: string; handle: string; verifyType: string };
  itemCount: number;
  startsAt: string;
  endsAt: string | null;
  createdAt: string;
  canEdit: boolean;
};

function normalizeCollection(value: unknown): ServerCollection | null {
  if (!isRecord(value)) return null;
  const id = str(value.id);
  const slug = str(value.slug);
  if (!id || !slug) return null;

  const curator = isRecord(value.curator) ? value.curator : {};
  return {
    id,
    slug,
    title: str(value.title) || "未命名策展集",
    description: str(value.description),
    coverUrl: typeof value.coverUrl === "string" && value.coverUrl ? value.coverUrl : null,
    curator: {
      id: str(curator.id),
      name: str(curator.name) || "未知用户",
      handle: str(curator.handle),
      verifyType: str(curator.verifyType),
    },
    itemCount: num(value.itemCount),
    startsAt: str(value.startsAt),
    endsAt: typeof value.endsAt === "string" ? value.endsAt : null,
    createdAt: str(value.createdAt),
    canEdit: value.canEdit === true,
  };
}

function normalizeCollectionItem(value: unknown): ServerCollectionItem | null {
  if (!isRecord(value) || typeof value.id !== "string") return null;
  const addedBy = isRecord(value.addedBy) ? value.addedBy : {};
  return {
    id: value.id,
    position: num(value.position),
    note: str(value.note),
    post: normalizePost(value.post),
    paper: normalizePaper(value.paper),
    addedBy: {
      id: str(addedBy.id),
      name: str(addedBy.name) || "未知用户",
      handle: str(addedBy.handle),
    },
    createdAt: str(value.createdAt),
  };
}

/**
 * 读取策展集详情（公开读）。
 *
 * 必须 no-store：canEdit 随会话变化，且未上架的集合对非策展人返回 404；
 * 若进入共享 Data Cache，会把某个策展人的编辑入口渲染给别人。
 */
export const fetchCollection = cache(
  async (
    slug: string,
  ): Promise<{ collection: ServerCollection; items: ServerCollectionItem[] } | null> => {
    const body = await fetchJSON(`/collections/${encodeURIComponent(slug)}`, false);
    if (!isRecord(body)) return null;
    const collection = normalizeCollection(body.collection);
    if (!collection) return null;
    const items = Array.isArray(body.items)
      ? body.items
          .map(normalizeCollectionItem)
          .filter((item): item is ServerCollectionItem => item !== null)
      : [];
    return { collection, items };
  },
);
