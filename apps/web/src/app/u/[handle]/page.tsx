"use client";

import Link from "next/link";
import { useParams } from "next/navigation";
import { useCallback, useEffect, useRef, useState } from "react";
import { useAuth } from "@/components/AuthProvider";
import { FollowButton } from "@/components/FollowButton";
import { SealBadge } from "@/components/SealBadge";
import { SiteHeader } from "@/components/SiteHeader";
import { AuthError } from "@/lib/auth";
import {
  getPublicUser,
  recallFollowState,
  rememberFollowState,
  type PublicProfile,
} from "@/lib/users";

/**
 * 公开个人主页（PRD A-03）。
 *
 * 调用 `GET /users/{handle}` 展示任意用户的公开资料：姓名 / 句柄 / 简介 /
 * 机构 / 研究方向 / 统计。未登录也可浏览；登录后右上角出现关注按钮。
 *
 * 资料接口尚未上线（404）时给出明确说明，而不是空白页。
 */

export default function ProfilePage() {
  const params = useParams<{ handle: string }>();
  const { status, user } = useAuth();

  const handle = decodeURIComponent(params.handle ?? "");

  const [profile, setProfile] = useState<PublicProfile | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<{ message: string; notFound: boolean } | null>(null);
  const [followerCount, setFollowerCount] = useState(0);
  // 服务端未返回 following 时，用本会话记住的状态兜底（避免刷新后误显示「关注」）。
  const [following, setFollowing] = useState(() => recallFollowState(handle) ?? false);

  const requestId = useRef(0);

  const load = useCallback(async () => {
    const id = ++requestId.current;
    setLoading(true);
    setError(null);
    try {
      const data = await getPublicUser(handle);
      if (id !== requestId.current) return;
      setProfile(data);
      setFollowerCount(data.stats.followerCount);
      // 服务端给出关注状态时以它为准；否则保留会话内记住的状态。
      if (data.followingKnown) {
        setFollowing(data.following);
        rememberFollowState(handle, data.following);
      }
    } catch (err) {
      if (id !== requestId.current) return;
      setProfile(null);
      // 契约里只有 user_not_found 表示确定不存在；其它 404（例如路由尚未上线）
      // 仍然可重试，不应把用户误判成「不存在」。
      const notFound =
        err instanceof AuthError && err.status === 404 && err.code === "user_not_found";
      setError({
        notFound,
        message: notFound
          ? `找不到 @${handle} 这个用户。`
          : err instanceof AuthError
            ? err.message
            : "资料加载失败，请稍后重试。",
      });
    } finally {
      if (id === requestId.current) setLoading(false);
    }
  }, [handle]);

  useEffect(() => {
    void load();
  }, [load]);

  // 登录用户查看自己的主页时，即使公开接口失败也至少有本地资料可展示。
  const selfFallback = status === "authenticated" && user?.handle === handle ? user : null;

  return (
    <>
      <SiteHeader />

      <main className="mx-auto w-full max-w-3xl px-5 py-10">
        <p className="mb-3 font-mono text-[0.72rem] tracking-[0.16em] text-ink-3 uppercase">
          个人主页 · Profile
        </p>

        {loading && <ProfileSkeleton />}

        {!loading && error && !selfFallback && (
          <div className="rounded-[12px] border border-line bg-surface p-6">
            <p className="font-mono text-[0.68rem] tracking-[0.12em] text-danger uppercase">
              {error.notFound ? "用户不存在" : "加载失败"}
            </p>
            <p className="mt-2 text-[0.9rem] leading-relaxed text-ink-2">{error.message}</p>
            {!error.notFound && (
              <button
                type="button"
                onClick={() => void load()}
                className="mt-4 rounded-[6px] border border-line-strong px-4 py-1.5 text-[0.85rem] font-semibold text-ink hover:border-ink-3"
              >
                重试
              </button>
            )}
            <Link
              href="/"
              className="mt-4 ml-2 inline-block rounded-[6px] border border-line-strong px-4 py-1.5 text-[0.85rem] font-semibold text-ink hover:border-ink-3"
            >
              返回首页
            </Link>
          </div>
        )}

        {!loading && (profile || selfFallback) && (
          <ProfileBody
            name={profile?.user.name ?? selfFallback!.name}
            handle={profile?.user.handle ?? selfFallback!.handle}
            bio={profile?.user.bio ?? selfFallback!.bio}
            institution={profile?.user.institution ?? selfFallback!.institution}
            researchFields={profile?.user.researchFields ?? selfFallback!.researchFields}
            verifyType={profile?.user.verifyType ?? selfFallback!.verifyType}
            userId={profile?.user.id ?? selfFallback!.id}
            postCount={profile?.stats.postCount ?? 0}
            followerCount={profile ? followerCount : 0}
            followingCount={profile?.stats.followingCount ?? 0}
            initialFollowing={following}
            onFollowChange={(next) => {
              setFollowing(next.following);
              setFollowerCount(next.followerCount);
              rememberFollowState(handle, next.following);
            }}
            notice={
              profile
                ? null
                : "公开资料接口暂时不可用，以下是你本地的资料。"
            }
          />
        )}
      </main>
    </>
  );
}

/* -------------------------------------------------------------------------- */
/* 主体                                                                        */
/* -------------------------------------------------------------------------- */

function ProfileBody({
  name,
  handle,
  bio,
  institution,
  researchFields,
  verifyType,
  userId,
  postCount,
  followerCount,
  followingCount,
  initialFollowing,
  onFollowChange,
  notice,
}: {
  name: string;
  handle: string;
  bio: string;
  institution: string;
  researchFields: string[];
  verifyType: string;
  userId: string;
  postCount: number;
  followerCount: number;
  followingCount: number;
  initialFollowing: boolean;
  onFollowChange: (next: { following: boolean; followerCount: number }) => void;
  notice: string | null;
}) {
  return (
    <>
      {notice && (
        <p className="mb-4 rounded-[6px] border border-warn/40 bg-warn-wash px-3 py-2 text-[0.8rem] leading-relaxed text-warn">
          {notice}
        </p>
      )}

      <div className="mb-8 flex flex-wrap items-start gap-5 border-b border-line pb-7">
        <div className="grid size-16 shrink-0 place-items-center rounded-full bg-ink font-display text-2xl text-paper">
          {[...name][0] ?? "?"}
        </div>

        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-start justify-between gap-3">
            <div className="min-w-0">
              <h1 className="flex flex-wrap items-center gap-2 font-display text-[clamp(1.6rem,4vw,2.2rem)] leading-tight font-semibold tracking-[-0.02em]">
                {name}
                {verifyType === "scholar" && <SealBadge variant="scholar" />}
                {verifyType === "influencer" && <SealBadge variant="influencer" />}
              </h1>
              <p className="mt-1 font-mono text-[0.8rem] text-ink-3">@{handle}</p>
            </div>

            <FollowButton
              handle={handle}
              userId={userId}
              initialFollowing={initialFollowing}
              onChange={onFollowChange}
            />
          </div>

          {institution && <p className="mt-2.5 text-[0.9rem] text-ink-2">{institution}</p>}
          {bio && (
            <p className="mt-2 max-w-[62ch] text-[0.9rem] leading-relaxed whitespace-pre-wrap text-ink-2">
              {bio}
            </p>
          )}
        </div>
      </div>

      {researchFields.length > 0 && (
        <div className="mb-7 flex flex-wrap gap-2">
          {researchFields.map((field) => (
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
        <Stat label="发帖" value={postCount} />
        <Stat label="关注者" value={followerCount} />
        <Stat label="关注中" value={followingCount} />
      </dl>

      <p className="mt-8 rounded-[12px] border border-dashed border-line-strong bg-surface px-5 py-6 text-center text-[0.85rem] leading-relaxed text-ink-2">
        该用户还没有公开的帖子列表。关注后，他/她的新帖会出现在你的关注流里。
      </p>
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

function ProfileSkeleton() {
  return (
    <div aria-hidden>
      <div className="mb-8 flex items-start gap-5 border-b border-line pb-7">
        <div className="size-16 shrink-0 animate-pulse rounded-full bg-surface-2" />
        <div className="flex-1">
          <div className="mb-2 h-7 w-48 animate-pulse rounded-[6px] bg-surface-2" />
          <div className="h-3 w-32 animate-pulse rounded-[3px] bg-surface-2" />
        </div>
      </div>
      <div className="grid grid-cols-3 gap-4">
        {[0, 1, 2].map((i) => (
          <div key={i} className="h-20 animate-pulse rounded-[12px] bg-surface-2" />
        ))}
      </div>
    </div>
  );
}
