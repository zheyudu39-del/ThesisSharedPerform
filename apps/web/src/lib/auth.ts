/**
 * 浏览器端认证 API 客户端。
 *
 * 认证方式：登录成功后，服务端下发 HttpOnly 的会话 Cookie。
 * 前端脚本**读不到**令牌，因此不存在「被 XSS 窃取 localStorage」的问题；
 * 所有请求由 lib/http.ts 统一带上 `credentials: "include"`。
 *
 * CSRF：会话 Cookie 会被浏览器自动携带，写请求必须回传双提交令牌，
 * 由 lib/http.ts 自动从 `thesis_csrf` Cookie 取出并放进 `X-CSRF-Token` 头。
 */

import { ApiError, apiFetch } from "@/lib/http";

/** 与 Go 服务对齐的用户数据形状（UserDTO）。 */
export type UserDTO = {
  id: string;
  email: string;
  name: string;
  handle: string;
  bio: string;
  institution: string;
  researchFields: string[];
  verifyType: string;
  role: string;
  createdAt: string;
};

/** 注册 / 登录成功的返回体（不再包含令牌）。 */
export type AuthSession = {
  user: UserDTO;
};

export type RegisterInput = {
  email: string;
  password: string;
  name: string;
  handle: string;
};

export type AuthFieldName = "email" | "password" | "name" | "handle";
export type AuthFieldErrors = Partial<Record<AuthFieldName, string>>;

/** 兼容旧命名：后端统一错误体的客户端表示。 */
export const AuthError = ApiError;
export type AuthError = ApiError;

/* -------------------------------------------------------------------------- */
/* 对外 API                                                                    */
/* -------------------------------------------------------------------------- */

/** 注册账号；成功后服务端已下发会话 Cookie，返回用户资料。 */
export async function register(
  email: string,
  password: string,
  name: string,
  handle: string,
): Promise<AuthSession> {
  const body = await apiFetch<{ user: UserDTO }>("/auth/register", {
    method: "POST",
    body: { email: email.trim(), password, name: name.trim(), handle: handle.trim() },
  });
  return { user: body.user };
}

/** 邮箱 + 密码登录；成功后服务端已下发会话 Cookie。 */
export async function login(email: string, password: string): Promise<AuthSession> {
  const body = await apiFetch<{ user: UserDTO }>("/auth/login", {
    method: "POST",
    body: { email: email.trim(), password },
  });
  return { user: body.user };
}

/**
 * 读取当前登录用户。
 * - 未登录（401）：返回 null。
 * - 其他错误：抛出 ApiError，交由调用方决定是否提示。
 */
export async function me(): Promise<UserDTO | null> {
  try {
    const body = await apiFetch<{ user: UserDTO }>("/users/me");
    return body.user;
  } catch (error) {
    if (error instanceof ApiError && error.status === 401) return null;
    throw error;
  }
}

/** 登出：服务端清除会话与 CSRF Cookie。失败也不阻塞前端状态复位。 */
export async function logout(): Promise<void> {
  try {
    await apiFetch("/auth/logout", { method: "POST" });
  } catch {
    // 会话可能已过期；本地状态照常清空即可。
  }
}

/* -------------------------------------------------------------------------- */
/* 客户端校验（与 API 契约保持一致）                                           */
/* -------------------------------------------------------------------------- */

const EMAIL_PATTERN = /^[^\s@]+@[^\s@]+\.[^\s@]+$/;
const HANDLE_PATTERN = /^[a-zA-Z0-9_-]+$/;

/**
 * 按 Unicode 码点计数，与服务端 Go 的 utf8.RuneCountInString 对齐。
 * 直接用 `s.length` 会把代理对（如 emoji）算成两个字符，导致前后端长度判断不一致。
 */
function runeLength(value: string): number {
  return [...value].length;
}

/** 仅用于即时反馈；服务端仍是最终裁决者。 */
export function validateLogin(email: string, password: string): AuthFieldErrors {
  const errors: AuthFieldErrors = {};
  const value = email.trim();

  if (!value) {
    errors.email = "请输入邮箱地址。";
  } else if (!EMAIL_PATTERN.test(value)) {
    errors.email = "邮箱格式不正确，例如 name@example.com。";
  }

  if (!password) {
    errors.password = "请输入密码。";
  }

  return errors;
}

/** 注册表单校验：email 合法、password ≥ 8、name 1..64、handle 3..32 且匹配句柄字符集。 */
export function validateRegister(input: RegisterInput): AuthFieldErrors {
  const errors: AuthFieldErrors = {};
  const email = input.email.trim();
  const name = input.name.trim();
  const handle = input.handle.trim();

  if (!email) {
    errors.email = "请输入邮箱地址。";
  } else if (!EMAIL_PATTERN.test(email)) {
    errors.email = "邮箱格式不正确，例如 name@example.com。";
  } else if (email.length > 254) {
    errors.email = "邮箱地址过长。";
  }

  if (!input.password) {
    errors.password = "请设置密码。";
  } else if (input.password.length < 8) {
    errors.password = "密码至少 8 位字符。";
  }

  const nameLength = runeLength(name);
  if (!name) {
    errors.name = "请输入显示名。";
  } else if (nameLength > 64) {
    errors.name = "显示名最多 64 个字符。";
  }

  if (!handle) {
    errors.handle = "请输入句柄。";
  } else if (handle.length < 3) {
    errors.handle = "句柄至少 3 个字符。";
  } else if (handle.length > 32) {
    errors.handle = "句柄最多 32 个字符。";
  } else if (!HANDLE_PATTERN.test(handle)) {
    errors.handle = "句柄只能包含字母、数字、下划线和连字符。";
  }

  return errors;
}
