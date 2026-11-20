import type { Metadata } from "next";
import { PapersWorkspace } from "@/app/papers/PapersWorkspace";
import { SiteHeader } from "@/components/SiteHeader";
import { absoluteUrl } from "@/lib/seo";

/**
 * 论文库外壳（Server Component）。
 *
 * 列表与解析表单依赖登录态，保留为客户端组件 {@link PapersWorkspace}；
 * 这里只负责可被爬虫抓取的标题 / 描述与 canonical。
 */
export const metadata: Metadata = {
  title: "论文库",
  description:
    "粘贴 DOI 或 arXiv 编号自动抓取论文元数据。以人的判断筛选前沿论文，从共读文献到共同成稿。",
  alternates: { canonical: absoluteUrl("/papers") },
  openGraph: {
    type: "website",
    title: "论文库 · Thesis Shared",
    description: "粘贴 DOI 或 arXiv 编号自动抓取论文元数据，以人的判断筛选前沿论文。",
    url: absoluteUrl("/papers"),
  },
};

export default function PapersPage() {
  return (
    <>
      <SiteHeader />
      <PapersWorkspace />
    </>
  );
}
