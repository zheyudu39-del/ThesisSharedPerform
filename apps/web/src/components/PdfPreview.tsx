"use client";

import Link from "next/link";
import {
  useCallback,
  useEffect,
  useLayoutEffect,
  useRef,
  useState,
} from "react";
import { AuthError } from "@/lib/auth";
import {
  formatBytes,
  getFile,
  getFilePages,
  isFailedStatus,
  isProcessingStatus,
  type FileDTO,
  type PageDTO,
} from "@/lib/files";

/**
 * PDF 内联预览（PRD §6.4 V-01 / V-02 / V-07）。
 *
 * 渲染策略：服务端已经把每页转成 PNG 并给出预签名 URL，前端不引入 pdf.js。
 *
 * - **虚拟渲染**：只为可视区 ±{@link OVERSCAN} 页挂载 <img>，其余页用等高占位撑住
 *   滚动条。离开窗口的页会被卸载，浏览器随之释放位图，单卡内存可控（V-02）。
 * - **滚动隔离**：容器 `overflow-y-auto` + `overscroll-behavior: contain`，
 *   在卡片内滚到底不会把滚动传给外层信息流（V-07）。
 * - **按需拉页**：页图 URL 15 分钟过期，所以不预取全部页；滚动到哪拉到哪，
 *   图片加载失败时丢弃该页 URL 并重新向服务端要一次（最多重试 2 次）。
 * - **未就绪**：status 处于转码流水线时展示进度提示并轮询 `GET /files/{id}`。
 * - **降级**：失败时展示封面与「在独立页打开」；若调用方提供 downloadHref 再给下载入口。
 */

/** 可视区上下各多渲染的页数。 */
const OVERSCAN = 2;

/** 页与页之间的间距（px），与占位高度计算保持一致。 */
const GAP = 12;

/** 转码中的轮询间隔。 */
const POLL_INTERVAL = 2500;

/** 默认页宽高比（A4，1200×1697），页图尺寸未知时用作占位。 */
const DEFAULT_RATIO = 1697 / 1200;

export type PdfPreviewProps = {
  fileId: string;
  /** 已知的文件元信息（例如刚上传完成），可省去首帧请求。 */
  initialFile?: FileDTO | null;
  /** 预览区高度（px）。信息流卡片默认 480（PRD §5.4）。 */
  maxHeight?: number;
  /** 「在独立页打开」的目标地址。 */
  openHref?: string;
  /** 下载原文件地址；不提供则不展示下载按钮（当前契约没有下载接口）。 */
  downloadHref?: string;
  /** 紧凑模式：用于转发嵌套等小尺寸场景。 */
  compact?: boolean;
  className?: string;
};

export function PdfPreview({
  fileId,
  initialFile = null,
  maxHeight = 480,
  openHref,
  downloadHref,
  compact = false,
  className = "",
}: PdfPreviewProps) {
  const [meta, setMeta] = useState<FileDTO | null>(initialFile);
  const [metaError, setMetaError] = useState<string | null>(null);
  const [pageCount, setPageCount] = useState(initialFile?.pageCount ?? 0);
  const [pages, setPages] = useState<Record<number, PageDTO>>({});
  const [initialLoaded, setInitialLoaded] = useState(false);
  const [pagesError, setPagesError] = useState<string | null>(null);
  const [viewport, setViewport] = useState({ start: 0, end: 0 });
  const [currentPage, setCurrentPage] = useState(1);
  const [size, setSize] = useState({ width: 0, height: 0 });
  const [retryTick, setRetryTick] = useState(0);

  const scrollRef = useRef<HTMLDivElement>(null);
  const pagesRef = useRef<Record<number, PageDTO>>({});
  const pageCountRef = useRef(pageCount);
  const inflightRef = useRef<Set<number>>(new Set());
  const attemptsRef = useRef<Record<number, number>>({});
  const mountedRef = useRef(true);

  useEffect(() => {
    mountedRef.current = true;
    return () => {
      mountedRef.current = false;
    };
  }, []);

  /* ---------------------------------------------------------------------- */
  /* 文件元信息：加载 + 转码中轮询                                            */
  /* ---------------------------------------------------------------------- */

  useEffect(() => {
    let cancelled = false;
    let timer: number | undefined;

    async function loadMeta() {
      try {
        const data = await getFile(fileId);
        if (cancelled) return;
        setMeta(data);
        setMetaError(null);
        if (data.pageCount > 0) {
          pageCountRef.current = data.pageCount;
          setPageCount(data.pageCount);
        }
        if (isProcessingStatus(data.status)) {
          timer = window.setTimeout(loadMeta, POLL_INTERVAL);
        }
      } catch (err) {
        if (cancelled) return;
        const message =
          err instanceof AuthError ? err.message : "无法加载文件信息，请稍后重试。";
        // 已有 initialFile 时保留它，避免一帧闪成错误页。
        setMetaError(message);
      }
    }

    void loadMeta();

    return () => {
      cancelled = true;
      if (timer !== undefined) window.clearTimeout(timer);
    };
  }, [fileId]);

  /* ---------------------------------------------------------------------- */
  /* 页图拉取                                                                */
  /* ---------------------------------------------------------------------- */

  const ensurePages = useCallback(
    async (start: number, end: number) => {
      const missing: number[] = [];
      for (let n = start; n <= end; n += 1) {
        if (n < 1) continue;
        const limit = pageCountRef.current;
        if (limit > 0 && n > limit) continue;
        if (pagesRef.current[n] || inflightRef.current.has(n)) continue;
        if ((attemptsRef.current[n] ?? 0) >= 2) continue;
        missing.push(n);
      }
      if (missing.length === 0) return;

      // 合并连续页，一次请求拿一段，减少往返。
      const groups: number[][] = [];
      for (const n of missing) {
        const last = groups[groups.length - 1];
        if (last && n === last[last.length - 1] + 1) last.push(n);
        else groups.push([n]);
      }

      for (const group of groups) {
        for (const n of group) inflightRef.current.add(n);
        try {
          const result = await getFilePages(fileId, group[0], group.length);
          if (!mountedRef.current) return;

          const next = { ...pagesRef.current };
          for (const page of result.pages) {
            if (group.includes(page.pageNo)) next[page.pageNo] = page;
          }
          pagesRef.current = next;
          setPages(next);
          setPagesError(null);
          setInitialLoaded(true);

          if (result.pageCount > 0 && result.pageCount !== pageCountRef.current) {
            pageCountRef.current = result.pageCount;
            setPageCount(result.pageCount);
          }
        } catch (err) {
          if (!mountedRef.current) return;
          const status = err instanceof AuthError ? err.status : 0;
          const message =
            err instanceof AuthError ? err.message : "页图加载失败，请稍后重试。";
          setInitialLoaded(true);

          for (const n of group) {
            const attempts = (attemptsRef.current[n] ?? 0) + 1;
            attemptsRef.current[n] = attempts;
          }

          // 401 / 403 / 404 是终态，重试也不会变好。
          if (status === 401 || status === 403 || status === 404) {
            setPagesError(message);
          } else {
            setPagesError(message);
            // 网络抖动或预签名过期：稍后再试一次。
            window.setTimeout(() => {
              if (mountedRef.current) setRetryTick((t) => t + 1);
            }, 3000);
          }
        } finally {
          for (const n of group) inflightRef.current.delete(n);
        }
      }
    },
    [fileId],
  );

  /* 首次进入 ready：先拉一小段，顺带拿到总页数。 */
  useEffect(() => {
    if (!meta || meta.status !== "ready") return;
    if (initialLoaded) return;
    void ensurePages(1, 6);
  }, [meta, initialLoaded, ensurePages]);

  /* 可视区间变化时补齐页图。 */
  useEffect(() => {
    if (pageCount <= 0) return;
    void ensurePages(viewport.start + 1, viewport.end + 1);
  }, [viewport.start, viewport.end, pageCount, pages, retryTick, ensurePages]);

  /* ---------------------------------------------------------------------- */
  /* 尺寸与滚动                                                              */
  /* ---------------------------------------------------------------------- */

  useLayoutEffect(() => {
    const el = scrollRef.current;
    if (!el) return;

    const measure = () => {
      setSize((prev) =>
        prev.width === el.clientWidth && prev.height === el.clientHeight
          ? prev
          : { width: el.clientWidth, height: el.clientHeight },
      );
    };
    measure();

    const observer = new ResizeObserver(measure);
    observer.observe(el);
    return () => observer.disconnect();
  }, [meta?.status, pageCount]);

  const ratio = pages[1] ? pages[1].height / pages[1].width : DEFAULT_RATIO;
  const pageStride = size.width > 0 ? size.width * ratio + GAP : 0;

  const recalc = useCallback(() => {
    const el = scrollRef.current;
    if (!el || pageStride <= 0 || pageCountRef.current <= 0) return;

    const total = pageCountRef.current;
    const first = Math.floor(el.scrollTop / pageStride);
    const visible = Math.max(1, Math.ceil(el.clientHeight / pageStride));
    const start = Math.max(0, Math.min(total - 1, first - OVERSCAN));
    const end = Math.max(0, Math.min(total - 1, first + visible - 1 + OVERSCAN));

    setViewport((prev) =>
      prev.start === start && prev.end === end ? prev : { start, end },
    );

    const centre = Math.floor((el.scrollTop + el.clientHeight / 2) / pageStride) + 1;
    setCurrentPage(Math.max(1, Math.min(total, centre)));
  }, [pageStride]);

  useLayoutEffect(() => {
    recalc();
  }, [recalc, pageCount, size.width, size.height]);

  const frameRef = useRef<number | null>(null);
  const handleScroll = useCallback(() => {
    if (frameRef.current !== null) return;
    frameRef.current = window.requestAnimationFrame(() => {
      frameRef.current = null;
      recalc();
    });
  }, [recalc]);

  useEffect(
    () => () => {
      if (frameRef.current !== null) window.cancelAnimationFrame(frameRef.current);
    },
    [],
  );

  /* 图片加载失败：丢弃该页 URL，交给 ensurePages 重新签发。 */
  const handleImageError = useCallback((pageNo: number) => {
    attemptsRef.current[pageNo] = (attemptsRef.current[pageNo] ?? 0) + 1;
    const next = { ...pagesRef.current };
    delete next[pageNo];
    pagesRef.current = next;
    setPages(next);
    inflightRef.current.delete(pageNo);
    setRetryTick((t) => t + 1);
  }, []);

  const scrollByPage = useCallback(
    (direction: 1 | -1) => {
      const el = scrollRef.current;
      if (!el || pageStride <= 0) return;
      el.scrollBy({ top: direction * pageStride, behavior: "smooth" });
    },
    [pageStride],
  );

  /* ---------------------------------------------------------------------- */
  /* 渲染                                                                    */
  /* ---------------------------------------------------------------------- */

  const shellClass = `overflow-hidden rounded-[6px] border border-line bg-surface ${className}`;

  if (metaError && !meta) {
    return (
      <div className={shellClass}>
        <FallbackPanel
          message={metaError}
          openHref={openHref}
          downloadHref={downloadHref}
          compact={compact}
        />
      </div>
    );
  }

  if (!meta) {
    return (
      <div className={shellClass} aria-busy>
        <SkeletonPage height={maxHeight} compact={compact} />
        <PreviewFooter currentPage={1} pageCount={0} openHref={openHref} compact={compact} />
      </div>
    );
  }

  if (isFailedStatus(meta.status)) {
    return (
      <div className={shellClass}>
        <FallbackPanel
          message={
            meta.status === "rejected"
              ? "该文件未通过安全扫描，无法预览。"
              : meta.errorReason
                ? // 服务端给出了具体原因（转换超时、缺少 LibreOffice 等），
                  // 直接展示比一句笼统的「处理失败」有用得多。
                  `文件处理失败：${meta.errorReason}`
                : "文件处理失败，暂时无法预览。"
          }
          coverUrl={meta.coverUrl}
          openHref={openHref}
          downloadHref={downloadHref}
          compact={compact}
        />
      </div>
    );
  }

  if (isProcessingStatus(meta.status)) {
    return (
      <div className={shellClass}>
        <ProcessingPanel status={meta} height={maxHeight} compact={compact} />
        <PreviewFooter currentPage={1} pageCount={0} openHref={openHref} compact={compact} />
      </div>
    );
  }

  // ready 但还没有任何页图：等首批请求返回；若确实没有页，则降级。
  if (pageCount === 0) {
    if (!initialLoaded) {
      return (
        <div className={shellClass} aria-busy>
          <SkeletonPage height={maxHeight} compact={compact} />
          <PreviewFooter currentPage={1} pageCount={0} openHref={openHref} compact={compact} />
        </div>
      );
    }
    return (
      <div className={shellClass}>
        <FallbackPanel
          message={pagesError ?? "该文件暂无可预览页面。"}
          coverUrl={meta.coverUrl}
          openHref={openHref}
          downloadHref={downloadHref}
          compact={compact}
        />
      </div>
    );
  }

  const rendered: React.ReactNode[] = [];
  for (let n = viewport.start + 1; n <= viewport.end + 1; n += 1) {
    if (n > pageCount) break;
    const page = pages[n];
    rendered.push(
      <div
        key={n}
        data-page-no={n}
        className="relative w-full overflow-hidden rounded-[3px] border border-line bg-surface-2"
        style={{ aspectRatio: `${page?.width ?? 1200} / ${page?.height ?? 1697}`, marginBottom: GAP }}
      >
        {page ? (
          // 预签名 URL 15 分钟过期；失败时 onError 会触发重新签发。
          // eslint-disable-next-line @next/next/no-img-element
          <img
            src={page.url}
            alt={`第 ${n} 页`}
            loading="lazy"
            decoding="async"
            draggable={false}
            onError={() => handleImageError(n)}
            className="absolute inset-0 h-full w-full object-contain"
          />
        ) : (
          <div className="absolute inset-0 animate-pulse bg-surface-2" aria-hidden />
        )}
      </div>,
    );
  }

  const leading = pageStride > 0 ? viewport.start * pageStride : 0;
  const trailing =
    pageStride > 0 ? Math.max(0, pageCount - 1 - viewport.end) * pageStride : 0;

  return (
    <div className={shellClass}>
      <div
        ref={scrollRef}
        onScroll={handleScroll}
        tabIndex={0}
        role="region"
        aria-label="PDF 预览，可上下滚动翻页"
        className="overflow-y-auto overscroll-contain bg-surface-2 focus:outline-none focus-visible:ring-2 focus-visible:ring-seal focus-visible:ring-inset"
        style={{ height: maxHeight, scrollbarGutter: "stable" }}
      >
        <div aria-hidden style={{ height: leading }} />
        {rendered}
        <div aria-hidden style={{ height: trailing }} />
      </div>

      <PreviewFooter
        currentPage={currentPage}
        pageCount={pageCount}
        openHref={openHref}
        compact={compact}
        onPrev={() => scrollByPage(-1)}
        onNext={() => scrollByPage(1)}
      />
    </div>
  );
}

/* -------------------------------------------------------------------------- */
/* 子组件                                                                      */
/* -------------------------------------------------------------------------- */

function PreviewFooter({
  currentPage,
  pageCount,
  openHref,
  compact,
  onPrev,
  onNext,
}: {
  currentPage: number;
  pageCount: number;
  openHref?: string;
  compact: boolean;
  onPrev?: () => void;
  onNext?: () => void;
}) {
  return (
    <div className="flex items-center justify-between gap-2 border-t border-line bg-surface px-3 py-1.5 font-mono text-[0.68rem] text-ink-3">
      <span className="tabular-nums">
        第 {currentPage} / {pageCount > 0 ? pageCount : "…"} 页
      </span>

      <div className="flex items-center gap-1">
        {onPrev && (
          <button
            type="button"
            onClick={onPrev}
            aria-label="上一页"
            className="grid size-6 place-items-center rounded-[3px] text-ink-2 hover:bg-surface-2 hover:text-ink"
          >
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" className="size-3.5" aria-hidden>
              <path d="m6 15 6-6 6 6" />
            </svg>
          </button>
        )}
        {onNext && (
          <button
            type="button"
            onClick={onNext}
            aria-label="下一页"
            className="grid size-6 place-items-center rounded-[3px] text-ink-2 hover:bg-surface-2 hover:text-ink"
          >
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" className="size-3.5" aria-hidden>
              <path d="m6 9 6 6 6-6" />
            </svg>
          </button>
        )}
        {openHref && (
          <Link
            href={openHref}
            className={`shrink-0 text-ink-2 hover:text-ink hover:underline ${compact ? "" : "ml-1"}`}
          >
            在独立页打开 ↗
          </Link>
        )}
      </div>
    </div>
  );
}

function SkeletonPage({ height, compact }: { height: number; compact: boolean }) {
  return (
    <div
      className="flex flex-col gap-3 bg-surface-2 p-4"
      style={{ height }}
      aria-hidden
    >
      <div className="flex-1 animate-pulse rounded-[3px] border border-line bg-surface" />
      {!compact && <div className="h-3 w-1/3 animate-pulse rounded-[3px] bg-surface" />}
    </div>
  );
}

function ProcessingPanel({
  status,
  height,
  compact,
}: {
  status: FileDTO;
  height: number;
  compact: boolean;
}) {
  const label =
    status.status === "scanning"
      ? "正在做安全扫描…"
      : status.status === "transcoding"
        ? "正在生成预览页…"
        : "正在处理文件…";

  return (
    <div
      className="flex flex-col items-center justify-center gap-3 bg-surface-2 px-5 text-center"
      style={{ height }}
      role="status"
      aria-live="polite"
    >
      <span
        aria-hidden
        className="size-6 animate-spin rounded-full border-2 border-line-strong border-t-seal"
      />
      <p className="text-[0.86rem] font-semibold text-ink">{label}</p>
      <p className="max-w-[36ch] text-[0.78rem] leading-relaxed text-ink-2">
        转码完成后会自动显示预览，无需刷新页面。
        {!compact && status.originalName ? `（${status.originalName}）` : ""}
      </p>
      {status.sizeBytes > 0 && (
        <p className="font-mono text-[0.68rem] text-ink-3">{formatBytes(status.sizeBytes)}</p>
      )}
    </div>
  );
}

function FallbackPanel({
  message,
  coverUrl,
  openHref,
  downloadHref,
  compact,
}: {
  message: string;
  coverUrl?: string | null;
  openHref?: string;
  downloadHref?: string;
  compact: boolean;
}) {
  return (
    <div className="flex flex-col items-center gap-3 bg-surface-2 px-5 py-6 text-center">
      {coverUrl ? (
        // eslint-disable-next-line @next/next/no-img-element
        <img
          src={coverUrl}
          alt="文件封面"
          loading="lazy"
          decoding="async"
          className="max-h-52 w-auto rounded-[3px] border border-line object-contain"
        />
      ) : (
        <span className="grid size-10 place-items-center rounded-[6px] border border-line bg-surface text-ink-3">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.6" className="size-5" aria-hidden>
            <path d="M14 3v5h5" />
            <path d="M19 8v11a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h7z" />
          </svg>
        </span>
      )}

      <p className="max-w-[42ch] text-[0.82rem] leading-relaxed text-ink-2">{message}</p>

      <div className="flex flex-wrap items-center justify-center gap-2">
        {openHref && (
          <Link
            href={openHref}
            className="rounded-[6px] border border-line-strong bg-surface px-3 py-1.5 text-[0.78rem] font-semibold text-ink-2 hover:border-ink-3 hover:text-ink"
          >
            在独立页打开
          </Link>
        )}
        {downloadHref && (
          <a
            href={downloadHref}
            download
            className="rounded-[6px] bg-seal px-3 py-1.5 text-[0.78rem] font-semibold text-white hover:bg-seal-deep"
          >
            下载原文件
          </a>
        )}
        {!openHref && !downloadHref && !compact && (
          <span className="font-mono text-[0.68rem] text-ink-3">可稍后重试</span>
        )}
      </div>
    </div>
  );
}
