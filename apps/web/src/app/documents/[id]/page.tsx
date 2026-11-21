import type { Metadata } from "next";
import Link from "next/link";
import { notFound } from "next/navigation";
import { DocumentWorkspace } from "./DocumentWorkspace";
import { SiteHeader } from "@/components/SiteHeader";
import { fetchDocument } from "@/lib/server-api";

type PageProps = { params: Promise<{ id: string }> };

export async function generateMetadata({ params }: PageProps): Promise<Metadata> {
  const { id } = await params;
  const found = await fetchDocument(id);
  if (!found) return { title: "文档不存在", robots: { index: false, follow: false } };
  return {
    title: found.document.title,
    // 协作文档是私有内容，不参与索引。
    robots: { index: false, follow: false },
  };
}

export default async function DocumentPage({ params }: PageProps) {
  const { id } = await params;
  const found = await fetchDocument(id);

  if (!found) notFound();

  const { document: doc, myRole } = found;
  if (!myRole) notFound();

  return (
    <>
      <SiteHeader />
      <main className="mx-auto w-full max-w-4xl px-5 py-8">
        <nav className="mb-4 flex items-center gap-2 font-mono text-[0.72rem] text-ink-3">
          <Link href="/groups" className="hover:text-ink-2">
            群组
          </Link>
          <span aria-hidden>/</span>
          <Link href={`/groups/${doc.groupSlug}`} className="hover:text-ink-2">
            {doc.groupSlug}
          </Link>
          <span aria-hidden>/</span>
          <span className="truncate text-ink-2">{doc.title}</span>
        </nav>

        <DocumentWorkspace document={doc} role={myRole} />
      </main>
    </>
  );
}
