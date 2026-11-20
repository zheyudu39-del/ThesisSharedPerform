"use client";

import { useEffect, useState } from "react";
import { FollowButton } from "@/components/FollowButton";
import { recallFollowState, rememberFollowState } from "@/lib/users";

/**
 * 公开个人主页的交互部分。
 *
 * Server Component 已经渲染了姓名 / 句柄 / 简介 / 研究方向与统计的初始值，
 * 这里只接管两件依赖浏览器的事：
 * 1. 关注按钮（需要登录态与写接口）；
 * 2. 关注后同步「关注者」计数——用浏览器事件在两块互不相邻的 DOM 之间传递，
 *    避免为了一个数字把整个页面变成客户端组件。
 */

const FOLLOW_EVENT = "thesis:follow-change";

type FollowDetail = { handle: string; followerCount: number };

function emitFollowChange(handle: string, followerCount: number) {
  window.dispatchEvent(
    new CustomEvent<FollowDetail>(FOLLOW_EVENT, { detail: { handle, followerCount } }),
  );
}

/**
 * 关注按钮。
 *
 * - 服务端匿名渲染时拿不到 following，因此先用会话内记住的状态兜底，
 *   避免「刚关注完刷新又显示未关注」；
 * - 关注成功后把状态记入会话，并广播计数变化。
 */
export function ProfileFollowButton({
  handle,
  userId,
  initialFollowing,
  followingKnown,
}: {
  handle: string;
  userId: string;
  initialFollowing: boolean;
  /** 服务端是否明确给出了关注状态；匿名请求时为 false。 */
  followingKnown: boolean;
}) {
  const [following, setFollowing] = useState(initialFollowing);

  // 服务端没有权威状态时，用本会话记住的状态补上（仅在客户端挂载后）。
  useEffect(() => {
    if (followingKnown) return;
    const cached = recallFollowState(handle);
    if (cached !== null) setFollowing(cached);
  }, [handle, followingKnown]);

  return (
    <FollowButton
      handle={handle}
      userId={userId}
      initialFollowing={following}
      onChange={(next) => {
        setFollowing(next.following);
        rememberFollowState(handle, next.following);
        emitFollowChange(handle, next.followerCount);
      }}
    />
  );
}

/** 关注者计数：初始值来自服务端，关注 / 取关后跟随事件更新。 */
export function FollowerCount({ handle, initial }: { handle: string; initial: number }) {
  const [count, setCount] = useState(initial);

  useEffect(() => {
    function onFollowChange(event: Event) {
      const detail = (event as CustomEvent<FollowDetail>).detail;
      if (detail?.handle === handle) setCount(detail.followerCount);
    }
    window.addEventListener(FOLLOW_EVENT, onFollowChange);
    return () => window.removeEventListener(FOLLOW_EVENT, onFollowChange);
  }, [handle]);

  return <>{count}</>;
}
