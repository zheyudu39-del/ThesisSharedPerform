/**
 * 后端 API 客户端。
 *
 * 两种运行环境走不同地址：
 * - 服务端（SSR/RSC）：必须用绝对地址，直连 Go API；
 * - 浏览器：走同源 /api/*，由 next.config.ts 的 rewrites 转发，避免跨域。
 */

const SERVER_API =
  process.env.API_INTERNAL_URL ?? process.env.NEXT_PUBLIC_API_URL ?? "http://localhost:8080";

const CLIENT_API = process.env.NEXT_PUBLIC_API_URL ?? "";

function apiBase(): string {
  return typeof window === "undefined" ? SERVER_API : CLIENT_API;
}

export type HealthResponse = {
  status: string;
  env: string;
  database: string;
  version: string;
};

export async function getHealth(): Promise<HealthResponse | null> {
  try {
    const res = await fetch(`${apiBase()}/api/v1/health`, {
      cache: "no-store",
    });
    if (!res.ok) return null;
    return (await res.json()) as HealthResponse;
  } catch {
    return null;
  }
}
