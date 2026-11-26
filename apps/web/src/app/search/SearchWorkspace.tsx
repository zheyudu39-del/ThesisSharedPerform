"use client";

import Link from "next/link";
import { useRouter, useSearchParams } from "next/navigation";
import { useCallback, useEffect, useRef, useState, type FormEvent } from "react";
import { ApiError, apiFetch } from "@/lib/http";
import {
  SEARCH_TYPES,
  SEARCH_TYPE_LABEL,
  searchItemHref,
  type SearchEntityType,
  type SearchItem,
  type SearchResult,
} from "@/lib/search";

/**
 * 搜索工作区（客户端）。
 *
 * - 输入框提交或调整任意筛选时发请求；
 * - 筛选条件（类型、时间范围、标签、作者）全部写进 URL：
 *   刷新、分享链接、浏览器前进后退都能还原同一组结果；
 * - 结果按类型分组展示标题 + 服务端生成的高亮片段；
 * - 只展示可点进的类型（论文 / 帖子），其余类型没有站内路由，仅展示摘要。
 *
 * 为什么用 router.replace 而不是 push：每次改筛选都是一次「原地重查」，
 * 用 replace 避免把每一次按键都塞进历史记录，用户点后退时回到进入搜索页之前。
 */
const DEBOUNCE_MS = 300;

/** 一次检索的完整筛选条件，与 URL 查询参数一一对应。 */
type Filters = {
  types: SearchEntityType[];
  from: string;
  to: string;
  tag: string;
  author: string;
};

const EMPTY_FILTERS: Filters = { types: [], from: "", to: "", tag: "", author: "" };

/**
 * 从 URL 还原筛选条件。
 *
 * 非法类型直接丢弃（URL 可能被手工改坏），而不是让整页报错；
 * 后端对非法时间格式仍会返回 422，这里不做重复校验，保持单一事实来源。
 */
function readFilters(params: URLSearchParams): Filters {
  const rawTypes = (params.get("type") ?? "")
    .split(",")
    .map((t) => t.trim())
    .filter((t): t is SearchEntityType => SEARCH_TYPES.some((o) => o.value === t));
  return {
    types: Array.from(new Set(rawTypes)),
    from: params.get("from") ?? "",
    to: params.get("to") ?? "",
    tag: params.get("tag") ?? "",
    author: params.get("author") ?? "",
  };
}

/** 把筛选条件写回 URL 查询串；空值不写，保持链接干净。 */
function buildQueryString(query: string, filters: Filters): string {
  const params = new URLSearchParams();
  if (query) params.set("q", query);
  if (filters.types.length > 0) params.set("type", filters.types.join(","));
  if (filters.from) params.set("from", filters.from);
  if (filters.to) params.set("to", filters.to);
  if (filters.tag) params.set("tag", filters.tag);
  if (filters.author) params.set("author", filters.author);
  return params.toString();
}

export function SearchWorkspace({ initialQuery = "" }: { initialQuery?: string }) {
  const router = useRouter();
  const searchParams = useSearchParams();

  // 初始状态取自 URL：服务端已把 q 解析成 initialQuery，其余筛选在客户端读取。
  const [input, setInput] = useState(initialQuery || searchParams.get("q") || "");
  const [query, setQuery] = useState(initialQuery || searchParams.get("q") || "");
  const [filters, setFilters] = useState<Filters>(() => readFilters(searchParams));
  // 标签 / 作者是自由文本，输入时先落在本地，防抖后再并入 filters 触发检索，
  // 避免每敲一个字就发一次请求、也避免 URL 被按键刷屏。
  const [tagInput, setTagInput] = useState(filters.tag);
  const [authorInput, setAuthorInput] = useState(filters.author);
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

  // 标签 / 作者同样防抖：只有值真正变化时才写回 filters，避免无谓的 URL 更新。
  useEffect(() => {
    const trimmed = tagInput.trim();
    const timer = window.setTimeout(() => {
      setFilters((prev) => (prev.tag === trimmed ? prev : { ...prev, tag: trimmed }));
    }, DEBOUNCE_MS);
    return () => window.clearTimeout(timer);
  }, [tagInput]);

  useEffect(() => {
    const trimmed = authorInput.trim();
    const timer = window.setTimeout(() => {
      setFilters((prev) => (prev.author === trimmed ? prev : { ...prev, author: trimmed }));
    }, DEBOUNCE_MS);
    return () => window.clearTimeout(timer);
  }, [authorInput]);

  // 把当前条件同步到 URL。只在条件真正变化时 replace，避免无意义的导航。
  useEffect(() => {
    const next = buildQueryString(query, filters);
    const current = searchParams.toString();
    if (next !== current) {
      router.replace(next ? `/search?${next}` : "/search", { scroll: false });
    }
    // searchParams 不放进依赖：它由 replace 更新，放进来会与上面的判断互相触发。
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [query, filters, router]);

  const run = useCallback(async (text: string, active: Filters) => {
    if (!text) {
      setItems([]);
      setError(null);
      setSearched(false);
      return;
    }
    setLoading(true);
    setError(null);
    try {
      const result = await searchWithFilters(text, active, 30);
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
    void run(query, filters);
  }, [query, filters, run]);

  function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setQuery(input.trim());
  }

  function toggleType(type: SearchEntityType) {
    setFilters((prev) => ({
      ...prev,
      types: prev.types.includes(type)
        ? prev.types.filter((t) => t !== type)
        : [...prev.types, type],
    }));
  }

  function patchFilters(partial: Partial<Filters>) {
    setFilters((prev) => ({ ...prev, ...partial }));
  }

  const hasFilters =
    filters.types.length > 0 ||
    filters.from !== "" ||
    filters.to !== "" ||
    filters.tag !== "" ||
    filters.author !== "";

  function clearFilters() {
    setFilters(EMPTY_FILTERS);
    setTagInput("");
    setAuthorInput("");
  }

  const grouped = groupByType(items);

  return (
    <main className="mx-auto w-full max-w-page px-5 py-10">
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
            const active = filters.types.includes(option.value);
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
        </div>

        {/* 时间 / 标签 / 作者：日期用原生 date 输入（输出 YYYY-MM-DD，后端直接接受）。 */}
        <div className="flex flex-wrap items-end gap-3 rounded-[8px] border border-line bg-surface-2/60 px-3 py-3">
          <label className="flex flex-col gap-1 text-[0.72rem] text-ink-3">
            <span className="font-mono tracking-[0.1em] uppercase">起始日期</span>
            <input
              type="date"
              value={filters.from}
              onChange={(e) => patchFilters({ from: e.target.value })}
              className="rounded-[4px] border border-line-strong bg-surface px-2 py-1.5 text-[0.82rem] text-ink focus:border-ink-3 focus:outline-none"
            />
          </label>
          <label className="flex flex-col gap-1 text-[0.72rem] text-ink-3">
            <span className="font-mono tracking-[0.1em] uppercase">结束日期</span>
            <input
              type="date"
              value={filters.to}
              onChange={(e) => patchFilters({ to: e.target.value })}
              className="rounded-[4px] border border-line-strong bg-surface px-2 py-1.5 text-[0.82rem] text-ink focus:border-ink-3 focus:outline-none"
            />
          </label>
          <label className="flex min-w-[9rem] flex-1 flex-col gap-1 text-[0.72rem] text-ink-3">
            <span className="font-mono tracking-[0.1em] uppercase">标签</span>
            <input
              type="text"
              value={tagInput}
              onChange={(e) => setTagInput(e.target.value)}
              placeholder="例如：机器学习"
              className="rounded-[4px] border border-line-strong bg-surface px-2 py-1.5 text-[0.82rem] text-ink placeholder:text-ink-3 focus:border-ink-3 focus:outline-none"
            />
          </label>
          <label className="flex min-w-[9rem] flex-1 flex-col gap-1 text-[0.72rem] text-ink-3">
            <span className="font-mono tracking-[0.1em] uppercase">作者</span>
            <input
              type="text"
              value={authorInput}
              onChange={(e) => setAuthorInput(e.target.value)}
              placeholder="@handle"
              className="rounded-[4px] border border-line-strong bg-surface px-2 py-1.5 text-[0.82rem] text-ink placeholder:text-ink-3 focus:border-ink-3 focus:outline-none"
            />
          </label>
          {hasFilters && (
            <button
              type="button"
              onClick={clearFilters}
              className="mb-0.5 text-[0.76rem] text-ink-3 underline underline-offset-2 hover:text-ink"
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
              onClick={() => void run(query, filters)}
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
            输入关键词开始检索。可按类型、时间、标签与作者筛选。
          </p>
        ) : searched && items.length === 0 && !error ? (
          <div className="rounded-[12px] border border-dashed border-line-strong bg-surface px-5 py-12 text-center">
            <p className="font-display text-[1.1rem] font-semibold">没有找到相关内容</p>
            <p className="mx-auto mt-2 max-w-[40ch] text-[0.88rem] leading-relaxed text-ink-2">
              {hasFilters
                ? "试试放宽筛选条件：换一个时间范围、去掉标签或作者限制，或清除类型筛选。"
                : "换个关键词试试，或清除类型筛选扩大范围。"}
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
                  <ul className="grid gap-2 lg:grid-cols-2">
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

/**
 * 带筛选条件的检索。
 *
 * 没有扩展 lib/search.ts 的 search()：那个函数被其它页面复用，
 * 筛选参数只有本工作区需要；在这里直接拼请求可以避免改动共享契约。
 * 响应解析规则与 search() 保持一致（entityType 必须是四种已知类型）。
 */
async function searchWithFilters(
  text: string,
  filters: Filters,
  limit: number,
): Promise<SearchResult> {
  const params = new URLSearchParams({
    q: text,
    limit: String(Math.min(Math.max(limit, 1), 50)),
  });
  if (filters.types.length > 0) params.set("type", filters.types.join(","));
  if (filters.from) params.set("from", filters.from);
  if (filters.to) params.set("to", filters.to);
  if (filters.tag) params.set("tag", filters.tag);
  if (filters.author) params.set("author", filters.author);

  const body = await apiFetch<unknown>(`/search?${params.toString()}`);
  if (!isRecord(body) || !Array.isArray(body.items)) {
    throw new ApiError("invalid_response", "服务器返回了无法识别的响应。", 0);
  }
  const items = body.items
    .map(normalizeItem)
    .filter((item): item is SearchItem => item !== null);
  return {
    items,
    total: typeof body.total === "number" && Number.isFinite(body.total) ? body.total : items.length,
  };
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function normalizeItem(value: unknown): SearchItem | null {
  if (!isRecord(value) || typeof value.entityId !== "string") return null;
  const entityType = SEARCH_TYPES.find((o) => o.value === value.entityType)?.value;
  if (!entityType) return null;
  return {
    entityType,
    entityId: value.entityId,
    title: typeof value.title === "string" ? value.title : "",
    snippet: typeof value.snippet === "string" ? value.snippet : "",
    rank: typeof value.rank === "number" && Number.isFinite(value.rank) ? value.rank : 0,
    // 用户取 handle、群组取 slug，服务端已 JOIN 回源表填好；
    // 论文与帖子没有这个字段，留空即可。
    slug: typeof value.slug === "string" ? value.slug : "",
  };
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
    "block h-full rounded-[12px] border border-line bg-surface px-4 py-3.5 transition-colors";

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
