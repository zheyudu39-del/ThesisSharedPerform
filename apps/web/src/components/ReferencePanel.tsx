"use client";

import { useCallback, useEffect, useMemo, useState } from "react";
import {
  CITATION_FORMAT_LABEL,
  ENTRY_TYPE_LABEL,
  addReference,
  authorsSummary,
  bibliographyToText,
  deleteReference,
  getBibliography,
  importBibTeX,
  listReferences,
  type BibliographyEntryDTO,
  type CitationFormat,
  type ReferenceDTO,
} from "@/lib/citations";

/**
 * 文档侧栏的「引用」面板（PRD D-06）。
 *
 * 三件事：
 *  1. BibTeX 导入（粘贴文本框，幂等——重复导入只更新不重复）；
 *  2. 条目列表与删除；
 *  3. 格式切换 + 实时预览文末参考文献。
 *
 * 格式切换只重新请求 `/bibliography?format=`，不改库：
 * 引用格式是渲染时的决定，同一份条目三种格式随时可切。
 *
 * 权限：只有 editor 及以上能增删条目；viewer 仍然可以查看与切换格式，
 * 因为参考文献属于正文的一部分（服务端同样只要求读权限）。
 */
export function ReferencePanel({
  documentId,
  canEdit,
}: {
  documentId: string;
  canEdit: boolean;
}) {
  const [references, setReferences] = useState<ReferenceDTO[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  // 导入
  const [bibtex, setBibtex] = useState("");
  const [importing, setImporting] = useState(false);
  const [importNotice, setImportNotice] = useState<string | null>(null);

  // 手工添加（默认折叠，避免挤占列表空间）
  const [showAddForm, setShowAddForm] = useState(false);

  // 格式与预览
  const [format, setFormat] = useState<CitationFormat>("gbt7714");
  const [bibliography, setBibliography] = useState<BibliographyEntryDTO[]>([]);
  const [rendering, setRendering] = useState(false);
  const [copied, setCopied] = useState(false);

  const load = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      setReferences(await listReferences(documentId));
    } catch (err) {
      setError(err instanceof Error ? err.message : "加载引用失败");
    } finally {
      setLoading(false);
    }
  }, [documentId]);

  useEffect(() => {
    void load();
  }, [load]);

  // 格式或条目变化时重新渲染。条目变化后也重渲染，保证预览与列表同步。
  const renderKey = useMemo(
    () => references.map((r) => `${r.id}:${r.citeKey}`).join("|"),
    [references],
  );

  useEffect(() => {
    let cancelled = false;
    async function render() {
      setRendering(true);
      try {
        const result = await getBibliography(documentId, format);
        if (!cancelled) setBibliography(result.entries);
      } catch (err) {
        if (!cancelled) setError(err instanceof Error ? err.message : "生成参考文献失败");
      } finally {
        if (!cancelled) setRendering(false);
      }
    }
    void render();
    return () => {
      cancelled = true;
    };
    // renderKey 参与依赖：条目增删后要重新渲染预览。
  }, [documentId, format, renderKey]);

  async function handleImport() {
    const text = bibtex.trim();
    if (!text) {
      setError("请先粘贴 BibTeX 文本。");
      return;
    }
    setImporting(true);
    setError(null);
    setImportNotice(null);
    try {
      const result = await importBibTeX(documentId, text);
      setBibtex("");
      setImportNotice(`已处理 ${result.imported} 条文献（重复的引用键会更新而不是重复添加）。`);
      await load();
    } catch (err) {
      setError(err instanceof Error ? err.message : "导入失败");
    } finally {
      setImporting(false);
    }
  }

  async function handleDelete(ref: ReferenceDTO) {
    if (!window.confirm(`确定删除引用「${ref.citeKey}」？`)) return;
    setError(null);
    try {
      await deleteReference(documentId, ref.id);
      setReferences((prev) => prev.filter((r) => r.id !== ref.id));
    } catch (err) {
      setError(err instanceof Error ? err.message : "删除失败");
    }
  }

  async function handleAdd(input: {
    citeKey: string;
    entryType: string;
    title: string;
    authors: string;
    year: string;
    container: string;
    doi: string;
    url: string;
  }) {
    setError(null);
    try {
      // 手工输入的作者按「姓, 名; 姓, 名」解析，与 BibTeX 的展示习惯一致。
      const authors = input.authors
        .split(/[;；]/)
        .map((part) => part.trim())
        .filter(Boolean)
        .map((part) => {
          const [family, given] = part.split(/[,，]/);
          return { family: (family ?? "").trim(), given: (given ?? "").trim() };
        })
        .filter((a) => a.family || a.given);
      const year = Number.parseInt(input.year, 10);
      const created = await addReference(documentId, {
        citeKey: input.citeKey.trim(),
        entryType: input.entryType,
        title: input.title.trim(),
        authors,
        year: Number.isFinite(year) ? year : undefined,
        container: input.container.trim() || undefined,
        doi: input.doi.trim() || undefined,
        url: input.url.trim() || undefined,
      });
      setReferences((prev) => {
        // 同 citeKey 是更新语义，替换原条目而不是追加。
        const rest = prev.filter((r) => r.citeKey !== created.citeKey);
        return [...rest, created];
      });
      setShowAddForm(false);
    } catch (err) {
      setError(err instanceof Error ? err.message : "添加失败");
    }
  }

  async function handleCopy() {
    const text = bibliographyToText(bibliography);
    try {
      await navigator.clipboard.writeText(text);
      setCopied(true);
      window.setTimeout(() => setCopied(false), 2000);
    } catch {
      setError("复制失败，请手动选中文本复制。");
    }
  }

  return (
    <section className="rounded-[12px] border border-line bg-surface p-3.5">
      <div className="mb-2.5 flex items-center justify-between">
        <h2 className="font-mono text-[0.68rem] tracking-[0.1em] text-ink-3 uppercase">
          引用 · {references.length}
        </h2>
        {canEdit && (
          <button
            type="button"
            onClick={() => setShowAddForm((v) => !v)}
            className="rounded-[3px] px-2 py-0.5 text-[0.72rem] font-semibold text-seal hover:bg-seal-wash"
          >
            {showAddForm ? "收起" : "+ 手工添加"}
          </button>
        )}
      </div>

      {error && (
        <p className="mb-2 rounded-[6px] border border-danger/40 bg-danger-wash px-3 py-2 text-[0.75rem] text-danger">
          {error}
        </p>
      )}
      {importNotice && (
        <p className="mb-2 rounded-[6px] border border-line-strong bg-surface-2 px-3 py-2 text-[0.75rem] text-ink-2">
          {importNotice}
        </p>
      )}

      {canEdit && showAddForm && (
        <AddReferenceForm onSubmit={handleAdd} onCancel={() => setShowAddForm(false)} />
      )}

      {/* BibTeX 导入 */}
      {canEdit && (
        <div className="mb-3">
          <label className="mb-1 block font-mono text-[0.66rem] tracking-[0.08em] text-ink-3 uppercase">
            BibTeX 导入
          </label>
          <textarea
            value={bibtex}
            onChange={(e) => setBibtex(e.target.value)}
            rows={4}
            placeholder={"@article{key,\n  author = {Last, First},\n  title = {…},\n  year = {2024}\n}"}
            className="w-full resize-y rounded-[6px] border border-line bg-surface px-2.5 py-2 font-mono text-[0.72rem] leading-relaxed text-ink focus:border-seal focus:outline-none"
          />
          <div className="mt-1.5 flex items-center gap-2">
            <button
              type="button"
              onClick={() => void handleImport()}
              disabled={importing || !bibtex.trim()}
              className="rounded-[3px] bg-seal px-2.5 py-1 text-[0.74rem] font-semibold text-white hover:bg-seal-deep disabled:opacity-50"
            >
              {importing ? "导入中…" : "导入"}
            </button>
            <span className="text-[0.68rem] text-ink-3">重复引用键会更新，不会重复添加</span>
          </div>
        </div>
      )}

      {/* 条目列表 */}
      {loading ? (
        <Skeleton />
      ) : references.length === 0 ? (
        <p className="py-3 text-center text-[0.78rem] text-ink-3">还没有引用条目</p>
      ) : (
        <ul className="flex flex-col gap-2">
          {references.map((ref) => (
            <li key={ref.id} className="border-b border-line pb-2 last:border-0 last:pb-0">
              <div className="flex items-start gap-2">
                <div className="min-w-0 flex-1">
                  <p className="text-[0.82rem] leading-snug font-medium">{ref.title}</p>
                  <p className="mt-0.5 text-[0.72rem] text-ink-2">{authorsSummary(ref.authors)}</p>
                  <p className="font-mono text-[0.66rem] text-ink-3">
                    {[
                      ENTRY_TYPE_LABEL[ref.entryType] ?? ref.entryType,
                      ref.container,
                      ref.year > 0 ? String(ref.year) : "年份未知",
                    ]
                      .filter(Boolean)
                      .join(" · ")}
                  </p>
                  <p className="mt-0.5 font-mono text-[0.64rem] text-ink-3">@{ref.citeKey}</p>
                </div>
                {canEdit && (
                  <button
                    type="button"
                    onClick={() => void handleDelete(ref)}
                    className="shrink-0 text-[0.7rem] text-ink-3 hover:text-danger"
                    aria-label={`删除引用 ${ref.citeKey}`}
                  >
                    删除
                  </button>
                )}
              </div>
            </li>
          ))}
        </ul>
      )}

      {/* 格式切换与预览 */}
      <div className="mt-3 border-t border-line pt-3">
        <div className="mb-2 flex flex-wrap items-center gap-1">
          <span className="mr-1 font-mono text-[0.66rem] tracking-[0.08em] text-ink-3 uppercase">
            文末参考文献
          </span>
          {(Object.keys(CITATION_FORMAT_LABEL) as CitationFormat[]).map((value) => (
            <button
              key={value}
              type="button"
              onClick={() => setFormat(value)}
              className={`rounded-[3px] border px-2 py-0.5 text-[0.7rem] ${
                format === value
                  ? "border-seal bg-seal-wash font-semibold text-seal"
                  : "border-line-strong text-ink-2 hover:border-ink-3 hover:text-ink"
              }`}
            >
              {CITATION_FORMAT_LABEL[value]}
            </button>
          ))}
          {bibliography.length > 0 && (
            <button
              type="button"
              onClick={() => void handleCopy()}
              className="ml-auto text-[0.7rem] text-ink-3 hover:text-seal"
            >
              {copied ? "已复制" : "复制"}
            </button>
          )}
        </div>

        {rendering ? (
          <Skeleton />
        ) : bibliography.length === 0 ? (
          <p className="py-2 text-center text-[0.74rem] text-ink-3">
            导入或添加条目后，这里会生成 {CITATION_FORMAT_LABEL[format]} 格式的参考文献
          </p>
        ) : (
          <ol className="flex flex-col gap-1.5 text-[0.74rem] leading-relaxed text-ink-2">
            {bibliography.map((entry) => (
              <li key={entry.citeKey} className="break-words">
                {entry.text}
              </li>
            ))}
          </ol>
        )}
      </div>
    </section>
  );
}

/** 手工添加表单：字段与后端 AddReferenceInput 对应，只保留常用项。 */
function AddReferenceForm({
  onSubmit,
  onCancel,
}: {
  onSubmit: (input: {
    citeKey: string;
    entryType: string;
    title: string;
    authors: string;
    year: string;
    container: string;
    doi: string;
    url: string;
  }) => Promise<void>;
  onCancel: () => void;
}) {
  const [form, setForm] = useState({
    citeKey: "",
    entryType: "article",
    title: "",
    authors: "",
    year: "",
    container: "",
    doi: "",
    url: "",
  });
  const [submitting, setSubmitting] = useState(false);

  function update(key: keyof typeof form, value: string) {
    setForm((prev) => ({ ...prev, [key]: value }));
  }

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    if (!form.citeKey.trim() || !form.title.trim()) return;
    setSubmitting(true);
    try {
      await onSubmit(form);
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <form onSubmit={(e) => void submit(e)} className="mb-3 flex flex-col gap-1.5 rounded-[6px] border border-line bg-surface-2 p-2.5">
      <div className="grid grid-cols-2 gap-1.5">
        <Field label="引用键" value={form.citeKey} onChange={(v) => update("citeKey", v)} placeholder="key2024" />
        <label className="flex flex-col gap-0.5">
          <span className="font-mono text-[0.62rem] text-ink-3">类型</span>
          <select
            value={form.entryType}
            onChange={(e) => update("entryType", e.target.value)}
            className="rounded-[4px] border border-line bg-surface px-1.5 py-1 text-[0.74rem] text-ink focus:border-seal focus:outline-none"
          >
            {Object.entries(ENTRY_TYPE_LABEL).map(([value, label]) => (
              <option key={value} value={value}>
                {label}
              </option>
            ))}
          </select>
        </label>
      </div>
      <Field label="题名" value={form.title} onChange={(v) => update("title", v)} />
      <Field
        label="作者（姓, 名; 姓, 名）"
        value={form.authors}
        onChange={(v) => update("authors", v)}
        placeholder="Vaswani, Ashish; Shazeer, Noam"
      />
      <div className="grid grid-cols-2 gap-1.5">
        <Field label="年份" value={form.year} onChange={(v) => update("year", v)} placeholder="2024" />
        <Field label="期刊 / 会议" value={form.container} onChange={(v) => update("container", v)} />
      </div>
      <div className="grid grid-cols-2 gap-1.5">
        <Field label="DOI" value={form.doi} onChange={(v) => update("doi", v)} />
        <Field label="URL" value={form.url} onChange={(v) => update("url", v)} />
      </div>
      <div className="mt-0.5 flex items-center gap-2">
        <button
          type="submit"
          disabled={submitting || !form.citeKey.trim() || !form.title.trim()}
          className="rounded-[3px] bg-seal px-2.5 py-1 text-[0.74rem] font-semibold text-white hover:bg-seal-deep disabled:opacity-50"
        >
          {submitting ? "添加中…" : "添加"}
        </button>
        <button
          type="button"
          onClick={onCancel}
          className="rounded-[3px] border border-line-strong px-2.5 py-1 text-[0.74rem] text-ink-2 hover:border-ink-3"
        >
          取消
        </button>
      </div>
    </form>
  );
}

function Field({
  label,
  value,
  onChange,
  placeholder,
}: {
  label: string;
  value: string;
  onChange: (value: string) => void;
  placeholder?: string;
}) {
  return (
    <label className="flex flex-col gap-0.5">
      <span className="font-mono text-[0.62rem] text-ink-3">{label}</span>
      <input
        value={value}
        onChange={(e) => onChange(e.target.value)}
        placeholder={placeholder}
        className="rounded-[4px] border border-line bg-surface px-1.5 py-1 text-[0.74rem] text-ink focus:border-seal focus:outline-none"
      />
    </label>
  );
}

function Skeleton() {
  return (
    <div className="flex flex-col gap-2" aria-hidden>
      {[0, 1].map((i) => (
        <div key={i} className="h-10 animate-pulse rounded-[6px] bg-surface-2" />
      ))}
    </div>
  );
}
