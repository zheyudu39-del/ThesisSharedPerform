import Link from "next/link";
import { PdfPreview } from "@/components/PdfPreview";
import {
  paperAuthorsText,
  paperIdentifier,
  type PaperDTO,
} from "@/lib/posts";

/**
 * 内联预览卡片（PRD V-01~V-07 的视觉基线）。
 *
 * - 论文关联了 PDF（`paper.pdfFileId`）时，主体交给 {@link PdfPreview}：
 *   服务端页图 + 虚拟滚动 + 滚动隔离，满足「卡片内连续翻页」；
 * - 没有文件时保持文本预览（标题 / 作者 / 摘要滚动区），不强行占位。
 *
 * 两种形态共用同一套头 / 底，独立页入口始终指向 /papers/[id]。
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
  const openHref = `/papers/${paper.id}`;

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
      {paper.pdfFileId ? (
        <div className="bg-surface-2 p-1.5">
          <PdfPreview
            fileId={paper.pdfFileId}
            openHref={openHref}
            compact={compact}
            maxHeight={compact ? 260 : 480}
          />
        </div>
      ) : compact ? (
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

      {/* 预览底：出处信息 + 独立页入口（PDF 形态下页脚已带入口，这里只留出处） */}
      <div className="flex items-center justify-between gap-3 border-t border-line px-3.5 py-2 font-mono text-[0.68rem] text-ink-3">
        <span className="truncate">
          {[paper.publishedYear > 0 ? String(paper.publishedYear) : "", paper.metaSource]
            .filter(Boolean)
            .join(" · ") || "论文"}
        </span>
        {!paper.pdfFileId && (
          <Link
            href={openHref}
            className="shrink-0 text-ink-2 hover:text-ink hover:underline"
          >
            在独立页打开 ↗
          </Link>
        )}
      </div>
    </div>
  );
}
