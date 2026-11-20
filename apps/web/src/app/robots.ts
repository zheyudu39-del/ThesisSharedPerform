import type { MetadataRoute } from "next";
import { absoluteUrl } from "@/lib/seo";

/**
 * robots.txt（PRD §7.4）。
 *
 * 允许抓取公开内容（首页、论文库、论文详情、个人主页、帖子详情）；
 * 禁止抓取需要登录或包含个人数据的页面：
 * - /settings：账号与资料设置；
 * - /login /register：表单页，没有收录价值；
 * - /api/：接口，避免爬虫触发写操作或消耗限流额度。
 */
export default function robots(): MetadataRoute.Robots {
  return {
    rules: [
      {
        userAgent: "*",
        allow: "/",
        disallow: ["/settings", "/login", "/register", "/api/"],
      },
    ],
    sitemap: absoluteUrl("/sitemap.xml"),
    host: absoluteUrl("/"),
  };
}
