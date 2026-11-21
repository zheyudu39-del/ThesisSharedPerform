/**
 * 通知中心 API 客户端。
 *
 * 契约（见后端 internal/handler/notify.go）：
 * - GET   /notifications              列表，cursor + limit（1–50，默认 20）
 * - GET   /notifications/unread-count 未读数
 * - GET   /notification-prefs         通知偏好
 * - POST  /notifications/{id}/read    单条已读（204）
 * - POST  /notifications/read-all     全部已读（204）
 * - PATCH /notification-prefs         更新偏好
 *
 * 所有接口都要求登录；未登录时后端返回 401，由调用方（顶栏铃铛）静默忽略。
 * 写请求的 CSRF 令牌由 {@link apiFetch} 自动回传。
 */

import { ApiError, apiFetch } from "@/lib/http";

export type NotificationType =
  | "comment"
  | "reply"
  | "mention"
  | "repost"
  | "like_agg"
  | "join_request"
  | "doc_edit"
  | "system";

export type NotificationActor = {
  id: string;
  name: string;
  handle: string;
  avatarUrl: string | null;
};

export type NotificationDTO = {
  id: string;
  type: NotificationType;
  actor: NotificationActor | null;
  targetType: string | null;
  targetId: string | null;
  payload: Record<string, unknown>;
  read: boolean;
  readAt: string | null;
  createdAt: string;
};

export type NotificationPage = {
  notifications: NotificationDTO[];
  nextCursor: string | null;
};

export type NotificationPrefs = {
  mutedTypes: string[];
  emailDigest: string;
};

const NOTIFICATION_TYPES: NotificationType[] = [
  "comment",
  "reply",
  "mention",
  "repost",
  "like_agg",
  "join_request",
  "doc_edit",
  "system",
];

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function str(value: unknown): string {
  return typeof value === "string" ? value : "";
}

function num(value: unknown): number {
  return typeof value === "number" && Number.isFinite(value) ? value : 0;
}

function normalizeType(value: unknown): NotificationType {
  return NOTIFICATION_TYPES.includes(value as NotificationType)
    ? (value as NotificationType)
    : "system";
}

function normalizeActor(value: unknown): NotificationActor | null {
  if (!isRecord(value) || typeof value.id !== "string") return null;
  return {
    id: value.id,
    name: str(value.name) || "未知用户",
    handle: str(value.handle),
    avatarUrl: typeof value.avatarUrl === "string" && value.avatarUrl ? value.avatarUrl : null,
  };
}

function normalizeNotification(value: unknown): NotificationDTO | null {
  if (!isRecord(value) || typeof value.id !== "string") return null;
  return {
    id: value.id,
    type: normalizeType(value.type),
    actor: normalizeActor(value.actor),
    targetType: typeof value.targetType === "string" ? value.targetType : null,
    targetId: typeof value.targetId === "string" ? value.targetId : null,
    payload: isRecord(value.payload) ? value.payload : {},
    read: value.read === true,
    readAt: typeof value.readAt === "string" ? value.readAt : null,
    createdAt: str(value.createdAt),
  };
}

/* -------------------------------------------------------------------------- */
/* 请求                                                                        */
/* -------------------------------------------------------------------------- */

/** 拉取一页通知；limit 由调用方收敛，服务端还会再截断到 50。 */
export async function listNotifications(
  cursor: string | null = null,
  limit = 20,
): Promise<NotificationPage> {
  const params = new URLSearchParams({
    limit: String(Math.min(Math.max(limit, 1), 50)),
  });
  if (cursor) params.set("cursor", cursor);

  const body = await apiFetch<unknown>(`/notifications?${params.toString()}`);
  if (!isRecord(body) || !Array.isArray(body.notifications)) {
    throw new ApiError("invalid_response", "服务器返回了无法识别的响应。", 0);
  }
  return {
    notifications: body.notifications
      .map(normalizeNotification)
      .filter((item): item is NotificationDTO => item !== null),
    nextCursor:
      typeof body.nextCursor === "string" && body.nextCursor ? body.nextCursor : null,
  };
}

/** 未读数；用于顶栏铃铛角标。 */
export async function getUnreadCount(): Promise<number> {
  const body = await apiFetch<unknown>("/notifications/unread-count");
  if (!isRecord(body)) return 0;
  return Math.max(0, Math.trunc(num(body.unreadCount)));
}

/** 标记单条已读；通知不存在或不属于本人时后端返回 404。 */
export async function markNotificationRead(id: string): Promise<void> {
  await apiFetch(`/notifications/${encodeURIComponent(id)}/read`, { method: "POST" });
}

/** 标记全部已读。 */
export async function markAllNotificationsRead(): Promise<void> {
  await apiFetch("/notifications/read-all", { method: "POST" });
}

function normalizePrefs(value: unknown): NotificationPrefs {
  if (!isRecord(value)) return { mutedTypes: [], emailDigest: "daily" };
  const muted = Array.isArray(value.mutedTypes)
    ? value.mutedTypes.filter((t): t is string => typeof t === "string")
    : [];
  const digest = str(value.emailDigest);
  return {
    mutedTypes: muted,
    emailDigest: digest === "daily" || digest === "weekly" || digest === "never" ? digest : "daily",
  };
}

export async function getNotificationPrefs(): Promise<NotificationPrefs> {
  const body = await apiFetch<unknown>("/notification-prefs");
  if (!isRecord(body)) return normalizePrefs(null);
  return normalizePrefs(body.prefs);
}

export async function updateNotificationPrefs(input: {
  mutedTypes?: string[];
  emailDigest?: string;
}): Promise<NotificationPrefs> {
  const payload: Record<string, unknown> = {};
  if (input.mutedTypes !== undefined) payload.mutedTypes = input.mutedTypes;
  if (input.emailDigest !== undefined) payload.emailDigest = input.emailDigest;

  const body = await apiFetch<unknown>("/notification-prefs", {
    method: "PATCH",
    body: payload,
  });
  if (!isRecord(body)) return normalizePrefs(null);
  return normalizePrefs(body.prefs);
}

/* -------------------------------------------------------------------------- */
/* 展示辅助                                                                    */
/* -------------------------------------------------------------------------- */

/** 通知类型 → 中文标签。 */
export const NOTIFICATION_LABEL: Record<NotificationType, string> = {
  comment: "评论",
  reply: "回复",
  mention: "提及",
  repost: "转发",
  like_agg: "点赞",
  join_request: "入群申请",
  doc_edit: "协作文档",
  system: "系统",
};

/** payload.count 的安全读取（JSON 数字解码后是 number）。 */
export function notificationCount(item: NotificationDTO): number {
  return Math.max(0, Math.trunc(num(item.payload.count)));
}

/**
 * 通知的跳转地址。
 *
 * 各 targetType 对应的站内路由：
 * - post      → /posts/{id}（点赞、评论、转发、提及）
 * - comment   → /posts/{payload.postId}（回复通知：通知里的 ID 是帖子，
 *               评论本身没有独立路由，跳到帖子详情即可看到回复）
 * - group     → /groups/{payload.slug}（入群申请）。群组路由按 slug 而不是 ID，
 *               因此优先用 payload 里的 slug；缺失时退回 ID 拼出的地址，
 *               由群组页自行处理「找不到」的情况。
 * - document  → /documents/{id}（文档被编辑）
 *
 * 其余类型（system 等）没有确定的落地页，返回 null，由调用方渲染为不可点击项。
 */
export function notificationHref(item: NotificationDTO): string | null {
  const targetId = item.targetId;

  switch (item.targetType) {
    case "post":
      return targetId ? `/posts/${encodeURIComponent(targetId)}` : null;
    case "comment": {
      // 回复通知把帖子 ID 放在 payload.postId；targetId 只是兜底。
      const postId = str(item.payload.postId) || targetId;
      return postId ? `/posts/${encodeURIComponent(postId)}` : null;
    }
    case "group": {
      const slug = str(item.payload.slug);
      if (slug) return `/groups/${encodeURIComponent(slug)}`;
      return targetId ? `/groups/${encodeURIComponent(targetId)}` : null;
    }
    case "document":
      return targetId ? `/documents/${encodeURIComponent(targetId)}` : null;
    default:
      return null;
  }
}

/** 一句话描述，供列表直接渲染。 */
export function notificationText(item: NotificationDTO): string {
  const actor = item.actor?.name ?? "有人";
  switch (item.type) {
    case "like_agg": {
      const count = notificationCount(item);
      return count > 1 ? `${actor} 等 ${count} 人赞了你的帖子` : `${actor} 赞了你的帖子`;
    }
    case "comment":
      return `${actor} 评论了你的帖子`;
    case "reply":
      return `${actor} 回复了你的评论`;
    case "mention":
      return `${actor} 在内容中提到了你`;
    case "repost":
      return `${actor} 转发了你的帖子`;
    case "join_request":
      return `${actor} 申请加入你的群组`;
    case "doc_edit":
      return `${actor} 编辑了你参与的文档`;
    case "system":
    default:
      return "系统通知";
  }
}
