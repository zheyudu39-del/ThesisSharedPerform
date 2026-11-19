import Link from "next/link";
import {
  paperAuthorsText,
  paperIdentifier,
  type PaperDTO,
} from "@/lib/posts";

/**
 * 内联预览卡片（PRD V-01~V-07 的视觉基线）。
 *
 * 数据来自真实 PaperDTO；预览主体为标题 / 作者 / 摘要的滚动区，
 * 页码与页图渲染由 PDF 转码服务承担，接入后替换此处即可。
 * 独立页入口指向 /papers/[id]。
 */
export function PaperPreviewCard({
  paper,
  compact = false,
}: {
  paper: PaperDTO;
  /** 引用转发内嵌时用紧凑版本，不展示摘要滚动区。 */
  compact?: boolean;
}) {
  const identifier = paperIdentifier(paper);

  return (
    <div className="overflow-hidden rounded-[6px] border border-line bg-paper">
      {/* 预览头：标识 + 来源 */}
      <div className="flex items-center gap-2.5 border-b border-line px-3.5 py-2.5">
        <span className="min-w-0 truncate font-mono text-[0.68rem] text-ink-3">
          {identifier || "无外部标识"}
        </span>
        {paper.venue && (
          <span className="ml-auto shrink-0 font-mono text-[0.68rem] text-ink-3">{paper.venue}</span>
        )}
      </div>

      {/* 预览主体 */}
      {compact ? (
        <div className="px-3.5 py-3">
          <h4 className="mb-1 font-display text-[0.95rem] leading-snug font-semibold text-balance">
            {paper.title}
          </h4>
          <p className="truncate text-[0.78rem] text-ink-2">{paperAuthorsText(paper, 4)}</p>
        </div>
      ) : (
        <div
          className="max-h-[300px] overflow-y-auto overscroll-contain px-6 py-5"
          // 触控/滚轮到达边界时交还外层，避免「滚动卡死」
          style={{ scrollbarGutter: "stable" }}
        >
          <h4 className="mb-2 font-display text-[1.05rem] leading-snug font-semibold text-balance">
            {paper.title}
          </h4>
          <p className="mb-4 text-[0.8rem] text-ink-2">{paperAuthorsText(paper)}</p>
          {paper.abstract ? (
            <p className="text-[0.82rem] leading-relaxed text-ink-2">{paper.abstract}</p>
          ) : (
            <p className="text-[0.8rem] text-ink-3">该论文暂无摘要。</p>
          )}
        </div>
      )}

      {/* 预览底：出处信息 + 独立页入口 */}
      <div className="flex items-center justify-between gap-3 border-t border-line px-3.5 py-2 font-mono text-[0.68rem] text-ink-3">
        <span className="truncate">
          {[paper.publishedYear > 0 ? String(paper.publishedYear) : "", paper.metaSource]
            .filter(Boolean)
            .join(" · ") || "论文"}
        </span>
        <Link
          href={`/papers/${paper.id}`}
          className="shrink-0 text-ink-2 hover:text-ink hover:underline"
        >
          在独立页打开 ↗
        </Link>
      </div>
    </div>
  );
}
