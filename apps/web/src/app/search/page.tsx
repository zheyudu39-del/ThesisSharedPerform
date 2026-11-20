import type { Metadata } from "next";
import { SearchWorkspace } from "@/app/search/SearchWorkspace";
import { SiteHeader } from "@/components/SiteHeader";
import { absoluteUrl } from "@/lib/seo";

/**
 * 搜索页外壳（Server Component）。
 *
 * 检索是公开读接口，页面本身无需登录；输入、筛选与结果列表依赖浏览器交互，
 * 因此保留为客户端组件 {@link SearchWorkspace}。
 * 初始关键词从查询串读出后作为 prop 下传，避免客户端组件依赖 useSearchParams
 * 而被迫整页进入 Suspense 边界。
 */
export const metadata: Metadata = {
  title: "搜索",
  description: "在论文、帖子、群组与用户中统一检索，按类型筛选并高亮命中片段。",
  alternates: { canonical: absoluteUrl("/search") },
  robots: { index: false, follow: true },
  openGraph: {
    type: "website",
    title: "搜索 · Thesis Shared",
    description: "在论文、帖子、群组与用户中统一检索。",
    url: absoluteUrl("/search"),
  },
};

type PageProps = {
  searchParams: Promise<{ q?: string | string[] }>;
};

export default async function SearchPage({ searchParams }: PageProps) {
  const raw = (await searchParams).q;
  const initialQuery = (Array.isArray(raw) ? raw[0] : raw)?.trim() ?? "";

  return (
    <>
      <SiteHeader />
      <SearchWorkspace initialQuery={initialQuery} />
    </>
  );
}
