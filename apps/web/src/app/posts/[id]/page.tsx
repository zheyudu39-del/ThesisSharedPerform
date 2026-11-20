import type { Metadata } from "next";
import Link from "next/link";
import { notFound } from "next/navigation";
import { CommentThread } from "@/components/CommentThread";
import { PostDetailCard } from "@/components/PostDetailCard";
import { CommentCount, PostComments } from "@/components/PostComments";
import { PostRepostPrompt } from "@/components/PostRepostPrompt";
import { SiteHeader } from "@/components/SiteHeader";
import { fetchComments, fetchPost } from "@/lib/server-api";
import { absoluteUrl, truncate } from "@/lib/seo";

/**
 * 帖子详情页（Server Component，PRD §7.4）。
 *
 * - 正文由 {@link PostDetailCard} 直出（它内部用 PostCard 渲染，首屏随 SSR 输出）；
 * - 评论列表在服务端拉取并渲染，爬虫能读到讨论内容；
 * - 发表评论、「加载更多」、转发框是客户端交互
 *   （{@link PostComments} / {@link PostRepostPrompt}）。
 *
 * 公开读接口允许匿名访问；私密帖与不存在的帖子都返回 404，这里统一 notFound()。
 */

type PageProps = {
  params: Promise<{ id: string }>;
  searchParams: Promise<{ repost?: string | string[] }>;
};

export async function generateMetadata({ params }: PageProps): Promise<Metadata> {
  const { id } = await params;
  const post = await fetchPost(id);
  const canonical = absoluteUrl(`/posts/${id}`);

  if (!post) {
    return { title: "帖子", alternates: { canonical }, robots: { index: false } };
  }

  const title = truncate(post.contentText, 50) || "帖子";
  const description =
    truncate(post.contentText, 160) ||
    (post.paper
      ? `${post.author.name} 分享的论文：${post.paper.title}`
      : `${post.author.name} 的帖子`);

  return {
    title,
    description,
    alternates: { canonical },
    openGraph: {
      type: "article",
      title,
      description,
      url: canonical,
      publishedTime: post.createdAt || undefined,
      authors: post.author.name ? [post.author.name] : undefined,
    },
    twitter: { card: "summary", title, description },
  };
}

export default async function PostDetailPage({ params, searchParams }: PageProps) {
  const { id } = await params;
  const [post, comments] = await Promise.all([fetchPost(id), fetchComments(id, 20)]);
  if (!post) notFound();

  const wantsRepost = (await searchParams).repost === "1";
  const initialComments = comments?.comments ?? [];

  return (
    <>
      <SiteHeader />
      <main className="mx-auto w-full max-w-3xl px-5 py-10">
        <Link
          href="/"
          className="mb-6 inline-flex items-center gap-1.5 text-[0.82rem] text-ink-2 hover:text-ink"
        >
          ← 返回信息流
        </Link>

        {wantsRepost && (
          <PostRepostPrompt postId={post.id} authorHandle={post.author.handle} />
        )}

        <PostDetailCard post={post} />

        <section id="comments" className="mt-8 scroll-mt-24">
          <h2 className="mb-4 flex items-baseline gap-2 border-b border-line pb-2.5">
            <span className="font-display text-[1.15rem] font-semibold">评论</span>
            <span className="font-mono text-[0.75rem] tabular-nums text-ink-3">
              <CommentCount postId={post.id} initial={post.commentCount} />
            </span>
          </h2>

          {/* 首屏评论：服务端直出，爬虫可见。 */}
          {initialComments.length > 0 ? (
            <CommentThread comments={initialComments} />
          ) : comments ? (
            <p className="mt-4 rounded-[12px] border border-dashed border-line-strong bg-surface px-5 py-8 text-center text-[0.86rem] text-ink-2">
              还没有评论。写下你的第一条判断。
            </p>
          ) : null}

          {/* 评论框、加载更多、新评论插入。 */}
          <div className="mt-4">
            <PostComments
              postId={post.id}
              initialNextCursor={comments?.nextCursor ?? null}
              commentsAvailable={comments !== null}
            />
          </div>
        </section>
      </main>
    </>
  );
}
