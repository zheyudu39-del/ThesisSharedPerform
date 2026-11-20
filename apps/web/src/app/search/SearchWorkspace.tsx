"use client";

import Link from "next/link";
import { useCallback, useEffect, useRef, useState, type FormEvent } from "react";
import { ApiError } from "@/lib/http";
import {
  SEARCH_TYPES,
  SEARCH_TYPE_LABEL,
  search,
  searchItemHref,
  type SearchEntityType,
  type SearchItem,
} from "@/lib/search";

/**
 * 搜索工作区（客户端）。
 *
 * - 输入框提交或点选类型筛选时发请求；
 * - 结果按类型分组展示标题 + 服务端生成的高亮片段；
 * - 只展示可点进的类型（论文 / 帖子），其余类型没有站内路由，仅展示摘要。
 */
const DEBOUNCE_MS = 300;

export function SearchWorkspace({ initialQuery = "" }: { initialQuery?: string }) {
  const [input, setInput] = useState(initialQuery);
  const [query, setQuery] = useState(initialQuery);
  const [types, setTypes] = useState<SearchEntityType[]>([]);
  const [items, setItems] = useState<SearchItem[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [searched, setSearched] = useState(false);

  const mounted = useRef(true);
  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
    };
  }, []);

  // 输入防抖：停止输入 300ms 后自动检索；与提交按钮共用同一份 query 状态。
  useEffect(() => {
    const trimmed = input.trim();
    const timer = window.setTimeout(() => setQuery(trimmed), DEBOUNCE_MS);
    return () => window.clearTimeout(timer);
  }, [input]);

  const run = useCallback(async (text: string, selected: SearchEntityType[]) => {
    if (!text) {
      setItems([]);
      setError(null);
      setSearched(false);
      return;
    }
    setLoading(true);
    setError(null);
    try {
      const result = await search(text, selected, 30);
      if (!mounted.current) return;
      setItems(result.items);
      setSearched(true);
    } catch (err) {
      if (!mounted.current) return;
      setItems([]);
      setSearched(true);
      setError(
        err instanceof ApiError ? err.message : "搜索失败，请稍后重试。",
      );
    } finally {
      if (mounted.current) setLoading(false);
    }
  }, []);

  useEffect(() => {
    void run(query, types);
  }, [query, types, run]);

  function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setQuery(input.trim());
  }

  function toggleType(type: SearchEntityType) {
    setTypes((prev) =>
      prev.includes(type) ? prev.filter((t) => t !== type) : [...prev, type],
    );
  }

  const grouped = groupByType(items);

  return (
    <main className="mx-auto w-full max-w-4xl px-5 py-10">
      <div className="mb-7 border-b border-line pb-7">
        <p className="mb-3 font-mono text-[0.72rem] tracking-[0.16em] text-ink-3 uppercase">
          检索 · Search
        </p>
        <h1 className="font-display text-[clamp(1.7rem,4vw,2.3rem)] leading-[1.08] font-semibold tracking-[-0.03em] text-balance">
          在论文与讨论中找到你要的
        </h1>
        <p className="mt-3 max-w-[58ch] text-[0.95rem] leading-relaxed text-ink-2">
          一次检索覆盖论文、帖子、群组与用户。输入关键词即可，命中片段会高亮显示。
        </p>
      </div>

      <form onSubmit={handleSubmit} role="search" className="flex flex-col gap-3">
        <div className="flex items-stretch gap-2">
          <input
            type="search"
            value={input}
            onChange={(e) => setInput(e.target.value)}
            placeholder="输入关键词，例如：图神经网络"
            aria-label="搜索关键词"
            className="min-w-0 flex-1 rounded-[6px] border border-line-strong bg-surface px-3.5 py-2.5 text-[0.92rem] text-ink placeholder:text-ink-3 focus:border-ink-3 focus:outline-none"
          />
          <button
            type="submit"
            className="shrink-0 rounded-[6px] bg-seal px-4 py-2.5 text-[0.88rem] font-semibold text-white transition-colors hover:bg-seal-deep"
          >
            搜索
          </button>
        </div>

        <div className="flex flex-wrap items-center gap-1.5">
          <span className="mr-1 font-mono text-[0.7rem] tracking-[0.12em] text-ink-3 uppercase">
            类型
          </span>
          {SEARCH_TYPES.map((option) => {
            const active = types.includes(option.value);
            return (
              <button
                key={option.value}
                type="button"
                onClick={() => toggleType(option.value)}
                aria-pressed={active}
                className={`rounded-[3px] border px-2.5 py-1 text-[0.78rem] transition-colors ${
                  active
                    ? "border-seal bg-seal-wash text-seal-deep"
                    : "border-line-strong text-ink-2 hover:border-ink-3 hover:text-ink"
                }`}
              >
                {option.label}
              </button>
            );
          })}
          {types.length > 0 && (
            <button
              type="button"
              onClick={() => setTypes([])}
              className="ml-1 text-[0.76rem] text-ink-3 underline underline-offset-2 hover:text-ink"
            >
              清除筛选
            </button>
          )}
        </div>
      </form>

      <div className="mt-8">
        {error && (
          <div
            role="alert"
            className="mb-5 flex items-start gap-2.5 rounded-[6px] border border-danger/45 bg-danger-wash px-3.5 py-3 text-[0.85rem] leading-relaxed text-danger"
          >
            <span aria-hidden className="mt-0.5 font-mono font-semibold">
              !
            </span>
            <span className="flex-1">{error}</span>
            <button
              type="button"
              onClick={() => void run(query, types)}
              className="shrink-0 font-semibold underline underline-offset-2"
            >
              重试
            </button>
          </div>
        )}

        {loading ? (
          <div className="flex flex-col gap-3" aria-hidden>
            {[0, 1, 2].map((i) => (
              <div key={i} className="h-[84px] animate-pulse rounded-[12px] bg-surface-2" />
            ))}
          </div>
        ) : !query ? (
          <p className="rounded-[12px] border border-dashed border-line-strong bg-surface px-5 py-12 text-center text-[0.88rem] text-ink-2">
            输入关键词开始检索。可按类型筛选，只看论文或只看帖子。
          </p>
        ) : searched && items.length === 0 && !error ? (
          <div className="rounded-[12px] border border-dashed border-line-strong bg-surface px-5 py-12 text-center">
            <p className="font-display text-[1.1rem] font-semibold">没有找到相关内容</p>
            <p className="mx-auto mt-2 max-w-[40ch] text-[0.88rem] leading-relaxed text-ink-2">
              换个关键词试试，或清除类型筛选扩大范围。
            </p>
          </div>
        ) : (
          <div className="flex flex-col gap-7">
            <p className="font-mono text-[0.72rem] text-ink-3">
              共 {items.length} 条结果
            </p>
            {SEARCH_TYPES.map(({ value }) => {
              const list = grouped.get(value);
              if (!list || list.length === 0) return null;
              return (
                <section key={value} aria-labelledby={`search-${value}`}>
                  <h2
                    id={`search-${value}`}
                    className="mb-2.5 font-mono text-[0.72rem] tracking-[0.14em] text-ink-3 uppercase"
                  >
                    {SEARCH_TYPE_LABEL[value]} · {list.length}
                  </h2>
                  <ul className="flex flex-col gap-2">
                    {list.map((item) => (
                      <li key={`${item.entityType}-${item.entityId}`}>
                        <ResultCard item={item} />
                      </li>
                    ))}
                  </ul>
                </section>
              );
            })}
          </div>
        )}
      </div>
    </main>
  );
}

function ResultCard({ item }: { item: SearchItem }) {
  const href = searchItemHref(item);
  const title = item.title || "（无标题）";

  const inner = (
    <>
      <div className="flex items-start justify-between gap-3">
        <h3 className="min-w-0 font-display text-[1.02rem] leading-snug font-semibold text-ink">
          {title}
        </h3>
        <span className="mt-0.5 shrink-0 rounded-[3px] border border-line-strong px-1.5 py-0.5 font-mono text-[0.64rem] text-ink-3">
          {SEARCH_TYPE_LABEL[item.entityType]}
        </span>
      </div>
      {item.snippet && (
        // 片段由服务端转义并只插入 <mark>，因此可安全注入（见 lib/search.ts 说明）。
        <p
          className="mt-1.5 line-clamp-3 text-[0.86rem] leading-relaxed text-ink-2 [&_mark]:bg-seal-wash [&_mark]:px-0.5 [&_mark]:font-semibold [&_mark]:text-seal-deep"
          dangerouslySetInnerHTML={{ __html: item.snippet }}
        />
      )}
    </>
  );

  const className =
    "block rounded-[12px] border border-line bg-surface px-4 py-3.5 transition-colors";

  if (href) {
    return (
      <Link href={href} className={`${className} hover:border-line-strong hover:bg-surface-2`}>
        {inner}
      </Link>
    );
  }
  return <div className={className}>{inner}</div>;
}

function groupByType(items: SearchItem[]): Map<SearchEntityType, SearchItem[]> {
  const map = new Map<SearchEntityType, SearchItem[]>();
  for (const item of items) {
    const list = map.get(item.entityType) ?? [];
    list.push(item);
    map.set(item.entityType, list);
  }
  return map;
}
