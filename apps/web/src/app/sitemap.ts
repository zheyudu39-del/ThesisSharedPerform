import type { MetadataRoute } from "next";
import { fetchPaperList } from "@/lib/server-api";
import { absoluteUrl } from "@/lib/seo";

/**
 * 动态 sitemap（PRD §7.4）。
 *
 * - 静态公开页：首页、论文库、登录、注册；
 * - 论文详情页：从公开的 `GET /papers?limit=100` 拉取。
 *
 * 接口不可用时只输出静态页，sitemap 本身不能因为上游故障而 500。
 */

/**
 * 强制在请求时生成。
 *
 * 若交给 Next 在构建时预渲染，构建环境通常连不上数据库 / API，sitemap 会把
 * 「空论文库」固化下来，之后要等 revalidate 窗口过去才补上。改为请求时生成后，
 * 底层的 fetch 仍按 {@link REVALIDATE_SECONDS} 走 Data Cache，不会每次都打 API。
 */
export const dynamic = "force-dynamic";

export default async function sitemap(): Promise<MetadataRoute.Sitemap> {
  const now = new Date();

  const staticRoutes: MetadataRoute.Sitemap = [
    { url: absoluteUrl("/"), lastModified: now, changeFrequency: "hourly", priority: 1 },
    { url: absoluteUrl("/papers"), lastModified: now, changeFrequency: "daily", priority: 0.8 },
    { url: absoluteUrl("/login"), lastModified: now, changeFrequency: "yearly", priority: 0.3 },
    { url: absoluteUrl("/register"), lastModified: now, changeFrequency: "yearly", priority: 0.3 },
  ];

  const papers = await fetchPaperList(100);
  const paperRoutes: MetadataRoute.Sitemap = papers.map((paper) => ({
    url: absoluteUrl(`/papers/${paper.id}`),
    lastModified: paper.createdAt ? new Date(paper.createdAt) : now,
    changeFrequency: "weekly",
    priority: 0.7,
  }));

  return [...staticRoutes, ...paperRoutes];
}
