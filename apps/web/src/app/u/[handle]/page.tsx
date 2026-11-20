import type { Metadata } from "next";
import Link from "next/link";
import { notFound } from "next/navigation";
import { FollowerCount, ProfileFollowButton } from "@/components/ProfileInteractive";
import { SealBadge } from "@/components/SealBadge";
import { SiteHeader } from "@/components/SiteHeader";
import { fetchPublicUser } from "@/lib/server-api";
import { absoluteUrl, jsonLd, truncate } from "@/lib/seo";

/**
 * 公开个人主页（Server Component，PRD A-03 / §7.4）。
 *
 * 姓名、句柄、简介、机构、研究方向与统计数字全部服务端渲染，便于搜索引擎
 * 抓取与分享链接直出；关注按钮与关注者计数拆到客户端组件。
 */

type PageProps = { params: Promise<{ handle: string }> };

/** Next 已解码动态段；这里再兜一次，防止手工编码的分享链接带来 `%` 残留。 */
function normalizeHandle(raw: string): string {
  try {
    return decodeURIComponent(raw);
  } catch {
    return raw;
  }
}

export async function generateMetadata({ params }: PageProps): Promise<Metadata> {
  const handle = normalizeHandle((await params).handle);
  const profile = await fetchPublicUser(handle);
  const canonical = absoluteUrl(`/u/${encodeURIComponent(handle)}`);

  if (!profile) {
    return { title: `@${handle}`, alternates: { canonical } };
  }

  const { user } = profile;
  const title = `${user.name} (@${user.handle})`;
  const description =
    truncate(user.bio, 160) ||
    truncate(
      [user.institution, user.researchFields.join("、")].filter(Boolean).join(" · "),
      160,
    ) ||
    `${user.name} 在 Thesis Shared 的个人主页`;

  return {
    title,
    description,
    alternates: { canonical },
    openGraph: {
      type: "profile",
      title,
      description,
      url: canonical,
    },
    twitter: { card: "summary", title, description },
  };
}

export default async function ProfilePage({ params }: PageProps) {
  const handle = normalizeHandle((await params).handle);
  const profile = await fetchPublicUser(handle);
  if (!profile) notFound();

  const { user, stats } = profile;

  const structuredData = {
    "@context": "https://schema.org",
    "@type": "ProfilePage",
    url: absoluteUrl(`/u/${encodeURIComponent(user.handle)}`),
    mainEntity: {
      "@type": "Person",
      name: user.name,
      alternateName: `@${user.handle}`,
      description: user.bio || undefined,
      affiliation: user.institution
        ? { "@type": "Organization", name: user.institution }
        : undefined,
      knowsAbout: user.researchFields.length > 0 ? user.researchFields : undefined,
      url: absoluteUrl(`/u/${encodeURIComponent(user.handle)}`),
    },
  };

  return (
    <>
      <SiteHeader />

      <main className="mx-auto w-full max-w-3xl px-5 py-10">
        <p className="mb-3 font-mono text-[0.72rem] tracking-[0.16em] text-ink-3 uppercase">
          个人主页 · Profile
        </p>

        <div className="mb-8 flex flex-wrap items-start gap-5 border-b border-line pb-7">
          <div className="grid size-16 shrink-0 place-items-center rounded-full bg-ink font-display text-2xl text-paper">
            {[...user.name][0] ?? "?"}
          </div>

          <div className="min-w-0 flex-1">
            <div className="flex flex-wrap items-start justify-between gap-3">
              <div className="min-w-0">
                <h1 className="flex flex-wrap items-center gap-2 font-display text-[clamp(1.6rem,4vw,2.2rem)] leading-tight font-semibold tracking-[-0.02em]">
                  {user.name}
                  {user.verifyType === "scholar" && <SealBadge variant="scholar" />}
                  {user.verifyType === "influencer" && <SealBadge variant="influencer" />}
                </h1>
                <p className="mt-1 font-mono text-[0.8rem] text-ink-3">@{user.handle}</p>
              </div>

              <ProfileFollowButton
                handle={user.handle}
                userId={user.id}
                initialFollowing={profile.following}
                followingKnown={profile.followingKnown}
              />
            </div>

            {user.institution && (
              <p className="mt-2.5 text-[0.9rem] text-ink-2">{user.institution}</p>
            )}
            {user.bio && (
              <p className="mt-2 max-w-[62ch] text-[0.9rem] leading-relaxed whitespace-pre-wrap text-ink-2">
                {user.bio}
              </p>
            )}
          </div>
        </div>

        {user.researchFields.length > 0 && (
          <div className="mb-7 flex flex-wrap gap-2">
            {user.researchFields.map((field) => (
              <span
                key={field}
                className="rounded-full border border-line bg-surface px-3 py-1 text-[0.78rem] text-ink-2"
              >
                {field}
              </span>
            ))}
          </div>
        )}

        <dl className="grid grid-cols-3 gap-4">
          <Stat label="发帖" value={stats.postCount} />
          <Stat
            label="关注者"
            value={<FollowerCount handle={user.handle} initial={stats.followerCount} />}
          />
          <Stat label="关注中" value={stats.followingCount} />
        </dl>

        <p className="mt-8 rounded-[12px] border border-dashed border-line-strong bg-surface px-5 py-6 text-center text-[0.85rem] leading-relaxed text-ink-2">
          该用户还没有公开的帖子列表。关注后，他/她的新帖会出现在你的关注流里。
        </p>

        <p className="mt-6 text-center">
          <Link href="/" className="text-[0.84rem] text-ink-2 hover:text-ink hover:underline">
            ← 返回信息流
          </Link>
        </p>
      </main>

      <script
        type="application/ld+json"
        dangerouslySetInnerHTML={jsonLd(structuredData)}
      />
    </>
  );
}

function Stat({ label, value }: { label: string; value: React.ReactNode }) {
  return (
    <div className="rounded-[12px] border border-line bg-surface p-4">
      <dt className="font-mono text-[0.68rem] tracking-[0.1em] text-ink-3 uppercase">{label}</dt>
      <dd className="mt-1.5 font-display text-2xl font-semibold tabular-nums">{value}</dd>
    </div>
  );
}
