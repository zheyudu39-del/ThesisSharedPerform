"use client";

import { useEffect, useState } from "react";
import { PostCard } from "@/components/PostCard";
import type { PostDTO } from "@/lib/posts";

/**
 * 帖子详情页的正文卡片（客户端外壳）。
 *
 * 为什么需要它：{@link PostCard} 的点赞 / 收藏 / 转发都是客户端交互，而评论数
 * 会随「发表评论」变化。Server Component 无法在交互后重新渲染，因此这里持有
 * 一份帖子状态，监听评论广播并同步 commentCount，避免卡片上的数字与标题栏脱节。
 */

const COMMENT_EVENT = "thesis:comment-created";

export function PostDetailCard({ post }: { post: PostDTO }) {
  const [current, setCurrent] = useState(post);

  // 服务端重新渲染（router.refresh）时用新数据覆盖本地状态。
  useEffect(() => {
    setCurrent(post);
  }, [post]);

  useEffect(() => {
    function onCommentCreated(event: Event) {
      const detail = (event as CustomEvent<{ postId: string }>).detail;
      if (detail?.postId !== post.id) return;
      setCurrent((prev) => ({ ...prev, commentCount: prev.commentCount + 1 }));
    }
    window.addEventListener(COMMENT_EVENT, onCommentCreated);
    return () => window.removeEventListener(COMMENT_EVENT, onCommentCreated);
  }, [post.id]);

  return <PostCard post={current} detail onPatch={setCurrent} />;
}
