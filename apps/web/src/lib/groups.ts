/**
 * 群组 API 客户端（M2）。
 *
 * 与后端契约对齐（见任务约定的 API 契约），请求走同源 `/api/v1/*`，
 * 由 {@link apiFetch} 自动携带 HttpOnly 会话 Cookie 与 CSRF 双提交令牌。
 *
 * 约定：
 * - 群组详情 / 群组列表是公开读接口，未登录也能读取；成员列表、邀请码、
 *   入群申请与群内动态需要登录（后端返回 401 时由调用方展示登录引导）；
 * - 错误统一抛 {@link ApiError}（code / message / status），页面直接展示 message；
 * - 所有响应都经过归一化，缺字段时给出安全默认值，避免一个脏字段拖垮整页。
 */

import { ApiError, apiFetch } from "@/lib/http";
import { normalizePost, type PostDTO } from "@/lib/posts";

/* -------------------------------------------------------------------------- */
/* 类型                                                                        */
/* -------------------------------------------------------------------------- */

export type JoinPolicy = "public" | "request" | "invite";
export type GroupRole = "owner" | "admin" | "member";

/** 与后端 GroupDTO 对齐。 */
export type GroupDTO = {
  id: string;
  name: string;
  slug: string;
  description: string;
  coverUrl: string | null;
  ownerId: string;
  ownerName: string;
  joinPolicy: JoinPolicy;
  memberCount: number;
  myRole: GroupRole | null;
  createdAt: string;
};

/** GET /groups/{slug} 的完整返回：群组 + 当前用户身份 + 成员数。 */
export type GroupDetail = {
  group: GroupDTO;
  myRole: GroupRole | null;
  memberCount: number;
};

/** 与后端 MemberDTO 对齐。 */
export type MemberDTO = {
  userId: string;
  name: string;
  handle: string;
  role: GroupRole;
  joinedAt: string;
  verifyType: string;
};

export type GroupInviteDTO = {
  code: string;
  expiresAt: string | null;
  maxUses: number | null;
};

/** 入群申请。列表接口不返回状态字段，按 pending 展示。 */
export type JoinRequestDTO = {
  id: string;
  user: { name: string; handle: string };
  message: string;
  createdAt: string;
};

export type GroupListPage = {
  groups: GroupDTO[];
  nextCursor: string | null;
};

export type MemberPage = {
  members: MemberDTO[];
  nextCursor: string | null;
};

export type GroupFeedPage = {
  posts: PostDTO[];
  nextCursor: string | null;
};

export type CreateGroupInput = {
  name: string;
  description?: string;
  joinPolicy: JoinPolicy;
};

export type UpdateGroupInput = {
  name?: string;
  description?: string;
  joinPolicy?: JoinPolicy;
};

export type CreateInviteInput = {
  /** 默认由后端决定（168 小时）。 */
  expiresInHours?: number;
  maxUses?: number;
};

/* -------------------------------------------------------------------------- */
/* 展示标签（与后端枚举严格对应）                                              */
/* -------------------------------------------------------------------------- */

export const JOIN_POLICY_LABEL: Record<JoinPolicy, string> = {
  public: "公开加入",
  request: "申请加入",
  invite: "仅邀请",
};

/** 列表 / 详情页对加入方式的解释。 */
export const JOIN_POLICY_HINT: Record<JoinPolicy, string> = {
  public: "任何登录用户都可以直接加入。",
  request: "提交申请，由群主或管理员审批后加入。",
  invite: "需要群主或管理员生成的邀请码才能加入。",
};

export const GROUP_ROLE_LABEL: Record<GroupRole, string> = {
  owner: "群主",
  admin: "管理员",
  member: "成员",
};

/** 与后端校验规则保持一致（name 1..64、description ≤500）。 */
export const GROUP_LIMITS = {
  nameMax: 64,
  descriptionMax: 500,
  requestMessageMax: 280,
} as const;

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

function normalizeJoinPolicy(value: unknown): JoinPolicy {
  return value === "public" || value === "request" || value === "invite" ? value : "invite";
}

function normalizeRole(value: unknown): GroupRole | null {
  return value === "owner" || value === "admin" || value === "member" ? value : null;
}

function invalidResponse(): ApiError {
  return new ApiError("invalid_response", "服务器返回了无法识别的响应。", 0);
}

function normalizeGroup(value: unknown): GroupDTO {
  if (!isRecord(value)) throw invalidResponse();
  const id = str(value.id);
  if (!id) throw invalidResponse();

  return {
    id,
    name: str(value.name) || "未命名群组",
    // slug 缺失时退回 id：详情页路由仍可工作（后端通常两者都有）。
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

function normalizeMember(value: unknown): MemberDTO {
  if (!isRecord(value)) throw invalidResponse();
  const userId = str(value.userId);
  if (!userId) throw invalidResponse();
  return {
    userId,
    name: str(value.name) || str(value.handle) || "未知成员",
    handle: str(value.handle),
    role: normalizeRole(value.role) ?? "member",
    joinedAt: str(value.joinedAt),
    verifyType: str(value.verifyType),
  };
}

function normalizeJoinRequest(value: unknown): JoinRequestDTO {
  if (!isRecord(value)) throw invalidResponse();
  const id = str(value.id);
  if (!id) throw invalidResponse();
  const user = isRecord(value.user) ? value.user : {};
  return {
    id,
    user: { name: str(user.name) || "未知用户", handle: str(user.handle) },
    message: str(value.message),
    createdAt: str(value.createdAt),
  };
}

function normalizeInvite(value: unknown): GroupInviteDTO {
  if (!isRecord(value)) throw invalidResponse();
  const code = str(value.code);
  if (!code) throw invalidResponse();
  return {
    code,
    expiresAt: typeof value.expiresAt === "string" && value.expiresAt ? value.expiresAt : null,
    maxUses: typeof value.maxUses === "number" && Number.isFinite(value.maxUses) ? value.maxUses : null,
  };
}

function nextCursorOf(body: Record<string, unknown>): string | null {
  return typeof body.nextCursor === "string" && body.nextCursor ? body.nextCursor : null;
}

function clampLimit(limit: number): number {
  return Math.min(Math.max(limit, 1), 50);
}

/* -------------------------------------------------------------------------- */
/* 群组 CRUD                                                                   */
/* -------------------------------------------------------------------------- */

/** 创建群组；成功后调用者通常是群主。 */
export async function createGroup(input: CreateGroupInput): Promise<GroupDTO> {
  const payload: Record<string, unknown> = {
    name: input.name,
    joinPolicy: input.joinPolicy,
  };
  if (input.description !== undefined) payload.description = input.description;

  const body = await apiFetch("/groups", { method: "POST", body: payload });
  if (!isRecord(body) || !("group" in body)) throw invalidResponse();
  return normalizeGroup(body.group);
}

/** 读取群组详情（公开）；响应里的 myRole / memberCount 覆盖群组自身的同名字段。 */
export async function getGroup(slug: string): Promise<GroupDetail> {
  const body = await apiFetch(`/groups/${encodeURIComponent(slug)}`);
  if (!isRecord(body) || !("group" in body)) throw invalidResponse();

  const group = normalizeGroup(body.group);
  const myRole = normalizeRole(body.myRole) ?? group.myRole;
  const memberCount =
    typeof body.memberCount === "number" && Number.isFinite(body.memberCount)
      ? num(body.memberCount)
      : group.memberCount;

  return {
    group: { ...group, myRole, memberCount },
    myRole,
    memberCount,
  };
}

/** 列出公开群组（joinPolicy='public'），按后端排序游标翻页。 */
export async function listGroups(
  cursor: string | null = null,
  limit = 20,
): Promise<GroupListPage> {
  const params = new URLSearchParams({ limit: String(clampLimit(limit)) });
  if (cursor) params.set("cursor", cursor);

  const body = await apiFetch(`/groups?${params.toString()}`);
  if (!isRecord(body) || !Array.isArray(body.groups)) throw invalidResponse();
  return {
    groups: body.groups.map(normalizeGroup),
    nextCursor: nextCursorOf(body),
  };
}

/** 更新群组（仅 owner/admin）；只发送出现的字段。 */
export async function updateGroup(slug: string, input: UpdateGroupInput): Promise<GroupDTO> {
  const body = await apiFetch(`/groups/${encodeURIComponent(slug)}`, {
    method: "PATCH",
    body: input,
  });
  if (!isRecord(body) || !("group" in body)) throw invalidResponse();
  return normalizeGroup(body.group);
}

/* -------------------------------------------------------------------------- */
/* 成员                                                                        */
/* -------------------------------------------------------------------------- */

export async function listMembers(
  slug: string,
  cursor: string | null = null,
  limit = 20,
): Promise<MemberPage> {
  const params = new URLSearchParams({ limit: String(clampLimit(limit)) });
  if (cursor) params.set("cursor", cursor);

  const body = await apiFetch(`/groups/${encodeURIComponent(slug)}/members?${params.toString()}`);
  if (!isRecord(body) || !Array.isArray(body.members)) throw invalidResponse();
  return {
    members: body.members.map(normalizeMember),
    nextCursor: nextCursorOf(body),
  };
}

/** 修改成员角色（仅 owner/admin；owner 的角色不可修改，后端返回 422）。 */
export async function updateMemberRole(
  slug: string,
  userId: string,
  role: Exclude<GroupRole, "owner">,
): Promise<MemberDTO> {
  const body = await apiFetch(
    `/groups/${encodeURIComponent(slug)}/members/${encodeURIComponent(userId)}`,
    { method: "PATCH", body: { role } },
  );
  if (!isRecord(body) || !("member" in body)) throw invalidResponse();
  return normalizeMember(body.member);
}

/** 移除成员（owner/admin），或本人退出群组。204 无响应体。 */
export async function removeMember(slug: string, userId: string): Promise<void> {
  await apiFetch(
    `/groups/${encodeURIComponent(slug)}/members/${encodeURIComponent(userId)}`,
    { method: "DELETE" },
  );
}

/* -------------------------------------------------------------------------- */
/* 邀请                                                                        */
/* -------------------------------------------------------------------------- */

/** 生成邀请码（仅 owner/admin）。 */
export async function createInvite(
  slug: string,
  input: CreateInviteInput = {},
): Promise<GroupInviteDTO> {
  const payload: Record<string, unknown> = {};
  if (input.expiresInHours !== undefined) payload.expiresInHours = input.expiresInHours;
  if (input.maxUses !== undefined) payload.maxUses = input.maxUses;

  const body = await apiFetch(`/groups/${encodeURIComponent(slug)}/invites`, {
    method: "POST",
    body: payload,
  });
  if (!isRecord(body) || !("invite" in body)) throw invalidResponse();
  return normalizeInvite(body.invite);
}

/** 用邀请码加入群组；404 invite_not_found、409 already_member。 */
export async function joinByCode(code: string): Promise<GroupDTO> {
  const body = await apiFetch("/groups/join", {
    method: "POST",
    body: { code: code.trim() },
  });
  if (!isRecord(body) || !("group" in body)) throw invalidResponse();
  return normalizeGroup(body.group);
}

/**
 * 公开群组直接加入。
 *
 * 注意：任务给出的契约只列了邀请码加入与入群申请，没有单列「公开群加入」。
 * 这里按 REST 语义调用 `POST /groups/{slug}/join`（期望返回 `{group}`），
 * 与后端对接时若接口名不同，只需改这一处。
 */
export async function joinPublicGroup(slug: string): Promise<GroupDTO> {
  const body = await apiFetch(`/groups/${encodeURIComponent(slug)}/join`, { method: "POST" });
  if (!isRecord(body) || !("group" in body)) throw invalidResponse();
  return normalizeGroup(body.group);
}

/* -------------------------------------------------------------------------- */
/* 入群申请                                                                    */
/* -------------------------------------------------------------------------- */

/** 提交入群申请（joinPolicy='request'）；409 already_requested / already_member。 */
export async function requestJoin(
  slug: string,
  message?: string,
): Promise<{ id: string; status: string }> {
  const payload: Record<string, unknown> = {};
  if (message && message.trim()) payload.message = message.trim();

  const body = await apiFetch(`/groups/${encodeURIComponent(slug)}/requests`, {
    method: "POST",
    body: payload,
  });
  if (!isRecord(body) || !isRecord(body.request)) throw invalidResponse();
  return {
    id: str(body.request.id),
    status: str(body.request.status) || "pending",
  };
}

/** 待审批申请列表（仅 owner/admin）。 */
export async function listJoinRequests(slug: string): Promise<JoinRequestDTO[]> {
  const body = await apiFetch(`/groups/${encodeURIComponent(slug)}/requests`);
  if (!isRecord(body) || !Array.isArray(body.requests)) throw invalidResponse();
  return body.requests.map(normalizeJoinRequest);
}

async function reviewRequest(
  slug: string,
  requestId: string,
  action: "approve" | "reject",
): Promise<JoinRequestDTO | null> {
  const body = await apiFetch(
    `/groups/${encodeURIComponent(slug)}/requests/${encodeURIComponent(requestId)}/${action}`,
    { method: "POST" },
  );
  if (!isRecord(body) || !isRecord(body.request)) return null;
  // 响应体可能是精简的 {id,status}，能归一化就归一化，否则由调用方保留原行数据。
  try {
    return normalizeJoinRequest(body.request);
  } catch {
    return null;
  }
}

/** 批准入群申请（仅 owner/admin）。 */
export async function approveRequest(
  slug: string,
  requestId: string,
): Promise<JoinRequestDTO | null> {
  return reviewRequest(slug, requestId, "approve");
}

/** 拒绝入群申请（仅 owner/admin）。 */
export async function rejectRequest(
  slug: string,
  requestId: string,
): Promise<JoinRequestDTO | null> {
  return reviewRequest(slug, requestId, "reject");
}

/* -------------------------------------------------------------------------- */
/* 群内动态                                                                    */
/* -------------------------------------------------------------------------- */

/** 群内帖子（仅成员）。复用 content 模块的 PostDTO。 */
export async function getGroupFeed(
  slug: string,
  cursor: string | null = null,
  limit = 20,
): Promise<GroupFeedPage> {
  const params = new URLSearchParams({ limit: String(clampLimit(limit)) });
  if (cursor) params.set("cursor", cursor);

  const body = await apiFetch(`/groups/${encodeURIComponent(slug)}/feed?${params.toString()}`);
  if (!isRecord(body) || !Array.isArray(body.posts)) throw invalidResponse();
  return {
    posts: body.posts.map(normalizePost),
    nextCursor: nextCursorOf(body),
  };
}

/* -------------------------------------------------------------------------- */
/* 展示辅助                                                                    */
/* -------------------------------------------------------------------------- */

/** 绝对时间：YYYY-MM-DD HH:mm（本地时区）。 */
export function formatDateTime(value: string): string {
  const time = Date.parse(value);
  if (Number.isNaN(time)) return "";
  const date = new Date(time);
  const pad = (n: number) => String(n).padStart(2, "0");
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())} ${pad(
    date.getHours(),
  )}:${pad(date.getMinutes())}`;
}
