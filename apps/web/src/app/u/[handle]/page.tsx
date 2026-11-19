"use client";

import Link from "next/link";
import { useParams } from "next/navigation";
import { useAuth } from "@/components/AuthProvider";
import { SealBadge } from "@/components/SealBadge";
import { SiteHeader } from "@/components/SiteHeader";

/**
 * 个人主页（M1 占位版）。
 *
 * 目前只能展示当前登录用户自己的资料——因为还没有公开资料查询接口。
 * 后续接入 GET /api/v1/users/{handle} 后，可展示任意用户主页并支持 SSR/SEO
 * （见 PRD A-03「个人主页支持分享链接，SSR 可被搜索引擎索引」）。
 */
export default function ProfilePage() {
  const params = useParams<{ handle: string }>();
  const { status, user } = useAuth();

  const handle = decodeURIComponent(params.handle ?? "");
  const isSelf = status === "authenticated" && user?.handle === handle;

  return (
    <>
      <SiteHeader />

      <main className="mx-auto w-full max-w-3xl px-5 py-10">
        <p className="mb-3 font-mono text-[0.72rem] tracking-[0.16em] text-ink-3 uppercase">
          个人主页 · Profile
        </p>

        {isSelf && user ? (
          <>
            <div className="mb-8 flex flex-wrap items-start gap-5 border-b border-line pb-7">
              <div className="grid size-16 shrink-0 place-items-center rounded-full bg-ink font-display text-2xl text-paper">
                {[...user.name][0] ?? "?"}
              </div>
              <div className="min-w-0 flex-1">
                <h1 className="flex flex-wrap items-center gap-2 font-display text-[clamp(1.6rem,4vw,2.2rem)] leading-tight font-semibold tracking-[-0.02em]">
                  {user.name}
                  {user.verifyType === "scholar" && <SealBadge variant="scholar" />}
                  {user.verifyType === "influencer" && <SealBadge variant="influencer" />}
                </h1>
                <p className="mt-1 font-mono text-[0.8rem] text-ink-3">@{user.handle}</p>
                {user.institution && (
                  <p className="mt-2 text-[0.9rem] text-ink-2">{user.institution}</p>
                )}
              </div>
            </div>

            <dl className="grid grid-cols-2 gap-4 sm:grid-cols-4">
              <Stat label="研究方向" value={user.researchFields.length || 0} />
              <Stat label="发帖" value={0} />
              <Stat label="转发" value={0} />
              <Stat label="收藏" value={0} />
            </dl>

            <p className="mt-8 rounded-[12px] border border-line bg-surface p-4 text-[0.85rem] leading-relaxed text-ink-2">
              个人主页目前只展示你自己的资料。公开资料查询接口
              <code className="mx-1 rounded-[3px] bg-surface-2 px-1.5 py-0.5 font-mono text-[0.78rem]">
                GET /api/v1/users/:handle
              </code>
              尚未实现。
            </p>
          </>
        ) : (
          <div className="rounded-[12px] border border-line bg-surface p-6">
            <h1 className="font-display text-xl font-semibold">@{handle}</h1>
            <p className="mt-3 text-[0.9rem] leading-relaxed text-ink-2">
              公开用户资料尚未开放。当前版本只能查看你自己的主页。
            </p>
            <Link
              href="/"
              className="mt-5 inline-block rounded-[6px] border border-line-strong px-4 py-2 text-sm font-semibold text-ink hover:border-ink-3"
            >
              返回首页
            </Link>
          </div>
        )}
      </main>
    </>
  );
}

function Stat({ label, value }: { label: string; value: number }) {
  return (
    <div className="rounded-[12px] border border-line bg-surface p-4">
      <dt className="font-mono text-[0.68rem] tracking-[0.1em] text-ink-3 uppercase">{label}</dt>
      <dd className="mt-1.5 font-display text-2xl font-semibold tabular-nums">{value}</dd>
    </div>
  );
}
