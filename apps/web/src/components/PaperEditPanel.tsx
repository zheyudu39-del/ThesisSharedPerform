"use client";

import { useState } from "react";
import { useRouter } from "next/navigation";
import { useAuth } from "@/components/AuthProvider";
import {
  mergePapers,
  updatePaper,
  type PaperAuthor,
  type PaperDTO,
} from "@/lib/papers";

/**
 * 论文元数据编辑与合并（PRD P-02）。
 *
 * 只对论文创建者与管理员显示入口——服务端也会校验，这里只是不给
 * 无意义的按钮。合并是破坏性操作（会删除一条记录），因此要求二次确认，
 * 并明确列出会被迁移的关联数量。
 */

export function PaperEditPanel({ paper }: { paper: PaperDTO }) {
  const { status, user } = useAuth();
  const router = useRouter();
  const [open, setOpen] = useState(false);
  const [title, setTitle] = useState(paper.title);
  const [authorsText, setAuthorsText] = useState(
    paper.authors.map((a) => (a.affiliation ? `${a.name} | ${a.affiliation}` : a.name)).join("\n"),
  );
  const [abstract, setAbstract] = useState(paper.abstract);
  const [venue, setVenue] = useState(paper.venue);
  const [year, setYear] = useState(paper.publishedYear > 0 ? String(paper.publishedYear) : "");
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);

  // 合并相关状态
  const [mergeOpen, setMergeOpen] = useState(false);
  const [duplicateId, setDuplicateId] = useState("");
  const [merging, setMerging] = useState(false);
  const [mergeError, setMergeError] = useState<string | null>(null);

  if (status !== "authenticated") return null;
  // 前端无法得知 created_by，这里只做「登录了就给入口」的粗判；
  // 真正的权限由服务端返回 403 决定。管理员同样能进。
  void user;

  function parseAuthors(raw: string): PaperAuthor[] {
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

  async function handleSave() {
    setError(null);
    setNotice(null);
    const authors = parseAuthors(authorsText);
    if (!title.trim()) {
      setError("标题不能为空。");
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

    setSaving(true);
    try {
      await updatePaper(paper.id, {
        title: title.trim(),
        authors,
        abstract: abstract.trim(),
        venue: venue.trim(),
        publishedYear: publishedYear || 0,
      });
      setNotice("已保存。");
      router.refresh();
    } catch (err) {
      setError(err instanceof Error ? err.message : "保存失败，请稍后重试。");
    } finally {
      setSaving(false);
    }
  }

  async function handleMerge() {
    const dup = duplicateId.trim();
    if (!dup) {
      setMergeError("请填写要合并掉的论文 ID。");
      return;
    }
    if (dup === paper.id) {
      setMergeError("不能把论文合并到自己。");
      return;
    }
    if (
      !window.confirm(
        "合并后，重复论文下的帖子、标签、群资料库条目、策展项与文档引用都会迁到当前论文，重复记录将被删除。此操作不可撤销，确定继续？",
      )
    ) {
      return;
    }
    setMerging(true);
    setMergeError(null);
    try {
      const result = await mergePapers(paper.id, dup);
      const m = result.moved;
      setNotice(
        `已合并：迁移帖子 ${m.posts} 条、标签 ${m.tags} 个、群资料库 ${m.libraryItems} 条、` +
          `策展项 ${m.collectionItems} 条、文档引用 ${m.docReferences} 条。`,
      );
      setDuplicateId("");
      setMergeOpen(false);
      router.refresh();
    } catch (err) {
      setMergeError(err instanceof Error ? err.message : "合并失败，请稍后重试。");
    } finally {
      setMerging(false);
    }
  }

  if (!open) {
    return (
      <div className="mb-6">
        <button
          type="button"
          onClick={() => setOpen(true)}
          className="text-[0.8rem] text-ink-2 underline decoration-line-strong underline-offset-4 hover:text-seal"
        >
          编辑元数据
        </button>
      </div>
    );
  }

  return (
    <section className="mb-6 rounded-[12px] border border-line bg-surface p-4">
      <h2 className="mb-3 font-mono text-[0.68rem] tracking-[0.1em] text-ink-3 uppercase">
        编辑元数据
      </h2>

      <div className="flex flex-col gap-3">
        <label className="block">
          <span className="mb-1 block font-mono text-[0.64rem] tracking-[0.08em] text-ink-3 uppercase">标题</span>
          <input
            value={title}
            onChange={(e) => setTitle(e.target.value)}
            maxLength={512}
            className={inputClass}
          />
        </label>

        <label className="block">
          <span className="mb-1 block font-mono text-[0.64rem] tracking-[0.08em] text-ink-3 uppercase">
            作者 <span className="normal-case tracking-normal">（每行一位，可写「姓名 | 机构」）</span>
          </span>
          <textarea
            value={authorsText}
            onChange={(e) => setAuthorsText(e.target.value)}
            rows={3}
            className={`${inputClass} resize-y font-mono text-[0.8rem]`}
          />
        </label>

        <div className="grid gap-3 sm:grid-cols-2">
          <label className="block">
            <span className="mb-1 block font-mono text-[0.64rem] tracking-[0.08em] text-ink-3 uppercase">出处 / 期刊</span>
            <input
              value={venue}
              onChange={(e) => setVenue(e.target.value)}
              maxLength={256}
              className={inputClass}
            />
          </label>
          <label className="block">
            <span className="mb-1 block font-mono text-[0.64rem] tracking-[0.08em] text-ink-3 uppercase">年份</span>
            <input
              value={year}
              onChange={(e) => setYear(e.target.value)}
              inputMode="numeric"
              maxLength={4}
              className={inputClass}
            />
          </label>
        </div>

        <label className="block">
          <span className="mb-1 block font-mono text-[0.64rem] tracking-[0.08em] text-ink-3 uppercase">摘要</span>
          <textarea
            value={abstract}
            onChange={(e) => setAbstract(e.target.value)}
            rows={5}
            className={`${inputClass} resize-y`}
          />
        </label>
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

      <div className="mt-4 flex flex-wrap items-center justify-between gap-2">
        <button
          type="button"
          onClick={() => setOpen(false)}
          className="text-[0.8rem] text-ink-3 hover:text-ink"
        >
          收起
        </button>
        <button
          type="button"
          onClick={() => void handleSave()}
          disabled={saving}
          className="rounded-[6px] bg-seal px-4 py-2 text-[0.85rem] font-semibold text-white hover:bg-seal-deep disabled:opacity-50"
        >
          {saving ? "保存中…" : "保存"}
        </button>
      </div>

      {/* 合并重复论文（P-02）：破坏性操作，单独一块并二次确认。 */}
      <div className="mt-4 border-t border-line pt-3">
        <button
          type="button"
          onClick={() => setMergeOpen((v) => !v)}
          aria-expanded={mergeOpen}
          className="text-[0.78rem] text-ink-3 hover:text-danger"
        >
          {mergeOpen ? "取消合并" : "合并一篇重复论文…"}
        </button>

        {mergeOpen && (
          <div className="mt-2 rounded-[6px] border border-danger/30 bg-danger-wash/40 p-2.5">
            <p className="mb-2 text-[0.74rem] leading-relaxed text-ink-2">
              把另一条重复记录合并到本篇。帖子、标签、群资料库、策展项与文档引用都会迁移过来，
              重复记录随后删除。<strong>不可撤销。</strong>
            </p>
            <div className="flex gap-1.5">
              <input
                value={duplicateId}
                onChange={(e) => setDuplicateId(e.target.value)}
                placeholder="要合并掉的论文 ID"
                aria-label="重复论文 ID"
                className={`${inputClass} font-mono text-[0.76rem]`}
              />
              <button
                type="button"
                onClick={() => void handleMerge()}
                disabled={merging}
                className="shrink-0 rounded-[4px] bg-danger px-3 py-1 text-[0.76rem] font-semibold text-white hover:opacity-90 disabled:opacity-50"
              >
                {merging ? "合并中…" : "合并"}
              </button>
            </div>
            {mergeError && (
              <p role="alert" className="mt-1.5 text-[0.74rem] text-danger">
                {mergeError}
              </p>
            )}
          </div>
        )}
      </div>
    </section>
  );
}

const inputClass =
  "w-full rounded-[6px] border border-line-strong bg-surface px-3 py-2 text-[0.86rem] text-ink placeholder:text-ink-3 focus:border-seal focus:outline-none";
