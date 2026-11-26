"use client";

import Link from "next/link";
import { useEffect, useState } from "react";
import { listCollections, type CollectionDTO } from "@/lib/collections";
import { listPapers, type PaperDTO } from "@/lib/papers";
import { listTopTags, type TagDTO } from "@/lib/posts";
import type { HealthResponse } from "@/lib/api";

/**
 * 首页右栏。
 *
 * 三块内容全部来自公开接口（话题、论文库、精选），因此匿名访客也能看到，
 * 不会出现「未登录时整条侧栏是空的」。
 *
 * 取舍：这里不做「猜你喜欢」式的推荐。平台的产品前提是人工筛选，
 * 右栏只陈列「已经被别人整理过的东西」——话题的热度、最新入库的论文、
 * 编辑写下的栏目。它们都有可解释的排序依据。
 *
 * 加载失败一律静默降级为不渲染该块，避免首页因为一个侧栏接口挂掉而变丑。
 */
export function HomeRail({ health }: { health: HealthResponse | null }) {
  // null = 仍在加载；空数组 = 确认没有数据。
  // 区分两者是为了在加载期间保留占位高度，否则三块数据到达时
  // 会把下方的「排序规则 / 服务状态」整体推下去，读的时候会跳一下。
  const [tags, setTags] = useState<TagDTO[] | null>(null);
  const [papers, setPapers] = useState<PaperDTO[] | null>(null);
  const [collections, setCollections] = useState<CollectionDTO[] | null>(null);

  useEffect(() => {
    let cancelled = false;

    // 三块互不依赖，各自失败各自消失。
    void listTopTags(8)
      .then((list) => {
        if (!cancelled) setTags(list);
      })
      .catch(() => {
        if (!cancelled) setTags([]);
      });

    void listPapers(4, 0)
      .then((page) => {
        if (!cancelled) setPapers(page.papers ?? []);
      })
      .catch(() => {
        if (!cancelled) setPapers([]);
      });

    void listCollections(null, 3)
      .then((page) => {
        if (!cancelled) setCollections(page.collections ?? []);
      })
      .catch(() => {
        if (!cancelled) setCollections([]);
      });

    return () => {
      cancelled = true;
    };
  }, []);

  return (
    <div className="flex flex-col gap-4">
      {tags === null ? (
        <RailSkeleton title="热门话题" rows={2} />
      ) : tags.length > 0 ? (
        <RailPanel title="热门话题" href="/tags" linkLabel="全部话题">
          <ul className="flex flex-wrap gap-1.5">
            {tags.map((tag) => (
              <li key={tag.slug}>
                <Link
                  href={`/tags/${encodeURIComponent(tag.slug)}`}
                  className="inline-flex items-baseline gap-1 rounded-full border border-line px-2.5 py-1 text-[0.78rem] text-ink-2 transition-colors hover:border-seal/50 hover:bg-seal-wash hover:text-seal"
                >
                  <span className="font-mono text-[0.72rem] text-seal">#</span>
                  <span className="max-w-[9rem] truncate">{tag.name}</span>
                  <span className="font-mono text-[0.64rem] text-ink-3 tabular-nums">
                    {tag.postCount}
                  </span>
                </Link>
              </li>
            ))}
          </ul>
        </RailPanel>
      ) : null}

      {papers === null ? (
        <RailSkeleton title="最近入库" rows={3} />
      ) : papers.length > 0 ? (
        <RailPanel title="最近入库" href="/papers" linkLabel="论文库">
          <ul className="flex flex-col divide-y divide-line">
            {papers.map((paper) => (
              <li key={paper.id} className="py-2 first:pt-0 last:pb-0">
                <Link
                  href={`/papers/${encodeURIComponent(paper.id)}`}
                  className="group block"
                >
                  <p className="line-clamp-2 text-[0.82rem] leading-snug font-medium text-ink group-hover:text-seal">
                    {paper.title || "未命名论文"}
                  </p>
                  <p className="mt-0.5 flex flex-wrap items-center gap-x-2 font-mono text-[0.64rem] text-ink-3">
                    {paper.venue && <span className="truncate">{paper.venue}</span>}
                    {paper.publishedYear > 0 && <span>{paper.publishedYear}</span>}
                    {paper.citationCount > 0 && (
                      <span className="tabular-nums">被引 {paper.citationCount}</span>
                    )}
                  </p>
                </Link>
              </li>
            ))}
          </ul>
        </RailPanel>
      ) : null}

      {collections === null ? (
        <RailSkeleton title="精选栏目" rows={2} />
      ) : collections.length > 0 ? (
        <RailPanel title="精选栏目" href="/collections" linkLabel="全部栏目">
          <ul className="flex flex-col divide-y divide-line">
            {collections.map((collection) => (
              <li key={collection.id} className="py-2 first:pt-0 last:pb-0">
                <Link
                  href={`/collections/${encodeURIComponent(collection.slug)}`}
                  className="group block"
                >
                  <p className="line-clamp-1 text-[0.82rem] leading-snug font-medium text-ink group-hover:text-seal">
                    {collection.title}
                  </p>
                  <p className="mt-0.5 font-mono text-[0.64rem] text-ink-3">
                    {collection.curator.name} · {collection.itemCount} 篇
                  </p>
                </Link>
              </li>
            ))}
          </ul>
        </RailPanel>
      ) : null}

      <RailPanel title="排序规则">
        <p className="text-[0.78rem] leading-relaxed text-ink-2">
          关注流按发布时间倒序，不做加权。热门流使用公开公式，规则对所有人一致。
        </p>
        <pre className="mt-2.5 overflow-x-auto rounded-[6px] border border-line bg-surface-2 p-2.5 font-mono text-[0.68rem] leading-relaxed text-ink-2">
{`raw = 1.0·likes + 2.0·comments
    + 3.0·reposts
hot = raw / (hours + 2)^1.5`}
        </pre>
      </RailPanel>

      <RailPanel title="服务状态">
        <div className="flex flex-col gap-2 text-[0.78rem]">
          <StatusRow label="API" ok={health?.status === "ok"} value={health?.status ?? "不可达"} />
          <StatusRow label="数据库" ok={health?.database === "up"} value={health?.database ?? "—"} />
          <StatusRow label="版本" ok value={health?.version ?? "—"} mono />
        </div>
      </RailPanel>
    </div>
  );
}

function RailPanel({
  title,
  href,
  linkLabel,
  children,
}: {
  title: string;
  href?: string;
  linkLabel?: string;
  children: React.ReactNode;
}) {
  return (
    <section className="rounded-[12px] border border-line bg-surface p-4">
      <div className="mb-2.5 flex items-baseline justify-between gap-2">
        <h2 className="font-mono text-[0.66rem] font-semibold tracking-[0.1em] text-ink-3 uppercase">
          {title}
        </h2>
        {href && (
          <Link
            href={href}
            className="font-mono text-[0.64rem] text-ink-3 transition-colors hover:text-seal"
          >
            {linkLabel ?? "更多"} →
          </Link>
        )}
      </div>
      {children}
    </section>
  );
}

/** 加载占位：标题立即出现，内容用灰条，加载完成后高度基本不变。 */
function RailSkeleton({ title, rows }: { title: string; rows: number }) {
  return (
    <section className="rounded-[12px] border border-line bg-surface p-4" aria-hidden>
      <h2 className="mb-2.5 font-mono text-[0.66rem] font-semibold tracking-[0.1em] text-ink-3 uppercase">
        {title}
      </h2>
      <div className="flex flex-col gap-2">
        {Array.from({ length: rows }, (_, i) => (
          <div key={i} className="h-4 animate-pulse rounded-[3px] bg-surface-2" />
        ))}
      </div>
    </section>
  );
}

function StatusRow({
  label,
  ok,
  value,
  mono = false,
}: {
  label: string;
  ok: boolean;
  value: string;
  mono?: boolean;
}) {
  return (
    <div className="flex items-center justify-between gap-3">
      <span className="text-ink-2">{label}</span>
      <span className={`flex items-center gap-1.5 ${mono ? "font-mono" : ""}`}>
        <span className={`size-1.5 rounded-full ${ok ? "bg-good" : "bg-danger"}`} />
        {value}
      </span>
    </div>
  );
}
