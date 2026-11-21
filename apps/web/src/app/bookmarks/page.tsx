import type { Metadata } from "next";
import { BookmarksWorkspace } from "@/app/bookmarks/BookmarksWorkspace";
import { SiteHeader } from "@/components/SiteHeader";
import { absoluteUrl } from "@/lib/seo";

/**
 * 收藏列表外壳（Server Component）。
 *
 * 收藏是个人数据，页面必须登录；外壳本身不读取登录态（会话在 HttpOnly Cookie 里），
 * 由客户端工作区 {@link BookmarksWorkspace} 用 RequireAuth 做跳转。
 * 与群组列表页保持一致：外壳只负责可被爬虫抓取的标题与 canonical。
 */
export const metadata: Metadata = {
  title: "收藏",
  description: "查看你收藏的帖子，按收藏时间倒序排列。",
  alternates: { canonical: absoluteUrl("/bookmarks") },
  // 收藏是个人数据，不应被搜索引擎索引。
  robots: { index: false, follow: true },
};

export default function BookmarksPage() {
  return (
    <>
      <SiteHeader />
      <BookmarksWorkspace />
    </>
  );
}
