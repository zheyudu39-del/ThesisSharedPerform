"use client";

import Link from "next/link";
import { useCallback, useEffect, useState, type FormEvent } from "react";
import { useAuth } from "@/components/AuthProvider";
import { RequireAuth } from "@/components/RequireAuth";
import { ManualPaperForm } from "@/components/ManualPaperForm";
import { AuthError } from "@/lib/auth";
import { listPapers, resolvePaper, type PaperDTO, type ResolveResult } from "@/lib/papers";

/**
 * 论文库工作区（客户端）。
 *
 * 解析表单、论文列表与登录态都依赖浏览器，因此整块保留客户端；
 * 页面外壳与 metadata 在 `page.tsx`（Server Component）里。
 */
export function PapersWorkspace() {
  return (
    <RequireAuth>
      <PapersPanel />
    </RequireAuth>
  );
}

function PapersPanel() {
  const { user } = useAuth();
  const [input, setInput] = useState("");
  const [resolving, setResolving] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [result, setResult] = useState<ResolveResult | null>(null);
  const [papers, setPapers] = useState<PaperDTO[]>([]);
  const [loadingList, setLoadingList] = useState(true);

  const refreshList = useCallback(async () => {
    setLoadingList(true);
    try {
      const data = await listPapers(20, 0);
      setPapers(data.papers ?? []);
    } catch {
      // 列表失败不阻塞解析功能，静默处理。
      setPapers([]);
    } finally {
      setLoadingList(false);
    }
  }, []);

  useEffect(() => {
    void refreshList();
  }, [refreshList]);

  async function handleResolve(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (resolving || !input.trim()) return;

    setError(null);
    setResult(null);
    setResolving(true);
    try {
      const resolved = await resolvePaper(input);
      setResult(resolved);
      setInput("");
      await refreshList();
    } catch (err) {
      setError(err instanceof AuthError ? err.message : "解析失败，请稍后重试。");
    } finally {
      setResolving(false);
    }
  }

  return (
    <main className="mx-auto w-full max-w-page px-5 py-10">
      <div className="mb-8 border-b border-line pb-7">
        <p className="mb-3 font-mono text-[0.72rem] tracking-[0.16em] text-ink-3 uppercase">
          论文库 · Papers
        </p>
        <h1 className="font-display text-[clamp(1.7rem,4vw,2.3rem)] leading-[1.08] font-semibold tracking-[-0.03em] text-balance">
          粘贴 DOI 或 arXiv 编号
        </h1>
        <p className="mt-3 max-w-[58ch] text-[0.95rem] leading-relaxed text-ink-2">
          自动抓取标题、作者、摘要与出处。同一篇论文只会入库一次，重复解析会直接返回已有记录。
        </p>
      </div>

      {/* 解析表单 */}
      <form onSubmit={handleResolve} className="mb-8 max-w-3xl">
        <div className="flex flex-col gap-2.5 sm:flex-row">
          <label htmlFor="paper-input" className="sr-only">
            DOI、arXiv 编号或论文链接
          </label>
          <input
            id="paper-input"
            value={input}
            onChange={(e) => setInput(e.target.value)}
            disabled={resolving}
            placeholder="10.1145/3292500.3330701 或 2405.13412 或 https://arxiv.org/abs/2405.13412"
            className="min-w-0 flex-1 rounded-[6px] border border-line-strong bg-surface px-3.5 py-2.5 font-mono text-[0.88rem] text-ink placeholder:text-ink-3 focus:border-seal focus:outline-none disabled:opacity-60"
          />
          <button
            type="submit"
            disabled={resolving || !input.trim()}
            aria-busy={resolving}
            className="inline-flex items-center justify-center gap-2 rounded-[6px] bg-seal px-5 py-2.5 text-[0.9rem] font-semibold text-white shadow-[var(--shadow-1)] transition-colors hover:bg-seal-deep disabled:cursor-not-allowed disabled:opacity-50"
          >
            {resolving && (
              <span
                aria-hidden
                className="size-3.5 animate-spin rounded-full border-2 border-white/35 border-t-white"
              />
            )}
            {resolving ? "解析中…" : "解析"}
          </button>
        </div>
        <p className="mt-2.5 font-mono text-[0.72rem] text-ink-3">
          支持 DOI · arXiv · OpenReview · doi.org / arxiv.org 链接
        </p>
      </form>

      {error && (
        <div
          role="alert"
          className="mb-6 flex items-start gap-2.5 rounded-[6px] border border-danger/45 bg-danger-wash px-3.5 py-3 text-[0.85rem] leading-relaxed text-danger"
        >
          <span aria-hidden className="mt-0.5 font-mono font-semibold">
            !
          </span>
          <span>{error}</span>
        </div>
      )}

      {result && <ResolveCard result={result} />}

      <ManualPaperForm
        onCreated={(r) => {
          setResult(r);
          // 新建成功后刷新列表，让刚补录的论文立刻出现在下方。
          void refreshList();
        }}
      />

      {/* 论文列表 */}
      <section className="mt-10">
        <h2 className="mb-4 font-mono text-[0.7rem] font-semibold tracking-[0.1em] text-ink-3 uppercase">
          最近入库 · {papers.length} 篇
        </h2>

        {loadingList ? (
          <div className="flex flex-col gap-3" aria-hidden>
            {[0, 1].map((i) => (
              <div key={i} className="h-24 animate-pulse rounded-[12px] bg-surface-2" />
            ))}
          </div>
        ) : papers.length === 0 ? (
          <p className="rounded-[12px] border border-dashed border-line-strong bg-surface px-5 py-8 text-center text-[0.88rem] text-ink-2">
            {user ? "还没有论文。粘贴一个 DOI 或 arXiv 编号开始。" : "登录后可以添加论文。"}
          </p>
        ) : (
          <ul className="grid gap-3 sm:grid-cols-2 xl:grid-cols-3">
            {papers.map((p) => (
              <li key={p.id}>
                <PaperListItem paper={p} />
              </li>
            ))}
          </ul>
        )}
      </section>
    </main>
  );
}

function ResolveCard({ result }: { result: ResolveResult }) {
  const { paper, existing } = result;
  return (
    <div className="mb-6 overflow-hidden rounded-[12px] border border-line bg-surface shadow-[var(--shadow-1)]">
      <div className="flex items-center gap-2 border-b border-line bg-surface-2 px-4 py-2">
        <span
          className={`inline-flex items-center gap-1.5 rounded-full px-2 py-0.5 text-[0.72rem] font-semibold ${
            existing ? "bg-warn-wash text-warn" : "bg-good-wash text-good"
          }`}
        >
          {existing ? "已存在 · 未新建" : "解析成功 · 已入库"}
        </span>
        {paper.metaSource && (
          <span className="ml-auto font-mono text-[0.68rem] text-ink-3">
            来源 {paper.metaSource}
          </span>
        )}
      </div>
      <div className="p-4">
        <h3 className="mb-1.5 font-display text-[1.1rem] leading-snug font-semibold text-balance">
          {paper.title}
        </h3>
        <p className="mb-2 text-[0.82rem] text-ink-2">
          {paper.authors.map((a) => a.name).join(", ") || "未知作者"}
        </p>
        <p className="mb-3 flex flex-wrap items-center gap-x-3 gap-y-1 font-mono text-[0.7rem] text-ink-3">
          {paper.doi && <span>{paper.doi}</span>}
          {paper.arxivId && <span>arXiv:{paper.arxivId}</span>}
          {paper.venue && <span>{paper.venue}</span>}
          {paper.publishedYear > 0 && <span>{paper.publishedYear}</span>}
          {paper.citationCount > 0 && <span>引用 {paper.citationCount}</span>}
        </p>
        {paper.abstract && (
          <p className="line-clamp-4 text-[0.84rem] leading-relaxed text-ink-2">{paper.abstract}</p>
        )}
      </div>
    </div>
  );
}

function PaperListItem({ paper }: { paper: PaperDTO }) {
  return (
    <Link
      href={`/papers/${paper.id}`}
      className="flex h-full flex-col rounded-[12px] border border-line bg-surface p-4 transition-colors hover:border-line-strong"
    >
      <h3 className="mb-1 font-display text-[0.98rem] leading-snug font-semibold">{paper.title}</h3>
      <p className="mb-2 text-[0.8rem] text-ink-2">
        {paper.authors
          .slice(0, 4)
          .map((a) => a.name)
          .join(", ")}
        {paper.authors.length > 4 && " 等"}
      </p>
      <p className="mt-auto flex flex-wrap items-center gap-x-3 gap-y-1 font-mono text-[0.68rem] text-ink-3">
        {paper.doi && <span>{paper.doi}</span>}
        {paper.arxivId && <span>arXiv:{paper.arxivId}</span>}
        {paper.venue && <span>{paper.venue}</span>}
        {paper.publishedYear > 0 && <span>{paper.publishedYear}</span>}
      </p>
    </Link>
  );
}
