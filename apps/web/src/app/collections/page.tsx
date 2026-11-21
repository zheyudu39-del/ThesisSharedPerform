import type { Metadata } from "next";
import { CollectionsWorkspace } from "@/app/collections/CollectionsWorkspace";
import { SiteHeader } from "@/components/SiteHeader";
import { absoluteUrl } from "@/lib/seo";

/**
 * 精选列表外壳（Server Component）。
 *
 * 标题与描述放在服务端，供搜索引擎与分享预览抓取；
 * 列表内容依赖游标翻页，交给客户端工作区。
 */
export const metadata: Metadata = {
  title: "精选",
  description:
    "由运营与认证大V 人工策展的论文与观点合集。不做算法推荐，只呈现编辑读过后认为值得读的内容。",
  alternates: { canonical: absoluteUrl("/collections") },
  openGraph: {
    type: "website",
    title: "精选 · Thesis Shared",
    description: "由运营与认证大V 人工策展的论文与观点合集。",
    url: absoluteUrl("/collections"),
  },
  twitter: {
    card: "summary",
    title: "精选 · Thesis Shared",
    description: "由运营与认证大V 人工策展的论文与观点合集。",
  },
};

export default function CollectionsPage() {
  return (
    <>
      <SiteHeader />
      <CollectionsWorkspace />
    </>
  );
}
