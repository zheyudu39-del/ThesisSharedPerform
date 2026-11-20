"use client";

import Link from "next/link";
import { useRouter, useSearchParams } from "next/navigation";
import { Suspense, useState } from "react";
import { useAuth } from "@/components/AuthProvider";
import { PostComposer } from "@/components/PostComposer";

/**
 * `?repost=1` 的转发入口。
 *
 * 只有登录用户才显示转发框；匿名用户看到登录引导（带 next，登录后回到本页
 * 并保留转发意图）。转发成功后清掉查询串，避免刷新又弹一次。
 */
export function PostRepostPrompt({
  postId,
  authorHandle,
}: {
  postId: string;
  authorHandle: string;
}) {
  // useSearchParams 需要 Suspense 边界。
  return (
    <Suspense fallback={null}>
      <RepostPromptInner postId={postId} authorHandle={authorHandle} />
    </Suspense>
  );
}

function RepostPromptInner({ postId, authorHandle }: { postId: string; authorHandle: string }) {
  const { status } = useAuth();
  const router = useRouter();
  const searchParams = useSearchParams();
  const [reposted, setReposted] = useState(false);

  // 转发成功后 URL 会被改写为不带 ?repost=1；这里保留一条成功提示，
  // 而不是让提示随查询串一起消失。
  if (reposted) {
    return (
      <p
        role="status"
        className="mb-4 rounded-[6px] border border-good/40 bg-good-wash px-3 py-2 text-[0.82rem] text-good"
      >
        已转发。去你的主页或关注流看看。
      </p>
    );
  }

  if (searchParams.get("repost") !== "1") return null;

  if (status === "loading") {
    return <div className="mb-4 h-28 animate-pulse rounded-[12px] bg-surface-2" aria-hidden />;
  }

  if (status === "anonymous") {
    return (
      <p className="mb-4 rounded-[12px] border border-dashed border-line-strong bg-surface px-4 py-4 text-center text-[0.86rem] text-ink-2">
        <Link
          href={`/login?next=${encodeURIComponent(`/posts/${postId}?repost=1`)}`}
          className="font-semibold text-seal hover:underline"
        >
          登录
        </Link>
        后才能转发。
      </p>
    );
  }

  return (
    <div className="mb-4">
      <PostComposer
        repostOf={postId}
        repostLabel={`@${authorHandle} 的帖子`}
        autoFocus
        onCreated={() => {
          setReposted(true);
          router.replace(`/posts/${postId}`);
          // 让服务端重新取一次帖子，转发数随之更新。
          router.refresh();
        }}
      />
    </div>
  );
}
