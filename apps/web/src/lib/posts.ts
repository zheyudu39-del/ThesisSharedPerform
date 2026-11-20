/**
 * 帖子 / 信息流 / 互动 API 客户端。
 *
 * 与后端契约对齐（见任务约定的 API 契约），请求走同源 `/api/v1/*`：
 * - 发帖、点赞、收藏、评论：必须带令牌；
 * - 信息流（all / hot）、评论列表、帖子详情：令牌可选——未登录也尝试请求，
 *   若服务端返回 401 由调用方展示登录引导。
 *
 * 错误统一抛 {@link AuthError}（code / message / status），与 lib/papers.ts 一致，
 * 页面只需读取 message 即可展示可读中文提示。
 */

import { AuthError, getToken } from "@/lib/auth";
import type { PaperDTO } from "@/lib/papers";

export type { PaperDTO };

export type PostVisibility = "public" | "followers" | "group" | "private";
export type PostType = "original" | "repost";
export type FeedStream = "following" | "all" | "hot";

export type PostAuthor = {
  id: string;
  name: string;
  handle: string;
  verifyType: string;
};

/** 转发目标的降级快照（只到这里为止，因此转发层级天然最多两层）。 */
export type RepostRef = {
  id: string;
  authorName: string;
  contentText: string;
  paper: PaperDTO | null;
};

export type PostDTO = {
  id: string;
  type: PostType;
  author: PostAuthor;
  contentHtml: string;
  contentText: string;
  paper: PaperDTO | null;
  repostOf: RepostRef | null;
  groupId: string | null;
  visibility: PostVisibility;
  likeCount: number;
  commentCount: number;
  repostCount: number;
  hotScore: number;
  createdAt: string;
  liked: boolean;
  bookmarked: boolean;
  tags: string[];
};

export type CommentDTO = {
  id: string;
  postId: string;
  parentId: string | null;
  author: { id: string; name: string; handle: string };
  contentHtml: string;
  contentText: string;
  likeCount: number;
  createdAt: string;
};

export type FeedPage = {
  posts: PostDTO[];
  nextCursor: string | null;
};

export type CommentPage = {
  comments: CommentDTO[];
  nextCursor: string | null;
};

export type CreatePostInput = {
  content: string;
  paperId?: string;
  repostOf?: string;
  groupId?: string;
  visibility: PostVisibility;
  tags?: string[];
};

export type CreateCommentInput = {
  content: string;
  parentId?: string | null;
};

/* -------------------------------------------------------------------------- */
/* 请求与解析                                                                  */
/* -------------------------------------------------------------------------- */

type AuthMode = "required" | "optional";

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function str(value: unknown): string {
  return typeof value === "string" ? value : "";
}

function num(value: unknown): number {
  return typeof value === "number" && Number.isFinite(value) ? value : 0;
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

function fallbackMessage(status: number): string {
  switch (status) {
    case 401:
      return "登录状态已失效，请重新登录。";
    case 403:
      return "没有权限执行此操作。";
    case 404:
      return "内容不存在，或已被删除。";
    case 422:
      return "提交的内容未通过校验。";
    case 429:
      return "操作过于频繁，请稍后再试。";
    default:
      return status >= 500 ? "服务器暂时不可用，请稍后重试。" : "请求失败，请稍后重试。";
  }
}

async function requestJSON(path: string, init: RequestInit, auth: AuthMode): Promise<unknown> {
  const token = getToken();
  if (auth === "required" && !token) {
    throw new AuthError("unauthorized", "请先登录后再继续。", 401);
  }

  const headers = new Headers(init.headers);
  headers.set("Accept", "application/json");
  if (token) headers.set("Authorization", `Bearer ${token}`);
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
    const error = extractError(body, response.status);
    throw new AuthError(error.code, error.message, response.status);
  }

  return body;
}

function invalidResponse(): AuthError {
  return new AuthError("invalid_response", "服务器返回了无法识别的响应。", 0);
}

/**
 * 信息流 404 的语义是「后端路由尚未上线」，而不是「这一页不存在」，
 * 因此换成更准确的中文提示。
 */
function feedUnavailable(): AuthError {
  return new AuthError("feed_unavailable", "信息流接口尚未开放，请稍后再试。", 404);
}

function normalizePaper(value: unknown): PaperDTO | null {
  if (!isRecord(value) || typeof value.id !== "string") return null;
  const authors = Array.isArray(value.authors)
    ? value.authors.filter(isRecord).map((a) => ({
        name: str(a.name),
        affiliation: typeof a.affiliation === "string" ? a.affiliation : undefined,
        orcid: typeof a.orcid === "string" ? a.orcid : undefined,
      }))
    : [];
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
  };
}

function normalizePostAuthor(value: unknown): PostAuthor {
  if (!isRecord(value)) {
    return { id: "", name: "未知用户", handle: "", verifyType: "" };
  }
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

function normalizePost(value: unknown): PostDTO {
  if (!isRecord(value) || typeof value.id !== "string") throw invalidResponse();

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
    tags: Array.isArray(value.tags) ? value.tags.filter((t): t is string => typeof t === "string") : [],
  };
}

function normalizeComment(value: unknown): CommentDTO {
  if (!isRecord(value) || typeof value.id !== "string") throw invalidResponse();
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

function normalizePostList(body: unknown): FeedPage {
  if (!isRecord(body) || !Array.isArray(body.posts)) throw invalidResponse();
  return {
    posts: body.posts.map(normalizePost),
    nextCursor: typeof body.nextCursor === "string" && body.nextCursor ? body.nextCursor : null,
  };
}

/* -------------------------------------------------------------------------- */
/* 信息流                                                                      */
/* -------------------------------------------------------------------------- */

/**
 * 拉取一页信息流。
 *
 * 后端 `/feed` 整体位于鉴权中间件之后，三档流都需要令牌；调用前页面已按登录态
 * 决定是否渲染。游标是不透明字符串，原样回传即可。
 */
export async function getFeed(
  stream: FeedStream,
  cursor: string | null = null,
  limit = 20,
): Promise<FeedPage> {
  const params = new URLSearchParams({ stream, limit: String(Math.min(Math.max(limit, 1), 50)) });
  if (cursor) params.set("cursor", cursor);

  // 后端把 /feed 整体挂在 Authenticate 之后：三档流都要求登录。
  const auth: AuthMode = "required";
  let body: unknown;
  try {
    body = await requestJSON(
      `/feed?${params.toString()}`,
      { method: "GET", cache: "no-store" },
      auth,
    );
  } catch (err) {
    if (err instanceof AuthError && err.status === 404) throw feedUnavailable();
    throw err;
  }
  return normalizePostList(body);
}

/**
 * 单个帖子详情。
 *
 * 令牌可选：未登录也能尝试读取公开帖；服务端对「不存在」与「无权查看」
 * 返回相同的 404（post_not_found），避免通过状态码探测私密帖是否存在。
 *
 * 详情页优先调用本接口，失败时才回退到 {@link recallPost} 的会话内快照，
 * 因此冷启动直接打开分享链接也能正常渲染。
 */
export async function getPost(id: string): Promise<PostDTO> {
  const body = await requestJSON(
    `/posts/${encodeURIComponent(id)}`,
    { method: "GET", cache: "no-store" },
    "optional",
  );
  if (!isRecord(body) || !("post" in body)) throw invalidResponse();
  return normalizePost(body.post);
}

/* -------------------------------------------------------------------------- */
/* 会话内帖子缓存（详情页直出，避免额外接口与闪烁）                            */
/* -------------------------------------------------------------------------- */

const POST_CACHE_PREFIX = "thesis_post_";

/** 记住一篇帖子，供详情页在无详情接口时直接渲染。 */
export function rememberPost(post: PostDTO): void {
  if (typeof window === "undefined") return;
  try {
    window.sessionStorage.setItem(`${POST_CACHE_PREFIX}${post.id}`, JSON.stringify(post));
  } catch {
    // 隐私模式 / 存储已满：详情页会退回网络请求。
  }
}

/** 读取会话内缓存的帖子；格式不符时返回 null。 */
export function recallPost(id: string): PostDTO | null {
  if (typeof window === "undefined") return null;
  try {
    const raw = window.sessionStorage.getItem(`${POST_CACHE_PREFIX}${id}`);
    if (!raw) return null;
    return normalizePost(JSON.parse(raw) as unknown);
  } catch {
    return null;
  }
}

/* -------------------------------------------------------------------------- */
/* 发帖 / 转发                                                                 */
/* -------------------------------------------------------------------------- */

export async function createPost(input: CreatePostInput): Promise<PostDTO> {
  const payload: Record<string, unknown> = {
    content: input.content,
    visibility: input.visibility,
  };
  if (input.paperId) payload.paperId = input.paperId;
  if (input.repostOf) payload.repostOf = input.repostOf;
  if (input.groupId) payload.groupId = input.groupId;
  if (input.tags && input.tags.length > 0) payload.tags = input.tags;

  const body = await requestJSON(
    "/posts",
    { method: "POST", body: JSON.stringify(payload) },
    "required",
  );
  if (!isRecord(body) || !("post" in body)) throw invalidResponse();
  return normalizePost(body.post);
}

/* -------------------------------------------------------------------------- */
/* 互动                                                                        */
/* -------------------------------------------------------------------------- */

export async function likePost(id: string): Promise<{ liked: boolean; likeCount: number }> {
  const body = await requestJSON(
    `/posts/${encodeURIComponent(id)}/like`,
    { method: "POST" },
    "required",
  );
  if (!isRecord(body)) throw invalidResponse();
  return { liked: body.liked === true, likeCount: num(body.likeCount) };
}

export async function unlikePost(id: string): Promise<{ liked: boolean; likeCount: number }> {
  const body = await requestJSON(
    `/posts/${encodeURIComponent(id)}/like`,
    { method: "DELETE" },
    "required",
  );
  if (!isRecord(body)) throw invalidResponse();
  return { liked: body.liked === true, likeCount: num(body.likeCount) };
}

export async function bookmarkPost(id: string): Promise<{ bookmarked: boolean }> {
  const body = await requestJSON(
    `/posts/${encodeURIComponent(id)}/bookmark`,
    { method: "POST" },
    "required",
  );
  return { bookmarked: isRecord(body) && body.bookmarked === true };
}

export async function unbookmarkPost(id: string): Promise<{ bookmarked: boolean }> {
  const body = await requestJSON(
    `/posts/${encodeURIComponent(id)}/bookmark`,
    { method: "DELETE" },
    "required",
  );
  return { bookmarked: isRecord(body) && body.bookmarked === true };
}

/* -------------------------------------------------------------------------- */
/* 评论                                                                        */
/* -------------------------------------------------------------------------- */

export async function getComments(
  postId: string,
  cursor: string | null = null,
  limit = 20,
): Promise<CommentPage> {
  const params = new URLSearchParams({ limit: String(Math.min(Math.max(limit, 1), 50)) });
  if (cursor) params.set("cursor", cursor);

  const body = await requestJSON(
    `/posts/${encodeURIComponent(postId)}/comments?${params.toString()}`,
    { method: "GET", cache: "no-store" },
    "optional",
  );
  if (!isRecord(body) || !Array.isArray(body.comments)) throw invalidResponse();
  return {
    comments: body.comments.map(normalizeComment),
    nextCursor: typeof body.nextCursor === "string" && body.nextCursor ? body.nextCursor : null,
  };
}

export async function createComment(
  postId: string,
  input: CreateCommentInput,
): Promise<CommentDTO> {
  const payload: Record<string, unknown> = { content: input.content };
  // 契约允许显式传 null 表示一级评论；只在有父评论时才带上字段。
  if (input.parentId) payload.parentId = input.parentId;

  const body = await requestJSON(
    `/posts/${encodeURIComponent(postId)}/comments`,
    { method: "POST", body: JSON.stringify(payload) },
    "required",
  );
  if (!isRecord(body) || !("comment" in body)) throw invalidResponse();
  return normalizeComment(body.comment);
}

/* -------------------------------------------------------------------------- */
/* 展示辅助                                                                    */
/* -------------------------------------------------------------------------- */

/** 相对时间：刚刚 / N 分钟前 / N 小时前 / N 天前 / MM-DD / YYYY-MM-DD。 */
export function formatRelativeTime(value: string): string {
  const time = Date.parse(value);
  if (Number.isNaN(time)) return "";
  const diff = Date.now() - time;
  if (diff <= 60_000) return "刚刚";
  const minutes = Math.floor(diff / 60_000);
  if (minutes < 60) return `${minutes} 分钟前`;
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return `${hours} 小时前`;
  const days = Math.floor(hours / 24);
  if (days < 7) return `${days} 天前`;
  const date = new Date(time);
  const month = String(date.getMonth() + 1).padStart(2, "0");
  const day = String(date.getDate()).padStart(2, "0");
  return date.getFullYear() === new Date().getFullYear()
    ? `${month}-${day}`
    : `${date.getFullYear()}-${month}-${day}`;
}

/** 论文作者列表 → 单行文本（最多 max 位，其余用「等」）。 */
export function paperAuthorsText(paper: PaperDTO, max = 5): string {
  const names = paper.authors.map((a) => a.name).filter(Boolean);
  if (names.length === 0) return "未知作者";
  const head = names.slice(0, max).join("、");
  return names.length > max ? `${head} 等` : head;
}

/** 论文标识：优先 DOI，其次 arXiv。 */
export function paperIdentifier(paper: PaperDTO): string {
  if (paper.doi) return paper.doi;
  if (paper.arxivId) return `arXiv:${paper.arxivId}`;
  if (paper.openreviewId) return `OpenReview:${paper.openreviewId}`;
  return "";
}

export const VISIBILITY_LABEL: Record<PostVisibility, string> = {
  public: "公开",
  followers: "仅关注者",
  group: "仅群组",
  private: "仅自己",
};
