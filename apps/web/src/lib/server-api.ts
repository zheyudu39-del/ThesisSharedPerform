/**
 * 服务端专用 API 客户端（RSC / generateMetadata / sitemap）。
 *
 * 为什么不能复用 lib/papers.ts、lib/posts.ts、lib/users.ts：
 * 它们通过 `getToken()` 读取 localStorage，而 localStorage 在服务端不存在；
 * 并且它们的请求地址是相对路径 `/api/v1/*`，服务端 fetch 必须用绝对地址。
 *
 * 约定：
 * - 只读、匿名请求；公开读接口（papers / posts / users）后端已允许匿名访问；
 * - 任何失败（404、网络错误、超时、5xx）都返回 null / []，由调用方决定
 *   notFound() 或降级渲染，绝不让一个坏掉的接口拖垮整页；
 * - 用 React `cache` 在单次请求内去重：generateMetadata 与页面各调用一次，
 *   实际只发一次网络请求。
 */

import { cache } from "react";
import type { PaperAuthor, PaperDTO } from "@/lib/papers";
import type {
  CommentDTO,
  CommentPage,
  PostAuthor,
  PostDTO,
  PostVisibility,
  RepostRef,
} from "@/lib/posts";
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

async function fetchJSON(path: string): Promise<unknown | null> {
  try {
    const response = await fetch(`${API_BASE}/api/v1${path}`, {
      headers: { Accept: "application/json" },
      next: { revalidate: REVALIDATE_SECONDS },
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
