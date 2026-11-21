import type { Metadata } from "next";
import Link from "next/link";
import { notFound } from "next/navigation";
import { GroupAdminPanel } from "@/components/GroupAdminPanel";
import { GroupDocuments } from "@/components/GroupDocuments";
import { GroupFeed } from "@/app/groups/[slug]/GroupFeed";
import { GroupJoinButton, GroupMemberCount } from "@/components/GroupMembership";
import { GroupMembersPreview } from "@/app/groups/[slug]/GroupMembersPreview";
import { SiteHeader } from "@/components/SiteHeader";
import { fetchGroup, fetchGroupFeed } from "@/lib/server-api";
import { JOIN_POLICY_HINT, JOIN_POLICY_LABEL } from "@/lib/groups";
import { absoluteUrl, jsonLd, truncate } from "@/lib/seo";

/**
 * 群组主页（Server Component）。
 *
 * 群名、简介、成员数、加入方式全部服务端渲染，便于分享链接直出与搜索引擎抓取；
 * 加入按钮、成员列表、邀请码、入群申请审批、群内动态拆到客户端组件。
 *
 * 身份与成员数随会话变化，因此这里读取的 /groups/{slug} 不能走共享 Data Cache
 * （见 lib/server-api.ts 的 fetchGroup）。
 */

type PageProps = { params: Promise<{ slug: string }> };

function normalizeSlug(raw: string): string {
  try {
    return decodeURIComponent(raw);
  } catch {
    return raw;
  }
}

export async function generateMetadata({ params }: PageProps): Promise<Metadata> {
  const slug = normalizeSlug((await params).slug);
  const detail = await fetchGroup(slug);
  const canonical = absoluteUrl(`/groups/${encodeURIComponent(slug)}`);

  if (!detail) {
    return { title: "群组", alternates: { canonical }, robots: { index: false } };
  }

  const { group } = detail;
  const title = group.name;
  const description =
    truncate(group.description, 160) ||
    `${group.name} · ${JOIN_POLICY_LABEL[group.joinPolicy]} · ${detail.memberCount} 名成员`;

  return {
    title,
    description,
    alternates: { canonical },
    openGraph: {
      type: "website",
      title: `${group.name} · Thesis Shared`,
      description,
      url: canonical,
    },
    twitter: { card: "summary", title, description },
  };
}

export default async function GroupPage({ params }: PageProps) {
  const slug = normalizeSlug((await params).slug);
  const detail = await fetchGroup(slug);
  if (!detail) notFound();

  const { group, myRole, memberCount } = detail;
  const isMember = myRole !== null;
  const canManage = myRole === "owner" || myRole === "admin";

  // 群内动态仅成员可读；非成员不请求，避免必然 401 的噪声请求。
  const feedPosts = isMember ? await fetchGroupFeed(slug, 20) : null;

  const structuredData = {
    "@context": "https://schema.org",
    "@type": "Organization",
    name: group.name,
    description: group.description || undefined,
    url: absoluteUrl(`/groups/${encodeURIComponent(group.slug)}`),
  };

  return (
    <>
      <SiteHeader />

      <main className="mx-auto w-full max-w-4xl px-5 py-10">
        <Link
          href="/groups"
          className="mb-6 inline-flex items-center gap-1.5 text-[0.82rem] text-ink-2 hover:text-ink"
        >
          ← 全部群组
        </Link>

        {/* 群组头部 */}
        <header className="mb-8 border-b border-line pb-7">
          <p className="mb-3 font-mono text-[0.72rem] tracking-[0.16em] text-ink-3 uppercase">
            群组 · Group
          </p>

          <div className="flex flex-wrap items-start gap-5">
            <div className="size-16 shrink-0 overflow-hidden rounded-[12px] bg-surface-2">
              {group.coverUrl ? (
                // 封面来自用户上传；不用 next/image 以免新增远程域名配置。
                // eslint-disable-next-line @next/next/no-img-element
                <img src={group.coverUrl} alt="" className="size-full object-cover" />
              ) : (
                <span className="grid size-full place-items-center bg-ink font-display text-2xl font-semibold text-paper">
                  {[...group.name][0] ?? "群"}
                </span>
              )}
            </div>

            <div className="min-w-0 flex-1">
              <div className="flex flex-wrap items-start justify-between gap-3">
                <div className="min-w-0">
                  <h1 className="font-display text-[clamp(1.6rem,4vw,2.2rem)] leading-tight font-semibold tracking-[-0.02em] text-balance">
                    {group.name}
                  </h1>
                  <p className="mt-2 flex flex-wrap items-center gap-x-3 gap-y-1 font-mono text-[0.72rem] text-ink-3">
                    <span className="rounded-full bg-surface-2 px-2 py-0.5 tracking-[0.04em] text-ink-2">
                      {JOIN_POLICY_LABEL[group.joinPolicy]}
                    </span>
                    <span>
                      <GroupMemberCount slug={group.slug} initial={memberCount} /> 名成员
                    </span>
                    {group.ownerName && <span>群主 {group.ownerName}</span>}
                  </p>
                </div>

                <GroupJoinButton
                  slug={group.slug}
                  joinPolicy={group.joinPolicy}
                  initialRole={myRole}
                  memberCount={memberCount}
                />
              </div>

              {group.description && (
                <p className="mt-3 max-w-[62ch] text-[0.92rem] leading-relaxed whitespace-pre-wrap text-ink-2">
                  {group.description}
                </p>
              )}

              {!isMember && (
                <p className="mt-3 rounded-[6px] border border-line bg-surface px-3 py-2 text-[0.82rem] leading-relaxed text-ink-2">
                  {JOIN_POLICY_HINT[group.joinPolicy]}
                </p>
              )}
            </div>
          </div>
        </header>

        <div className="grid gap-8 lg:grid-cols-[minmax(0,1fr)_280px]">
          {/* 主栏：协作文档 */}
          <section className="min-w-0">
            <h2 className="mb-4 flex items-baseline gap-2 border-b border-line pb-2.5">
              <span className="font-display text-[1.15rem] font-semibold">协作文档</span>
              <span className="font-mono text-[0.7rem] tracking-[0.06em] text-ink-3 uppercase">
                Documents
              </span>
            </h2>
            <GroupDocuments slug={group.slug} isMember={isMember} />
          </section>

          {/* 主栏：群内动态 */}
          <section className="min-w-0">
            <h2 className="mb-4 flex items-baseline gap-2 border-b border-line pb-2.5">
              <span className="font-display text-[1.15rem] font-semibold">群内动态</span>
              <span className="font-mono text-[0.7rem] tracking-[0.06em] text-ink-3 uppercase">
                Feed
              </span>
            </h2>

            {isMember ? (
              <GroupFeed
                slug={group.slug}
                initialPosts={feedPosts?.posts ?? []}
                initialNextCursor={feedPosts?.nextCursor ?? null}
                feedAvailable={feedPosts !== null}
              />
            ) : (
              <p className="rounded-[12px] border border-dashed border-line-strong bg-surface px-5 py-10 text-center text-[0.88rem] leading-relaxed text-ink-2">
                加入群组后可以查看群内动态。
              </p>
            )}
          </section>

          {/* 侧栏：成员预览 */}
          <aside className="min-w-0">
            <GroupMembersPreview slug={group.slug} isMember={isMember} memberCount={memberCount} />
          </aside>
        </div>

        {/* 管理面板（仅 owner/admin） */}
        {canManage && myRole && (
          <div className="mt-8">
            <GroupAdminPanel slug={group.slug} myRole={myRole} memberCount={memberCount} />
          </div>
        )}
      </main>

      <script type="application/ld+json" dangerouslySetInnerHTML={jsonLd(structuredData)} />
    </>
  );
}
