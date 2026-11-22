/**
 * 运营后台 API 客户端。
 *
 * 契约：
 * - GET    /admin/overview?days=           概览统计（总量 + 趋势 + 热门话题）
 * - GET    /admin/users?q=&role=&status=   用户列表（游标分页）
 * - PATCH  /admin/users/{id}/role          改角色
 * - PATCH  /admin/users/{id}/status        封禁 / 解封
 * - GET    /admin/posts?q=&includeDeleted= 帖子列表
 * - DELETE /admin/posts/{id}               软删除帖子
 * - POST   /admin/posts/{id}/restore       恢复帖子
 * - GET    /admin/papers?q=                论文列表
 * - GET    /admin/groups?q=&includeDeleted= 群组列表
 * - DELETE /admin/groups/{id}              软删除群组
 * - POST   /admin/groups/{id}/restore      恢复群组
 * - GET    /admin/files?status=&onlyProblem= 文件排查
 *
 * 所有接口都要求 role='admin'：服务端在服务层判定，非运营返回 403。
 * 前端隐藏入口只是体验优化，不是安全边界。
 */

import { ApiError, apiFetch } from "@/lib/http";

/* -------------------------------------------------------------------------- */
/* 类型                                                                        */
/* -------------------------------------------------------------------------- */

export type AdminTotals = {
  users: number;
  activeUsers: number;
  admins: number;
  posts: number;
  publicPosts: number;
  comments: number;
  papers: number;
  papersWithPdf: number;
  groups: number;
  files: number;
  failedFiles: number;
  storageBytes: number;
  documents: number;
  pendingReports: number;
  blocks: number;
};

export type DailyActivity = {
  day: string;
  newUsers: number;
  newPosts: number;
  newPapers: number;
  newComments: number;
};

export type TagStat = { slug: string; name: string; postCount: number };

export type AdminOverview = {
  totals: AdminTotals;
  activity: DailyActivity[];
  topTags: TagStat[];
  generatedAt: string;
};

export type AdminUser = {
  id: string;
  email: string;
  name: string;
  handle: string;
  role: string;
  status: string;
  verifyType: string;
  institution: string;
  postCount: number;
  followerCount: number;
  createdAt: string;
  lastActiveAt: string | null;
};

export type AdminUserPage = {
  users: AdminUser[];
  nextCursor: string | null;
  total: number;
};

export type AdminPost = {
  id: string;
  authorId: string;
  authorHandle: string;
  authorName: string;
  type: string;
  visibility: string;
  groupId: string | null;
  excerpt: string;
  paperId: string | null;
  likeCount: number;
  commentCount: number;
  repostCount: number;
  createdAt: string;
  deletedAt: string | null;
};

export type AdminPostPage = { posts: AdminPost[]; nextCursor: string | null };

export type AdminPaper = {
  id: string;
  title: string;
  venue: string;
  publishedAt: string | null;
  citationCount: number;
  metaSource: string;
  pdfFileId: string | null;
  authorCount: number;
  postCount: number;
  createdAt: string;
};

export type AdminPaperPage = { papers: AdminPaper[]; nextCursor: string | null };

export type AdminGroup = {
  id: string;
  slug: string;
  name: string;
  description: string;
  ownerHandle: string;
  ownerName: string;
  joinPolicy: string;
  memberCount: number;
  documentCount: number;
  createdAt: string;
  deletedAt: string | null;
};

export type AdminGroupPage = { groups: AdminGroup[]; nextCursor: string | null };

export type AdminFile = {
  id: string;
  ownerId: string;
  ownerHandle: string;
  ownerName: string;
  originalName: string;
  kind: string;
  mime: string;
  sizeBytes: number;
  status: string;
  pageCount: number;
  errorReason: string;
  retryCount: number;
  createdAt: string;
  readyAt: string | null;
};

export type FileStat = { status: string; fileCount: number; bytes: number };

export type AdminFilePage = {
  files: AdminFile[];
  nextCursor: string | null;
  stats: FileStat[];
};

/* -------------------------------------------------------------------------- */
/* 内部工具                                                                    */
/* -------------------------------------------------------------------------- */

function isRecord(v: unknown): v is Record<string, unknown> {
  return typeof v === "object" && v !== null && !Array.isArray(v);
}

function num(v: unknown): number {
  return typeof v === "number" && Number.isFinite(v) ? v : 0;
}

function str(v: unknown): string {
  return typeof v === "string" ? v : "";
}

function strOrNull(v: unknown): string | null {
  return typeof v === "string" && v ? v : null;
}

function invalidResponse(): ApiError {
  return new ApiError("invalid_response", "服务器返回了无法识别的响应。", 0);
}

function nextCursorOf(body: unknown): string | null {
  if (!isRecord(body)) return null;
  return typeof body.nextCursor === "string" && body.nextCursor ? body.nextCursor : null;
}

function buildQuery(params: Record<string, string | number | boolean | undefined | null>): string {
  const sp = new URLSearchParams();
  for (const [key, value] of Object.entries(params)) {
    if (value === undefined || value === null || value === "") continue;
    sp.set(key, String(value));
  }
  const qs = sp.toString();
  return qs ? `?${qs}` : "";
}

function normalizeUser(v: unknown): AdminUser | null {
  if (!isRecord(v) || typeof v.id !== "string") return null;
  return {
    id: v.id,
    email: str(v.email),
    name: str(v.name),
    handle: str(v.handle),
    role: str(v.role) || "user",
    status: str(v.status) || "active",
    verifyType: str(v.verifyType),
    institution: str(v.institution),
    postCount: num(v.postCount),
    followerCount: num(v.followerCount),
    createdAt: str(v.createdAt),
    lastActiveAt: strOrNull(v.lastActiveAt),
  };
}

/* -------------------------------------------------------------------------- */
/* 概览                                                                        */
/* -------------------------------------------------------------------------- */

export async function getAdminOverview(days = 14): Promise<AdminOverview> {
  const body = await apiFetch(`/admin/overview${buildQuery({ days })}`);
  if (!isRecord(body) || !isRecord(body.totals)) throw invalidResponse();

  const t = body.totals;
  const totals: AdminTotals = {
    users: num(t.users),
    activeUsers: num(t.activeUsers),
    admins: num(t.admins),
    posts: num(t.posts),
    publicPosts: num(t.publicPosts),
    comments: num(t.comments),
    papers: num(t.papers),
    papersWithPdf: num(t.papersWithPdf),
    groups: num(t.groups),
    files: num(t.files),
    failedFiles: num(t.failedFiles),
    storageBytes: num(t.storageBytes),
    documents: num(t.documents),
    pendingReports: num(t.pendingReports),
    blocks: num(t.blocks),
  };

  const activity: DailyActivity[] = Array.isArray(body.activity)
    ? body.activity.filter(isRecord).map((d) => ({
        day: str(d.day),
        newUsers: num(d.newUsers),
        newPosts: num(d.newPosts),
        newPapers: num(d.newPapers),
        newComments: num(d.newComments),
      }))
    : [];

  const topTags: TagStat[] = Array.isArray(body.topTags)
    ? body.topTags.filter(isRecord).map((x) => ({
        slug: str(x.slug),
        name: str(x.name),
        postCount: num(x.postCount),
      }))
    : [];

  return { totals, activity, topTags, generatedAt: str(body.generatedAt) };
}

/* -------------------------------------------------------------------------- */
/* 用户                                                                        */
/* -------------------------------------------------------------------------- */

export type UserQuery = {
  q?: string;
  role?: string;
  status?: string;
  cursor?: string | null;
  limit?: number;
};

export async function listAdminUsers(query: UserQuery = {}): Promise<AdminUserPage> {
  const body = await apiFetch(
    `/admin/users${buildQuery({
      q: query.q,
      role: query.role,
      status: query.status,
      cursor: query.cursor,
      limit: query.limit,
    })}`,
  );
  if (!isRecord(body) || !Array.isArray(body.users)) throw invalidResponse();
  return {
    users: body.users.map(normalizeUser).filter((u): u is AdminUser => u !== null),
    nextCursor: nextCursorOf(body),
    total: num(body.total),
  };
}

/** 改角色。服务端会拒绝「撤销自己的管理员权限」与「移除最后一名管理员」。 */
export async function setAdminUserRole(id: string, role: "user" | "admin"): Promise<void> {
  await apiFetch(`/admin/users/${encodeURIComponent(id)}/role`, {
    method: "PATCH",
    body: { role },
  });
}

/** 封禁 / 解封。封禁立即生效，现有会话在下一次请求即失效。 */
export async function setAdminUserStatus(
  id: string,
  status: "active" | "suspended",
): Promise<void> {
  await apiFetch(`/admin/users/${encodeURIComponent(id)}/status`, {
    method: "PATCH",
    body: { status },
  });
}

/* -------------------------------------------------------------------------- */
/* 内容                                                                        */
/* -------------------------------------------------------------------------- */

export type ContentQuery = {
  q?: string;
  cursor?: string | null;
  limit?: number;
  includeDeleted?: boolean;
};

export async function listAdminPosts(query: ContentQuery = {}): Promise<AdminPostPage> {
  const body = await apiFetch(
    `/admin/posts${buildQuery({
      q: query.q,
      cursor: query.cursor,
      limit: query.limit,
      includeDeleted: query.includeDeleted ? "true" : undefined,
    })}`,
  );
  if (!isRecord(body) || !Array.isArray(body.posts)) throw invalidResponse();
  return {
    posts: body.posts.filter(isRecord).map((p) => ({
      id: str(p.id),
      authorId: str(p.authorId),
      authorHandle: str(p.authorHandle),
      authorName: str(p.authorName),
      type: str(p.type),
      visibility: str(p.visibility),
      groupId: strOrNull(p.groupId),
      excerpt: str(p.excerpt),
      paperId: strOrNull(p.paperId),
      likeCount: num(p.likeCount),
      commentCount: num(p.commentCount),
      repostCount: num(p.repostCount),
      createdAt: str(p.createdAt),
      deletedAt: strOrNull(p.deletedAt),
    })),
    nextCursor: nextCursorOf(body),
  };
}

export async function deleteAdminPost(id: string): Promise<void> {
  await apiFetch(`/admin/posts/${encodeURIComponent(id)}`, { method: "DELETE" });
}

export async function restoreAdminPost(id: string): Promise<void> {
  await apiFetch(`/admin/posts/${encodeURIComponent(id)}/restore`, { method: "POST" });
}

export async function listAdminPapers(query: ContentQuery = {}): Promise<AdminPaperPage> {
  const body = await apiFetch(
    `/admin/papers${buildQuery({
      q: query.q,
      cursor: query.cursor,
      limit: query.limit,
    })}`,
  );
  if (!isRecord(body) || !Array.isArray(body.papers)) throw invalidResponse();
  return {
    papers: body.papers.filter(isRecord).map((p) => ({
      id: str(p.id),
      title: str(p.title),
      venue: str(p.venue),
      publishedAt: strOrNull(p.publishedAt),
      citationCount: num(p.citationCount),
      metaSource: str(p.metaSource),
      pdfFileId: strOrNull(p.pdfFileId),
      authorCount: num(p.authorCount),
      postCount: num(p.postCount),
      createdAt: str(p.createdAt),
    })),
    nextCursor: nextCursorOf(body),
  };
}

export async function listAdminGroups(query: ContentQuery = {}): Promise<AdminGroupPage> {
  const body = await apiFetch(
    `/admin/groups${buildQuery({
      q: query.q,
      cursor: query.cursor,
      limit: query.limit,
      includeDeleted: query.includeDeleted ? "true" : undefined,
    })}`,
  );
  if (!isRecord(body) || !Array.isArray(body.groups)) throw invalidResponse();
  return {
    groups: body.groups.filter(isRecord).map((g) => ({
      id: str(g.id),
      slug: str(g.slug),
      name: str(g.name),
      description: str(g.description),
      ownerHandle: str(g.ownerHandle),
      ownerName: str(g.ownerName),
      joinPolicy: str(g.joinPolicy),
      memberCount: num(g.memberCount),
      documentCount: num(g.documentCount),
      createdAt: str(g.createdAt),
      deletedAt: strOrNull(g.deletedAt),
    })),
    nextCursor: nextCursorOf(body),
  };
}

export async function deleteAdminGroup(id: string): Promise<void> {
  await apiFetch(`/admin/groups/${encodeURIComponent(id)}`, { method: "DELETE" });
}

export async function restoreAdminGroup(id: string): Promise<void> {
  await apiFetch(`/admin/groups/${encodeURIComponent(id)}/restore`, { method: "POST" });
}

/* -------------------------------------------------------------------------- */
/* 文件                                                                        */
/* -------------------------------------------------------------------------- */

export type FileQuery = {
  status?: string;
  onlyProblem?: boolean;
  cursor?: string | null;
  limit?: number;
};

export async function listAdminFiles(query: FileQuery = {}): Promise<AdminFilePage> {
  const body = await apiFetch(
    `/admin/files${buildQuery({
      status: query.status,
      onlyProblem: query.onlyProblem ? "true" : undefined,
      cursor: query.cursor,
      limit: query.limit,
    })}`,
  );
  if (!isRecord(body) || !Array.isArray(body.files)) throw invalidResponse();
  return {
    files: body.files.filter(isRecord).map((f) => ({
      id: str(f.id),
      ownerId: str(f.ownerId),
      ownerHandle: str(f.ownerHandle),
      ownerName: str(f.ownerName),
      originalName: str(f.originalName),
      kind: str(f.kind),
      mime: str(f.mime),
      sizeBytes: num(f.sizeBytes),
      status: str(f.status),
      pageCount: num(f.pageCount),
      errorReason: str(f.errorReason),
      retryCount: num(f.retryCount),
      createdAt: str(f.createdAt),
      readyAt: strOrNull(f.readyAt),
    })),
    nextCursor: nextCursorOf(body),
    stats: Array.isArray(body.stats)
      ? body.stats.filter(isRecord).map((s) => ({
          status: str(s.status),
          fileCount: num(s.fileCount),
          bytes: num(s.bytes),
        }))
      : [],
  };
}
