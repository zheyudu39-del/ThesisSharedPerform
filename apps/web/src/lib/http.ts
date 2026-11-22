/**
 * 统一的浏览器端 HTTP 客户端。
 *
 * 认证方式：会话保存在 HttpOnly Cookie 中，前端脚本读不到令牌，
 * 因此所有请求必须带 `credentials: "include"` 让浏览器自动发送。
 *
 * CSRF 防护：写请求（非 GET/HEAD）必须回传双提交令牌 —— 从可读的
 * `thesis_csrf` Cookie 取值放进 `X-CSRF-Token` 头。攻击者站点能触发
 * 浏览器自动带 Cookie，但读不到 CSRF Cookie（同源策略），因此无法伪造请求头。
 */

export const CSRF_COOKIE = "thesis_csrf";
export const CSRF_HEADER = "X-CSRF-Token";

/** 后端统一错误体。 */
export class ApiError extends Error {
  readonly code: string;
  readonly status: number;

  constructor(code: string, message: string, status: number) {
    super(message);
    this.name = "ApiError";
    this.code = code;
    this.status = status;
  }
}

/** 读取 CSRF Cookie 的值；不可用时返回 null。 */
export function readCSRFCookie(): string | null {
  if (typeof document === "undefined") return null;
  const target = `${CSRF_COOKIE}=`;
  for (const part of document.cookie.split(";")) {
    const entry = part.trim();
    if (entry.startsWith(target)) {
      return decodeURIComponent(entry.slice(target.length));
    }
  }
  return null;
}

/** 需要写请求的方法。 */
function isMutating(method: string): boolean {
  const m = method.toUpperCase();
  return m !== "GET" && m !== "HEAD" && m !== "OPTIONS";
}

export type ApiFetchOptions = {
  method?: string;
  /**
   * 请求体。传对象即可，由 apiFetch 统一序列化——**不要**自己 JSON.stringify，
   * 否则会双重编码（历史上 19 处调用点都这么写，导致上传论文 422）。
   * 需要发送二进制/表单时用 rawBody。
   */
  body?: Record<string, unknown>;
  /** 已构造好的请求体（如 FormData），与 body 互斥。 */
  rawBody?: BodyInit;
  headers?: HeadersInit;
  signal?: AbortSignal;
  /** 默认 20 秒；上传场景可自行放宽。 */
  timeoutMs?: number;
};

function fallbackMessage(status: number): string {
  switch (status) {
    case 401:
      return "登录状态无效，请重新登录。";
    case 403:
      return "没有权限执行该操作。";
    case 404:
      return "请求的资源不存在。";
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

function extractError(body: unknown, status: number): { code: string; message: string } {
  if (typeof body === "object" && body !== null && !Array.isArray(body)) {
    const err = (body as Record<string, unknown>).error;
    if (typeof err === "object" && err !== null) {
      const e = err as Record<string, unknown>;
      return {
        code: typeof e.code === "string" && e.code ? e.code : "request_failed",
        message:
          typeof e.message === "string" && e.message.trim()
            ? e.message
            : fallbackMessage(status),
      };
    }
  }
  return { code: "request_failed", message: fallbackMessage(status) };
}

/**
 * 发起一次 API 请求，返回解析后的 JSON（无响应体时返回 null）。
 * 失败统一抛 ApiError。
 */
export async function apiFetch<T = unknown>(
  path: string,
  options: ApiFetchOptions = {},
): Promise<T> {
  const method = options.method ?? "GET";
  const headers = new Headers(options.headers);
  headers.set("Accept", "application/json");

  if (isMutating(method)) {
    const csrf = readCSRFCookie();
    if (csrf) headers.set(CSRF_HEADER, csrf);
  }

  let body: BodyInit | undefined = options.rawBody;
  if (body === undefined && options.body !== undefined) {
    headers.set("Content-Type", "application/json");
    body = JSON.stringify(options.body);
  }

  const controller = new AbortController();
  const timeout = window.setTimeout(
    () => controller.abort(),
    options.timeoutMs ?? 20000,
  );
  // 调用方传入的 signal 也要生效。
  if (options.signal) {
    if (options.signal.aborted) controller.abort();
    else options.signal.addEventListener("abort", () => controller.abort(), { once: true });
  }

  let response: Response;
  try {
    response = await fetch(`/api/v1${path}`, {
      method,
      headers,
      body,
      signal: controller.signal,
      // 关键：带上 HttpOnly 会话 Cookie。
      credentials: "include",
      cache: "no-store",
    });
  } catch {
    throw new ApiError("network_error", "无法连接服务器，请检查网络后重试。", 0);
  } finally {
    window.clearTimeout(timeout);
  }

  const text = await response.text();
  let parsed: unknown = null;
  if (text) {
    try {
      parsed = JSON.parse(text) as unknown;
    } catch {
      parsed = null;
    }
  }

  if (!response.ok) {
    const { code, message } = extractError(parsed, response.status);
    throw new ApiError(code, message, response.status);
  }

  return parsed as T;
}

/**
 * 发起一次返回二进制的请求（如文件导出），成功时返回原始 Response。
 *
 * 与 {@link apiFetch} 的差别：
 * - 不解析 JSON，调用方自行处理 blob；
 * - 失败时仍按统一错误体解析，抛出 ApiError，让页面能展示服务端给出的原因
 *   （例如「无权限导出」），而不是一句笼统的「下载失败」。
 */
export async function apiFetchRaw(
  path: string,
  options: ApiFetchOptions = {},
): Promise<Response> {
  const method = options.method ?? "GET";
  const headers = new Headers(options.headers);
  headers.set("Accept", "*/*");

  if (isMutating(method)) {
    const csrf = readCSRFCookie();
    if (csrf) headers.set(CSRF_HEADER, csrf);
  }

  let body: BodyInit | undefined = options.rawBody;
  if (body === undefined && options.body !== undefined) {
    headers.set("Content-Type", "application/json");
    body = JSON.stringify(options.body);
  }

  const controller = new AbortController();
  const timeout = window.setTimeout(
    () => controller.abort(),
    // 导出需要遍历整篇文档并打包 zip，给比普通请求更宽的窗口。
    options.timeoutMs ?? 60000,
  );

  let response: Response;
  try {
    response = await fetch(`/api/v1${path}`, {
      method,
      headers,
      body,
      signal: controller.signal,
      credentials: "include",
      cache: "no-store",
    });
  } catch {
    throw new ApiError("network_error", "无法连接服务器，请检查网络后重试。", 0);
  } finally {
    window.clearTimeout(timeout);
  }

  if (!response.ok) {
    const text = await response.text();
    let parsed: unknown = null;
    if (text) {
      try {
        parsed = JSON.parse(text) as unknown;
      } catch {
        parsed = null;
      }
    }
    const { code, message } = extractError(parsed, response.status);
    throw new ApiError(code, message, response.status);
  }

  return response;
}
