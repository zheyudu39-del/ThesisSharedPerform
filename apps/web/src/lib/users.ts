/**
 * 用户资料与关注关系 API 客户端（M1）。
 *
 * 与后端契约对齐（见任务约定的 API 契约），请求走同源 `/api/v1/*`：
 * - `GET /users/{handle}`：公开资料，无需登录；带上令牌时服务端可据此返回
 *   当前用户是否已关注（`following`，可选字段，缺省按未关注处理）；
 * - 关注 / 取关：必须带令牌，服务端保证幂等，计数以响应为准；
 * - `PATCH /users/me`：局部更新本人资料，只发送请求体中出现的字段。
 *
 * 错误统一抛 {@link ApiError}（code / message / status），与 lib/posts.ts 一致。
 */

import { type UserDTO } from "@/lib/auth";
import { ApiError, apiFetch } from "@/lib/http";

/** 公开资料：绝不包含 email / role / status 等私密字段。 */
export type PublicUserDTO = {
  id: string;
  name: string;
  handle: string;
  bio: string;
  institution: string;
  researchFields: string[];
  verifyType: string;
  createdAt: string;
};

export type PublicUserStats = {
  postCount: number;
  followerCount: number;
  followingCount: number;
};

export type PublicProfile = {
  user: PublicUserDTO;
  stats: PublicUserStats;
  /** 当前登录用户是否已关注此人；未登录或服务端未返回时为 false。 */
  following: boolean;
  /**
   * 服务端是否明确给出了关注状态。
   * GET /users/{handle} 在带上有效令牌时返回 `following`；匿名访问不返回，
   * 此时调用方可回退到 {@link recallFollowState} 的本地提示。
   */
  followingKnown: boolean;
};

export type FollowResult = {
  following: boolean;
  followerCount: number;
};

export type ProfileUpdateInput = {
  name?: string;
  bio?: string;
  institution?: string;
  researchFields?: string[];
};

/** 与后端校验规则保持一致，供设置页做即时反馈。 */
export const PROFILE_LIMITS = {
  nameMax: 64,
  bioMax: 280,
  institutionMax: 128,
  researchFieldsMax: 10,
  researchFieldMax: 64,
} as const;

/* -------------------------------------------------------------------------- */
/* 请求与解析                                                                  */
/* -------------------------------------------------------------------------- */

/** required：无令牌直接失败；optional：有令牌就带上。 */

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

function fallbackMessage(status: number): string {
  switch (status) {
    case 401:
      return "登录状态已失效，请重新登录。";
    case 403:
      return "没有权限执行此操作。";
    case 404:
      return "用户不存在。";
    case 422:
      return "提交的信息未通过校验。";
    case 429:
      return "操作过于频繁，请稍后再试。";
    default:
      return status >= 500 ? "服务器暂时不可用，请稍后重试。" : "请求失败，请稍后重试。";
  }
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

function invalidResponse(): ApiError {
  return new ApiError("invalid_response", "服务器返回了无法识别的响应。", 0);
}

function normalizePublicUser(value: unknown): PublicUserDTO | null {
  if (!isRecord(value)) return null;
  const id = str(value.id);
  const handle = str(value.handle);
  if (!id || !handle) return null;
  return {
    id,
    name: str(value.name) || handle,
    handle,
    bio: str(value.bio),
    institution: str(value.institution),
    researchFields: strList(value.researchFields),
    verifyType: str(value.verifyType),
    createdAt: str(value.createdAt),
  };
}

/* -------------------------------------------------------------------------- */
/* 公开资料                                                                    */
/* -------------------------------------------------------------------------- */

/**
 * 读取任意用户的公开资料与统计。
 *
 * 无需登录；若本地有令牌则一并发送，服务端会据此在响应中附带 `following`。
 * 404 表示用户不存在（或已注销）。
 */
export async function getPublicUser(handle: string): Promise<PublicProfile> {
  // 令牌可选：公开接口本身不要求登录；带有效令牌时服务端返回 `following`，
  // 匿名（或令牌已过期）时不返回该字段，按未关注处理。
  const body = await apiFetch(`/users/${encodeURIComponent(handle)}`, { method: "GET" });
  if (!isRecord(body)) throw invalidResponse();

  const user = normalizePublicUser(body.user);
  if (!user) throw invalidResponse();

  const stats = isRecord(body.stats) ? body.stats : {};
  const followingKnown =
    typeof body.following === "boolean" || typeof stats.following === "boolean";
  return {
    user,
    stats: {
      postCount: num(stats.postCount),
      followerCount: num(stats.followerCount),
      followingCount: num(stats.followingCount),
    },
    following: body.following === true || stats.following === true,
    followingKnown,
  };
}

/* -------------------------------------------------------------------------- */
/* 关注状态的会话内提示                                                        */
/* -------------------------------------------------------------------------- */

/**
 * 公开资料接口只在带有效令牌时返回 following。为了让匿名访问后登录、
 * 或接口暂时不可用时也不把「已关注」错显示成「关注」，关注成功后把状态
 * 记在本会话里作为初始值；服务端返回 following 时一律以服务端为准。
 */
const FOLLOW_CACHE_PREFIX = "thesis_follow_";

export function rememberFollowState(handle: string, following: boolean): void {
  if (typeof window === "undefined") return;
  try {
    window.sessionStorage.setItem(`${FOLLOW_CACHE_PREFIX}${handle}`, following ? "1" : "0");
  } catch {
    // 隐私模式 / 存储已满：仅影响下次打开的初始状态。
  }
}

/** 读取会话内记住的关注状态；没有记录时返回 null。 */
export function recallFollowState(handle: string): boolean | null {
  if (typeof window === "undefined") return null;
  try {
    const raw = window.sessionStorage.getItem(`${FOLLOW_CACHE_PREFIX}${handle}`);
    if (raw === "1") return true;
    if (raw === "0") return false;
    return null;
  } catch {
    return null;
  }
}

/* -------------------------------------------------------------------------- */
/* 关注 / 取关                                                                 */
/* -------------------------------------------------------------------------- */

async function setFollow(handle: string, following: boolean): Promise<FollowResult> {
  const body = await apiFetch(`/users/${encodeURIComponent(handle)}/follow`, { method: following ? "POST" : "DELETE" });
  if (!isRecord(body)) throw invalidResponse();
  return { following: body.following === true, followerCount: num(body.followerCount) };
}

/** 关注用户；重复关注幂等，返回服务端计数。 */
export async function followUser(handle: string): Promise<FollowResult> {
  return setFollow(handle, true);
}

/** 取消关注；重复取消幂等，返回服务端计数。 */
export async function unfollowUser(handle: string): Promise<FollowResult> {
  return setFollow(handle, false);
}

/* -------------------------------------------------------------------------- */
/* 本人资料                                                                    */
/* -------------------------------------------------------------------------- */

/**
 * 局部更新当前登录用户的资料。
 *
 * 只发送 payload 中实际出现的字段（undefined 会被 JSON.stringify 省略），
 * 服务端按请求体中出现的字段更新。成功后返回完整私有 DTO。
 */
export async function updateProfile(input: ProfileUpdateInput): Promise<UserDTO> {
  const body = await apiFetch("/users/me", { method: "PATCH", body: JSON.stringify(input) });
  if (!isRecord(body) || !isRecord(body.user)) throw invalidResponse();
  const user = body.user;
  if (typeof user.id !== "string" || typeof user.name !== "string") throw invalidResponse();
  return user as unknown as UserDTO;
}
