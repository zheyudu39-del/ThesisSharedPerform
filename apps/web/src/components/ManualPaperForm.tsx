"use client";

import { useState, type FormEvent } from "react";
import { createManualPaper, type ResolveResult } from "@/lib/papers";

/**
 * 论文手动补录表单（PRD P-04）。
 *
 * 适用场景：没有 DOI / arXiv 编号的论文——专著、技术报告、内部文档、
 * 尚未上线的预印本。在此之前这类论文完全建不了，`SourceManual`
 * 只是代码里一个从未被使用的常量。
 *
 * PRD 的必填项是「标题 + 至少一位作者」，其余都可留空。作者按行填写，
 * 每行「姓名」或「姓名 | 机构」，比逐个加输入框更省事。
 */

export function ManualPaperForm({ onCreated }: { onCreated?: (result: ResolveResult) => void }) {
  const [open, setOpen] = useState(false);
  const [title, setTitle] = useState("");
  const [authorsText, setAuthorsText] = useState("");
  const [abstract, setAbstract] = useState("");
  const [venue, setVenue] = useState("");
  const [year, setYear] = useState("");
  const [doi, setDoi] = useState("");
  const [arxivId, setArxivId] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);

  function parseAuthors(raw: string) {
    return raw
      .split("\n")
      .map((line) => line.trim())
      .filter(Boolean)
      .map((line) => {
        const [name, ...rest] = line.split("|");
        return { name: name.trim(), affiliation: rest.join("|").trim() };
      })
      .filter((a) => a.name.length > 0);
  }

  async function handleSubmit(e: FormEvent) {
    e.preventDefault();
    setError(null);
    setNotice(null);

    const authors = parseAuthors(authorsText);
    if (!title.trim()) {
      setError("请填写标题。");
      return;
    }
    if (authors.length === 0) {
      setError("至少填写一位作者。");
      return;
    }
    const publishedYear = year.trim() ? Number(year) : 0;
    if (year.trim() && (!Number.isInteger(publishedYear) || publishedYear < 1000 || publishedYear > 2100)) {
      setError("年份格式不正确，请填 4 位年份。");
      return;
    }

    setSubmitting(true);
    try {
      const result = await createManualPaper({
        title: title.trim(),
        authors,
        abstract: abstract.trim() || undefined,
        venue: venue.trim() || undefined,
        publishedYear: publishedYear || undefined,
        doi: doi.trim() || undefined,
        arxivId: arxivId.trim() || undefined,
      });
      setNotice(
        result.existing
          ? "库中已有这篇论文，已为你打开它的记录。"
          : "论文已创建。",
      );
      // 成功后清空表单但保持展开：用户可能连续补录多篇。
      setTitle("");
      setAuthorsText("");
      setAbstract("");
      setVenue("");
      setYear("");
      setDoi("");
      setArxivId("");
      onCreated?.(result);
    } catch (err) {
      setError(err instanceof Error ? err.message : "补录失败，请稍后重试。");
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <div className="mb-8">
      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        aria-expanded={open}
        className="text-[0.82rem] text-ink-2 underline decoration-line-strong underline-offset-4 hover:text-seal"
      >
        {open ? "收起手动补录" : "没有 DOI？手动补录一篇论文"}
      </button>

      {open && (
        <form
          onSubmit={handleSubmit}
          className="mt-3 rounded-[12px] border border-line bg-surface p-4"
        >
          <p className="mb-3 text-[0.82rem] leading-relaxed text-ink-2">
            适用于专著、技术报告、内部文档等没有 DOI / arXiv 编号的论文。
            标题与至少一位作者为必填。
          </p>

          <div className="flex flex-col gap-3">
            <Field label="标题" required>
              <input
                value={title}
                onChange={(e) => setTitle(e.target.value)}
                maxLength={512}
                placeholder="论文标题"
                className={inputClass}
              />
            </Field>

            <Field label="作者" required hint="每行一位，可写「姓名 | 机构」">
              <textarea
                value={authorsText}
                onChange={(e) => setAuthorsText(e.target.value)}
                rows={3}
                placeholder={"张三 | 清华大学\n李四 | 北京大学"}
                className={`${inputClass} resize-y font-mono text-[0.82rem]`}
              />
            </Field>

            <div className="grid gap-3 sm:grid-cols-2">
              <Field label="出处 / 期刊">
                <input
                  value={venue}
                  onChange={(e) => setVenue(e.target.value)}
                  maxLength={256}
                  placeholder="例如：计算机学报"
                  className={inputClass}
                />
              </Field>
              <Field label="年份">
                <input
                  value={year}
                  onChange={(e) => setYear(e.target.value)}
                  inputMode="numeric"
                  maxLength={4}
                  placeholder="2026"
                  className={inputClass}
                />
              </Field>
            </div>

            <Field label="摘要">
              <textarea
                value={abstract}
                onChange={(e) => setAbstract(e.target.value)}
                rows={4}
                placeholder="可选"
                className={`${inputClass} resize-y`}
              />
            </Field>

            <div className="grid gap-3 sm:grid-cols-2">
              <Field label="DOI" hint="填了会参与强去重">
                <input
                  value={doi}
                  onChange={(e) => setDoi(e.target.value)}
                  placeholder="10.xxxx/..."
                  className={`${inputClass} font-mono text-[0.82rem]`}
                />
              </Field>
              <Field label="arXiv 编号" hint="填了会参与强去重">
                <input
                  value={arxivId}
                  onChange={(e) => setArxivId(e.target.value)}
                  placeholder="2405.13412"
                  className={`${inputClass} font-mono text-[0.82rem]`}
                />
              </Field>
            </div>
          </div>

          {error && (
            <p role="alert" className="mt-3 rounded-[6px] border border-danger/40 bg-danger-wash px-3 py-2 text-[0.8rem] text-danger">
              {error}
            </p>
          )}
          {notice && (
            <p role="status" className="mt-3 rounded-[6px] border border-seal/40 bg-seal-wash px-3 py-2 text-[0.8rem] text-seal-deep">
              {notice}
            </p>
          )}

          <div className="mt-4 flex justify-end">
            <button
              type="submit"
              disabled={submitting}
              className="rounded-[6px] bg-seal px-4 py-2 text-[0.85rem] font-semibold text-white hover:bg-seal-deep disabled:opacity-50"
            >
              {submitting ? "创建中…" : "创建论文"}
            </button>
          </div>
        </form>
      )}
    </div>
  );
}

const inputClass =
  "w-full rounded-[6px] border border-line-strong bg-surface px-3 py-2 text-[0.86rem] text-ink placeholder:text-ink-3 focus:border-seal focus:outline-none";

function Field({
  label,
  hint,
  required,
  children,
}: {
  label: string;
  hint?: string;
  required?: boolean;
  children: React.ReactNode;
}) {
  return (
    <label className="block">
      <span className="mb-1 flex items-baseline gap-1.5 font-mono text-[0.66rem] tracking-[0.08em] text-ink-3 uppercase">
        {label}
        {required && <span className="text-danger">*</span>}
        {hint && <span className="normal-case tracking-normal">{hint}</span>}
      </span>
      {children}
    </label>
  );
}
