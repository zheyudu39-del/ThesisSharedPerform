import Link from "next/link";
import { formatRelativeTime, type CommentDTO } from "@/lib/posts";

/**
 * 评论列表的展示层。
 *
 * 本文件刻意不加 `"use client"`：它既被帖子详情页（Server Component）直接用于
 * SSR 首屏，也被「加载更多」客户端组件复用，两边共享同一套结构与分组逻辑。
 * 只依赖格式化函数与 `<Link>`，不触碰任何浏览器 API。
 */

export type CommentNode = { comment: CommentDTO; replies: CommentDTO[] };

/** 把扁平评论列表组织成「一级 + 回复」两层结构（更深层回复归入其根评论）。 */
export function groupComments(list: CommentDTO[]): CommentNode[] {
  const byId = new Map(list.map((c) => [c.id, c]));

  // 沿 parentId 向上找到根评论 id；缺失父评论或成环时把自身当作根。
  function rootIdOf(comment: CommentDTO): string {
    const visited = new Set<string>([comment.id]);
    let current = comment;
    while (current.parentId) {
      const parent = byId.get(current.parentId);
      if (!parent || visited.has(parent.id)) return current.id;
      visited.add(parent.id);
      current = parent;
    }
    return current.id;
  }

  const roots: CommentNode[] = [];
  const index = new Map<string, CommentNode>();

  for (const c of list) {
    if (rootIdOf(c) !== c.id) continue;
    const node: CommentNode = { comment: c, replies: [] };
    roots.push(node);
    index.set(c.id, node);
  }

  for (const c of list) {
    const root = index.get(rootIdOf(c));
    if (root && root.comment.id !== c.id) root.replies.push(c);
  }

  return roots;
}

/** 评论列表（已分组）。 */
export function CommentThread({ comments }: { comments: CommentDTO[] }) {
  return (
    <ul className="mt-4 flex flex-col gap-3">
      {groupComments(comments).map((node) => (
        <li key={node.comment.id}>
          <CommentItem comment={node.comment} />
          {node.replies.length > 0 && (
            <ul className="mt-2 ml-4 flex flex-col gap-2 border-l border-line pl-3.5">
              {node.replies.map((reply) => (
                <li key={reply.id}>
                  <CommentItem comment={reply} compact />
                </li>
              ))}
            </ul>
          )}
        </li>
      ))}
    </ul>
  );
}

export function CommentItem({
  comment,
  compact = false,
}: {
  comment: CommentDTO;
  compact?: boolean;
}) {
  const initial = [...comment.author.name][0] ?? "?";
  const hasHtml = comment.contentHtml.trim().length > 0;

  return (
    <article
      className={`rounded-[12px] border border-line bg-surface ${
        compact ? "px-3 py-2.5" : "p-3.5"
      } shadow-[var(--shadow-1)]`}
    >
      <div className="mb-1.5 flex items-center gap-2">
        <span
          className={`grid shrink-0 place-items-center rounded-full bg-ink font-display text-paper ${
            compact ? "size-5 text-[10px]" : "size-6 text-[11px]"
          }`}
          aria-hidden
        >
          {initial}
        </span>
        <Link
          href={`/u/${encodeURIComponent(comment.author.handle)}`}
          className="truncate text-[0.84rem] font-semibold hover:underline"
        >
          {comment.author.name}
        </Link>
        <span className="font-mono text-[0.7rem] text-ink-3">@{comment.author.handle}</span>
        <time
          dateTime={comment.createdAt}
          className="ml-auto shrink-0 font-mono text-[0.7rem] text-ink-3"
        >
          {formatRelativeTime(comment.createdAt)}
        </time>
      </div>

      {hasHtml ? (
        <div
          className="text-[0.88rem] leading-relaxed break-words text-ink-2 [&_a]:text-seal [&_a]:underline [&_code]:rounded-[3px] [&_code]:bg-surface-2 [&_code]:px-1 [&_code]:font-mono [&_p]:mb-1.5 [&_p:last-child]:mb-0"
          dangerouslySetInnerHTML={{ __html: comment.contentHtml }}
        />
      ) : (
        <p className="text-[0.88rem] leading-relaxed break-words whitespace-pre-wrap text-ink-2">
          {comment.contentText}
        </p>
      )}
    </article>
  );
}
