/**
 * SEO 辅助：站点地址、canonical、摘要截断与 JSON-LD 序列化。
 *
 * 服务端组件与 sitemap / robots 共用，不能依赖 window。
 */

/** 站点对外地址（用于 canonical / Open Graph / sitemap）。 */
export const SITE_URL = (
  process.env.NEXT_PUBLIC_SITE_URL ?? "http://localhost:3000"
).replace(/\/+$/, "");

/** 相对路径 → 绝对地址；已是绝对地址时原样返回。 */
export function absoluteUrl(path: string): string {
  if (/^https?:\/\//i.test(path)) return path;
  return `${SITE_URL}${path.startsWith("/") ? path : `/${path}`}`;
}

/** 压平空白后按 Unicode 码点截断，超出部分用省略号收尾。 */
export function truncate(value: string, max: number): string {
  const text = value.replace(/\s+/g, " ").trim();
  if ([...text].length <= max) return text;
  return `${[...text].slice(0, max).join("").trimEnd()}…`;
}

/**
 * 序列化 JSON-LD。
 *
 * 用 `dangerouslySetInnerHTML` 注入 `<script type="application/ld+json">` 时，
 * 内容里的 `</script>` 会提前闭合标签；把 `<` 转义成 `<` 即可阻断注入，
 * 同时仍是合法 JSON。
 */
export function jsonLd(data: unknown): { __html: string } {
  return { __html: JSON.stringify(data).replace(/</g, "\\u003c") };
}
