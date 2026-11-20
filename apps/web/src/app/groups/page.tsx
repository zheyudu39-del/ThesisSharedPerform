import type { Metadata } from "next";
import { GroupsWorkspace } from "@/app/groups/GroupsWorkspace";
import { SiteHeader } from "@/components/SiteHeader";
import { absoluteUrl } from "@/lib/seo";

/**
 * 群组列表外壳（Server Component）。
 *
 * 列表依赖登录态与游标翻页，保留为客户端组件 {@link GroupsWorkspace}；
 * 这里只负责可被爬虫抓取的标题 / 描述与 canonical。
 */
export const metadata: Metadata = {
  title: "群组",
  description:
    "加入学术群组，与同行共读文献、讨论方法、协作成稿。公开群组无需邀请即可加入。",
  alternates: { canonical: absoluteUrl("/groups") },
  openGraph: {
    type: "website",
    title: "群组 · Thesis Shared",
    description: "加入学术群组，与同行共读文献、讨论方法、协作成稿。",
    url: absoluteUrl("/groups"),
  },
  twitter: {
    card: "summary",
    title: "群组 · Thesis Shared",
    description: "加入学术群组，与同行共读文献、讨论方法、协作成稿。",
  },
};

export default function GroupsPage() {
  return (
    <>
      <SiteHeader />
      <GroupsWorkspace />
    </>
  );
}
