import { PaperPreviewCard, type PaperPreview } from "./PaperPreviewCard";
import { SealBadge } from "./SealBadge";

/**
 * 信息流卡片（PRD §5.4 卡片形态）。
 * 结构：作者行 → 转发观点 → 内联预览 → 互动栏。
 */

export type Post = {
  id: string;
  author: { name: string; handle: string; title: string; verified: boolean; influencer: boolean; initial: string };
  timeAgo: string;
  content: string;
  paper?: PaperPreview;
  stats: { likes: number; comments: number; reposts: number };
  tags: string[];
};

export function PostCard({ post }: { post: Post }) {
  return (
    <article className="rounded-[12px] border border-line bg-surface p-4.5 shadow-[var(--shadow-1)]">
      {/* 作者行 */}
      <div className="mb-3 flex items-center gap-2.5">
        <div className="grid size-[34px] shrink-0 place-items-center rounded-full bg-ink font-display text-[15px] text-paper">
          {post.author.initial}
        </div>
        <div className="min-w-0 flex-1">
          <div className="flex items-center gap-2 text-[0.9rem] font-semibold">
            {post.author.name}
            {post.author.verified && <SealBadge variant="scholar" />}
            {post.author.influencer && <SealBadge variant="influencer" />}
          </div>
          <div className="text-[0.78rem] text-ink-3">
            {post.author.title} · {post.timeAgo}
          </div>
        </div>
        <button
          type="button"
          className="ml-auto rounded-[6px] border border-line-strong bg-surface px-3 py-1 text-[0.78rem] font-semibold text-ink hover:border-ink-3"
        >
          关注
        </button>
      </div>

      {/* 正文 / 转发观点 */}
      <p className="mb-3.5 text-[0.92rem] leading-relaxed">{post.content}</p>

      {/* 内联预览 */}
      {post.paper && <PaperPreviewCard paper={post.paper} />}

      {/* 话题标签 */}
      {post.tags.length > 0 && (
        <div className="mt-3.5 flex flex-wrap gap-2">
          {post.tags.map((t) => (
            <span key={t} className="rounded-full bg-surface-2 px-3 py-1 text-[0.78rem] text-ink-2">
              <span className="font-mono text-seal">#</span>
              {t}
            </span>
          ))}
        </div>
      )}

      {/* 互动栏 */}
      <div className="mt-3.5 flex items-center gap-1 border-t border-line pt-2.5">
        <ActionButton label="赞" count={post.stats.likes} active />
        <ActionButton label="评论" count={post.stats.comments} />
        <ActionButton label="转发" count={post.stats.reposts} />
        <ActionButton label="收藏" className="ml-auto" />
      </div>
    </article>
  );
}

function ActionButton({
  label,
  count,
  active = false,
  className = "",
}: {
  label: string;
  count?: number;
  active?: boolean;
  className?: string;
}) {
  return (
    <button
      type="button"
      className={`inline-flex items-center gap-1.5 rounded-[6px] px-2.5 py-1.5 text-[0.84rem] hover:bg-surface-2 ${
        active ? "text-seal" : "text-ink-2"
      } ${className}`}
    >
      <span className="font-semibold">{label}</span>
      {count !== undefined && <span className="font-mono text-[0.78rem] tabular-nums">{count}</span>}
    </button>
  );
}
