/**
 * 浏览器端认证 API 客户端（M1）。
 *
 * 请求统一走同源 `/api/v1/*`：浏览器不直接访问 Go 服务，由 next.config.ts 的
 * rewrites 反向代理到 API_INTERNAL_URL，避免跨域与 CORS 预检。
 *
 * ⚠️ 令牌存储（M1 简化，后续必须迁移）
 * 当前 JWT 保存在 localStorage，键名 `thesis_token`。这能让纯客户端页面快速拿到
 * 登录态，但 localStorage 对同源脚本完全可读——任何 XSS 都能窃取令牌，也无法被
 * httpOnly 保护。后续应改为由 Go 服务下发 httpOnly + Secure + SameSite 的会话
 * Cookie，前端不再持有令牌，并同步补上 CSRF 防护（SameSite + 双提交令牌）。
 */

export const TOKEN_STORAGE_KEY = "thesis_token";

/** 与 Go 服务对齐的用户数据形状（UserDTO）。 */
export type UserDTO = {
  id: string;
  email: string;
  name: string;
  handle: string;
  // 服务端对可空列统一输出空字符串（见 identity.textValue），因此这里不收 null。
  bio: string;
  institution: string;
  researchFields: string[];
  verifyType: string;
  role: string;
  createdAt: string;
};

/** 注册 / 登录成功的返回体。 */
export type AuthSession = {
  user: UserDTO;
  token: string;
};

export type RegisterInput = {
  email: string;
  password: string;
  name: string;
  handle: string;
};

export type AuthFieldName = "email" | "password" | "name" | "handle";
export type AuthFieldErrors = Partial<Record<AuthFieldName, string>>;

/** 后端统一错误体 {"error":{"code","message"}} 的客户端表示。 */
export class AuthError extends Error {
  readonly code: string;
  readonly status: number;

  constructor(code: string, message: string, status: number) {
    super(message);
    this.name = "AuthError";
    this.code = code;
    this.status = status;
  }
}

/* -------------------------------------------------------------------------- */
/* 令牌存取                                                                    */
/* -------------------------------------------------------------------------- */

export function getToken(): string | null {
  if (typeof window === "undefined") return null;
  try {
    return window.localStorage.getItem(TOKEN_STORAGE_KEY);
  } catch {
    // 隐私模式 / 禁用站点数据时 localStorage 可能直接抛错。
    return null;
  }
}

export function saveToken(token: string): void {
  if (typeof window === "undefined") return;
  try {
    window.localStorage.setItem(TOKEN_STORAGE_KEY, token);
  } catch {
    // 存不进去不阻断本次会话，只是刷新后需要重新登录。
  }
}

export function clearToken(): void {
  if (typeof window === "undefined") return;
  try {
    window.localStorage.removeItem(TOKEN_STORAGE_KEY);
  } catch {
    // 忽略：清不掉也不会造成更多问题。
  }
}

/* -------------------------------------------------------------------------- */
/* 请求与错误解析                                                              */
/* -------------------------------------------------------------------------- */

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function fallbackMessage(status: number): string {
  switch (status) {
    case 401:
      return "登录状态无效，请重新登录。";
    case 409:
      return "该邮箱或句柄已被占用。";
    case 422:
      return "提交的信息未通过校验。";
    case 429:
      return "请求过于频繁，请稍后再试。";
    default:
      return status >= 500 ? "服务器暂时不可用，请稍后重试。" : "请求失败，请稍后重试。";
  }
}

/** 从错误响应体中取出 code / message，缺失时按状态码回退。 */
function extractError(body: unknown, status: number): { code: string; message: string } {
  if (isRecord(body) && isRecord(body.error)) {
    const code =
      typeof body.error.code === "string" && body.error.code.length > 0
        ? body.error.code
        : "request_failed";
    const message =
      typeof body.error.message === "string" && body.error.message.trim().length > 0
        ? body.error.message
        : fallbackMessage(status);
    return { code, message };
  }
  return { code: "request_failed", message: fallbackMessage(status) };
}

async function parseJSON(response: Response): Promise<unknown> {
  // 代理层异常时可能返回空体或 HTML，这里一律当作「无 JSON」处理。
  const text = await response.text();
  if (!text) return null;
  try {
    return JSON.parse(text) as unknown;
  } catch {
    return null;
  }
}

async function requestJSON(path: string, init: RequestInit): Promise<unknown> {
  const headers = new Headers(init.headers);
  headers.set("Accept", "application/json");
  if (init.body !== undefined) headers.set("Content-Type", "application/json");

  let response: Response;
  try {
    response = await fetch(`/api/v1${path}`, { ...init, headers });
  } catch {
    throw new AuthError("network_error", "无法连接服务器，请检查网络后重试。", 0);
  }

  const body = await parseJSON(response);

  if (!response.ok) {
    const { code, message } = extractError(body, response.status);
    throw new AuthError(code, message, response.status);
  }

  return body;
}

function parseSession(body: unknown): AuthSession {
  if (
    !isRecord(body) ||
    !isRecord(body.user) ||
    typeof body.token !== "string" ||
    body.token.length === 0
  ) {
    throw new AuthError("invalid_response", "服务器返回了无法识别的响应。", 0);
  }
  const user = body.user;
  if (
    typeof user.id !== "string" ||
    typeof user.name !== "string" ||
    typeof user.handle !== "string"
  ) {
    throw new AuthError("invalid_response", "服务器返回的用户数据不完整。", 0);
  }
  return { user: user as unknown as UserDTO, token: body.token };
}

/* -------------------------------------------------------------------------- */
/* 对外 API                                                                    */
/* -------------------------------------------------------------------------- */

/** 注册账号；成功后自动保存令牌，并返回用户与令牌。 */
export async function register(
  email: string,
  password: string,
  name: string,
  handle: string,
): Promise<AuthSession> {
  const body = await requestJSON("/auth/register", {
    method: "POST",
    body: JSON.stringify({
      email: email.trim(),
      password,
      name: name.trim(),
      handle: handle.trim(),
    }),
  });

  const session = parseSession(body);
  saveToken(session.token);
  return session;
}

/** 邮箱 + 密码登录；成功后自动保存令牌。 */
export async function login(email: string, password: string): Promise<AuthSession> {
  const body = await requestJSON("/auth/login", {
    method: "POST",
    body: JSON.stringify({ email: email.trim(), password }),
  });

  const session = parseSession(body);
  saveToken(session.token);
  return session;
}

/**
 * 读取当前登录用户。
 * - 无令牌：返回 null（未登录）。
 * - 令牌失效（401）：清除本地令牌并返回 null。
 * - 其他错误（网络 / 5xx）：抛出 AuthError，交由调用方决定是否提示。
 */
export async function me(): Promise<UserDTO | null> {
  const token = getToken();
  if (!token) return null;

  try {
    const body = await requestJSON("/users/me", {
      method: "GET",
      headers: { Authorization: `Bearer ${token}` },
      cache: "no-store",
    });
    if (!isRecord(body) || !isRecord(body.user)) {
      throw new AuthError("invalid_response", "服务器返回了无法识别的响应。", 0);
    }
    return body.user as unknown as UserDTO;
  } catch (error) {
    if (error instanceof AuthError && error.status === 401) {
      clearToken();
      return null;
    }
    throw error;
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
