/**
 * 开放 API 密钥管理客户端（M4）。
 *
 * 认证走普通会话（Cookie + CSRF），与 /openapi/v1 的 X-API-Key 是两套机制：
 * 这里是「管理自己的钥匙」，那边是「用钥匙访问公开数据」。
 *
 * 明文密钥只在创建响应里出现一次，服务端只存 SHA-256 哈希。
 * 前端必须把这一点讲清楚，否则用户关掉弹窗就再也拿不回来。
 */

import { ApiError, apiFetch } from "@/lib/http";

export type ApiScope = "read:papers" | "read:posts" | "read:users";

export const SCOPE_LABEL: Record<ApiScope, string> = {
  "read:papers": "论文",
  "read:posts": "帖子",
  "read:users": "用户主页",
};

export type ApiKeyDTO = {
  id: string;
  name: string;
  prefix: string;
  scopes: ApiScope[];
  lastUsedAt: string | null;
  expiresAt: string | null;
  createdAt: string;
};

export type CreatedApiKey = {
  key: ApiKeyDTO;
  /** 明文，只在此响应里出现一次。 */
  plaintext: string;
};

function normalizeScope(value: unknown): ApiScope | null {
  return value === "read:papers" || value === "read:posts" || value === "read:users"
    ? value
    : null;
}

function normalizeKey(raw: unknown): ApiKeyDTO {
  const r = (raw ?? {}) as Record<string, unknown>;
  const scopes = Array.isArray(r.scopes)
    ? r.scopes.map(normalizeScope).filter((s): s is ApiScope => s !== null)
    : [];
  return {
    id: typeof r.id === "string" ? r.id : "",
    name: typeof r.name === "string" ? r.name : "未命名密钥",
    prefix: typeof r.prefix === "string" ? r.prefix : "",
    scopes,
    lastUsedAt: typeof r.lastUsedAt === "string" ? r.lastUsedAt : null,
    expiresAt: typeof r.expiresAt === "string" ? r.expiresAt : null,
    createdAt: typeof r.createdAt === "string" ? r.createdAt : "",
  };
}

/** 列出当前用户的密钥。 */
export async function listApiKeys(): Promise<ApiKeyDTO[]> {
  const body = (await apiFetch("/api-keys")) as Record<string, unknown>;
  const keys = Array.isArray(body.keys) ? body.keys : [];
  return keys.map(normalizeKey);
}

/** 创建密钥，返回明文（仅此一次）。 */
export async function createApiKey(input: {
  name: string;
  scopes: ApiScope[];
}): Promise<CreatedApiKey> {
  const body = (await apiFetch("/api-keys", {
    method: "POST",
    body: input,
  })) as Record<string, unknown>;
  return {
    key: normalizeKey(body.key),
    plaintext: typeof body.plaintext === "string" ? body.plaintext : "",
  };
}

/** 撤销密钥，立即生效。 */
export async function revokeApiKey(id: string): Promise<void> {
  await apiFetch(`/api-keys/${encodeURIComponent(id)}`, { method: "DELETE" });
}

export { ApiError };
