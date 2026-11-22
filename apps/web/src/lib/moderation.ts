/**
 * 举报与屏蔽 API 客户端（PRD F-06）。
 *
 * 契约：
 * - POST   /reports                    提交举报（登录）
 * - GET    /reports?status=&cursor=    举报队列（仅运营）
 * - PATCH  /reports/{id}               处理举报（仅运营）
 * - POST   /users/{handle}/block       屏蔽（登录）
 * - DELETE /users/{handle}/block       取消屏蔽（登录）
 * - GET    /users/{handle}/block       查询是否已屏蔽（登录）
 * - GET    /blocks?cursor=             我的屏蔽列表（登录）
 *
 * 所有请求走 {@link apiFetch}，自动携带 HttpOnly 会话 Cookie 与 CSRF 双提交令牌。
 * 错误统一抛 {@link ApiError}，页面直接展示 message。
 */

import { ApiError, apiFetch } from "@/lib/http";

/* -------------------------------------------------------------------------- */
/* 类型                                                                        */
/* -------------------------------------------------------------------------- */

export type ReportTargetType = "post" | "comment" | "user" | "document";
export type ReportReason =
  | "spam"
  | "harassment"
  | "misinformation"
  | "copyright"
  | "other";
export type ReportStatus = "pending" | "reviewing" | "resolved" | "rejected";

export type UserRefDTO = {
  id: string;
  name: string;
  handle: string;
};

export type ReportDTO = {
  id: string;
  targetType: ReportTargetType;
  targetId: string;
  reason: ReportReason;
  detail: string;
  status: ReportStatus;
  reporter: UserRefDTO | null;
  handledBy: UserRefDTO | null;
  handledAt: string | null;
  createdAt: string;
};

export type ReportPage = {
  reports: ReportDTO[];
  nextCursor: string | null;
};

export type BlockedUserDTO = {
  id: string;
  name: string;
  handle: string;
  verifyType: string;
  blockedAt: string;
};

export type BlockPage = {
  blocked: BlockedUserDTO[];
  nextCursor: string | null;
};

/** 举报目标类型的中文标签。 */
export const TARGET_TYPE_LABEL: Record<ReportTargetType, string> = {
  post: "帖子",
  comment: "评论",
  user: "用户",
  document: "文档",
};

/** 举报原因的中文标签，顺序即下拉框顺序。 */
export const REASON_LABEL: Record<ReportReason, string> = {
  spam: "垃圾广告",
  harassment: "骚扰与攻击",
  misinformation: "虚假信息",
  copyright: "侵犯版权",
  other: "其他",
};

/** 审核状态的中文标签与展示色。 */
export const STATUS_LABEL: Record<ReportStatus, string> = {
  pending: "待处理",
  reviewing: "处理中",
  resolved: "已处理",
  rejected: "已驳回",
};

export const REPORT_REASONS: ReportReason[] = [
  "spam",
  "harassment",
  "misinformation",
  "copyright",
  "other",
];

/** 与后端 detail varchar(500) 对齐。 */
export const REPORT_DETAIL_MAX = 500;

/* -------------------------------------------------------------------------- */
/* 解析                                                                        */
/* -------------------------------------------------------------------------- */

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function str(value: unknown): string {
  return typeof value === "string" ? value : "";
}

function normalizeTargetType(value: unknown): ReportTargetType {
  return value === "post" || value === "comment" || value === "user" || value === "document"
    ? value
    : "post";
}

function normalizeReason(value: unknown): ReportReason {
  return value === "spam" ||
    value === "harassment" ||
    value === "misinformation" ||
    value === "copyright" ||
    value === "other"
    ? value
    : "other";
}

function normalizeStatus(value: unknown): ReportStatus {
  return value === "pending" ||
    value === "reviewing" ||
    value === "resolved" ||
    value === "rejected"
    ? value
    : "pending";
}

function normalizeUserRef(value: unknown): UserRefDTO | null {
  if (!isRecord(value)) return null;
  const id = str(value.id);
  if (!id) return null;
  return { id, name: str(value.name) || "未知用户", handle: str(value.handle) };
}

function invalidResponse(): ApiError {
  return new ApiError("invalid_response", "服务器返回了无法识别的响应。", 0);
}

function normalizeReport(value: unknown): ReportDTO {
  if (!isRecord(value)) throw invalidResponse();
  const id = str(value.id);
  if (!id) throw invalidResponse();
  return {
    id,
    targetType: normalizeTargetType(value.targetType),
    targetId: str(value.targetId),
    reason: normalizeReason(value.reason),
    detail: str(value.detail),
    status: normalizeStatus(value.status),
    reporter: normalizeUserRef(value.reporter),
    handledBy: normalizeUserRef(value.handledBy),
    handledAt: typeof value.handledAt === "string" && value.handledAt ? value.handledAt : null,
    createdAt: str(value.createdAt),
  };
}

function normalizeBlocked(value: unknown): BlockedUserDTO {
  if (!isRecord(value)) throw invalidResponse();
  const id = str(value.id);
  if (!id) throw invalidResponse();
  return {
    id,
    name: str(value.name) || str(value.handle) || "未知用户",
    handle: str(value.handle),
    verifyType: str(value.verifyType),
    blockedAt: str(value.blockedAt),
  };
}

function nextCursorOf(body: Record<string, unknown>): string | null {
  return typeof body.nextCursor === "string" && body.nextCursor ? body.nextCursor : null;
}

function clampLimit(limit: number): number {
  return Math.min(Math.max(limit, 1), 50);
}

/* -------------------------------------------------------------------------- */
/* 举报                                                                        */
/* -------------------------------------------------------------------------- */

/** 提交举报。重复举报返回 409 already_reported，自举报返回 422 self_report。 */
export async function createReport(input: {
  targetType: ReportTargetType;
  targetId: string;
  reason: ReportReason;
  detail?: string;
}): Promise<ReportDTO> {
  const payload: Record<string, unknown> = {
    targetType: input.targetType,
    targetId: input.targetId,
    reason: input.reason,
  };
  const detail = input.detail?.trim();
  if (detail) payload.detail = detail;

  const body = await apiFetch("/reports", { method: "POST", body: payload });
  if (!isRecord(body) || !("report" in body)) throw invalidResponse();
  return normalizeReport(body.report);
}

/** 举报队列（仅运营）。status 为空表示全部。 */
export async function listReports(
  status: ReportStatus | "" = "pending",
  cursor: string | null = null,
  limit = 20,
): Promise<ReportPage> {
  const params = new URLSearchParams({ limit: String(clampLimit(limit)) });
  if (status) params.set("status", status);
  if (cursor) params.set("cursor", cursor);

  const body = await apiFetch(`/reports?${params.toString()}`);
  if (!isRecord(body) || !Array.isArray(body.reports)) throw invalidResponse();
  return {
    reports: body.reports.map(normalizeReport),
    nextCursor: nextCursorOf(body),
  };
}

/** 处理举报（仅运营）。status 只能是 reviewing / resolved / rejected。 */
export async function updateReportStatus(
  id: string,
  status: Exclude<ReportStatus, "pending">,
): Promise<ReportDTO> {
  const body = await apiFetch(`/reports/${encodeURIComponent(id)}`, {
    method: "PATCH",
    body: { status },
  });
  if (!isRecord(body) || !("report" in body)) throw invalidResponse();
  return normalizeReport(body.report);
}

/* -------------------------------------------------------------------------- */
/* 屏蔽                                                                        */
/* -------------------------------------------------------------------------- */

/** 屏蔽用户。重复屏蔽返回 409 already_blocked。 */
export async function blockUser(handle: string): Promise<void> {
  await apiFetch(`/users/${encodeURIComponent(handle)}/block`, { method: "POST" });
}

/** 取消屏蔽。未屏蔽时返回 404 not_blocked。 */
export async function unblockUser(handle: string): Promise<void> {
  await apiFetch(`/users/${encodeURIComponent(handle)}/block`, { method: "DELETE" });
}

/** 查询是否已屏蔽某个用户（登录）。 */
export async function getBlockStatus(handle: string): Promise<boolean> {
  const body = await apiFetch(`/users/${encodeURIComponent(handle)}/block`);
  return isRecord(body) && body.blocked === true;
}

/** 我的屏蔽列表，游标翻页。 */
export async function listBlocks(
  cursor: string | null = null,
  limit = 20,
): Promise<BlockPage> {
  const params = new URLSearchParams({ limit: String(clampLimit(limit)) });
  if (cursor) params.set("cursor", cursor);

  const body = await apiFetch(`/blocks?${params.toString()}`);
  if (!isRecord(body) || !Array.isArray(body.blocked)) throw invalidResponse();
  return {
    blocked: body.blocked.map(normalizeBlocked),
    nextCursor: nextCursorOf(body),
  };
}
