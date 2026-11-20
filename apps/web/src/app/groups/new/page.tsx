import type { Metadata } from "next";
import Link from "next/link";
import { CreateGroupForm } from "@/app/groups/new/CreateGroupForm";
import { RequireAuth } from "@/components/RequireAuth";
import { SiteHeader } from "@/components/SiteHeader";
import { absoluteUrl } from "@/lib/seo";

/**
 * 创建群组（Server 外壳 + 客户端表单）。
 *
 * 需要登录：整块包在 RequireAuth 内，未登录会被带到登录页并在登录后回跳。
 */
export const metadata: Metadata = {
  title: "创建群组",
  description: "创建一个学术群组，把同方向的学者聚到一起，共享文献、讨论方法、协作成稿。",
  alternates: { canonical: absoluteUrl("/groups/new") },
  robots: { index: false },
};

export default function NewGroupPage() {
  return (
    <>
      <SiteHeader />
      <main className="mx-auto w-full max-w-2xl px-5 py-10">
        <Link
          href="/groups"
          className="mb-6 inline-flex items-center gap-1.5 text-[0.82rem] text-ink-2 hover:text-ink"
        >
          ← 全部群组
        </Link>

        <div className="mb-7 border-b border-line pb-6">
          <p className="mb-3 font-mono text-[0.72rem] tracking-[0.16em] text-ink-3 uppercase">
            群组 · New group
          </p>
          <h1 className="font-display text-[clamp(1.6rem,4vw,2.1rem)] leading-tight font-semibold tracking-[-0.02em]">
            创建一个群组
          </h1>
          <p className="mt-3 max-w-[54ch] text-[0.92rem] leading-relaxed text-ink-2">
            群组把关注同一个方向的学者聚在一起。创建后你是群主，可以审批申请、生成邀请码、管理成员。
          </p>
        </div>

        <RequireAuth>
          <CreateGroupForm />
        </RequireAuth>
      </main>
    </>
  );
}
