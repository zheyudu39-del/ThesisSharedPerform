/**
 * 内联预览卡片（PRD V-01~V-07 的视觉基线）。
 *
 * 当前为静态占位：真实实现由 pdf.js + 服务端 WebP 页图承担，
 * 见技术架构文档 §4.2「信息流卡片内联预览」。
 * 这里先固化交互骨架——滚动隔离、页码、独立页入口、受限提示。
 */

export type PaperPreview = {
  doi: string;
  title: string;
  authors: string;
  abstract: string;
  pageCount: number;
  currentPage: number;
};

export function PaperPreviewCard({ paper }: { paper: PaperPreview }) {
  return (
    <div className="overflow-hidden rounded-[6px] border border-line bg-paper">
      {/* 预览头：标识 + 权限/页数 */}
      <div className="flex items-center gap-2.5 border-b border-line px-3.5 py-2.5">
        <span className="font-mono text-[0.68rem] text-ink-3">{paper.doi}</span>
        <span className="ml-auto font-mono text-[0.68rem] text-ink-3">
          内联预览 · {paper.pageCount} 页
        </span>
      </div>

      {/* 预览主体：独立滚动容器，滚动不带动外层信息流 */}
      <div
        className="max-h-[300px] overflow-y-auto overscroll-contain px-6 py-5"
        // 触控/滚轮到达边界时交还外层，避免「滚动卡死」
        style={{ scrollbarGutter: "stable" }}
      >
        <h4 className="mb-2 font-display text-[1.05rem] leading-snug font-semibold text-balance">
          {paper.title}
        </h4>
        <p className="mb-4 text-[0.8rem] text-ink-2">{paper.authors}</p>
        <p className="text-[0.82rem] leading-relaxed text-ink-2">{paper.abstract}</p>
      </div>

      {/* 预览底：页码 + 翻页指示 + 独立页入口 */}
      <div className="flex items-center justify-between border-t border-line px-3.5 py-2 font-mono text-[0.68rem] text-ink-3">
        <span>第 {paper.currentPage} 页</span>
        <div className="flex gap-1.5">
          {Array.from({ length: Math.min(paper.pageCount, 5) }).map((_, i) => (
            <span
              key={i}
              className={
                i === paper.currentPage - 1
                  ? "size-[5px] rounded-full bg-seal"
                  : "size-[5px] rounded-full bg-line-strong"
              }
            />
          ))}
        </div>
        <a href="#" className="hover:text-ink">
          在独立页打开 ↗
        </a>
      </div>
    </div>
  );
}
