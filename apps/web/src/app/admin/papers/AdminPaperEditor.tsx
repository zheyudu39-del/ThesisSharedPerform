"use client";

import { useState } from "react";
import type { AdminPaper } from "@/lib/admin";
import { mergePapers, updatePaper } from "@/lib/papers";
import { ApiError } from "@/lib/http";

/**
 * 论文元数据编辑与合并（后台版）。
 *
 * 直接复用论文详情页的 PATCH /papers/{id} 与 POST /papers/{id}/merge——
 * 服务端的 requireEditor 已把「创建者或管理员」写进规则，后台调用天然通过。
 *
 * 与前台 PaperEditPanel 的差别：
 *   - 后台列表只有摘要字段，编辑时需要单独拉完整论文（含作者与摘要）；
 *   - 合并的「重复论文」用 ID 输入，因为后台没有逐条浏览的上下文。
 */

type PaperDetail = {
  title: string;
  authorsText: string;
  abstract: string;
  venue: string;
  year: string;
  citationCount: string;
};

function emptyDetail(): PaperDetail {
  return { title: "", authorsText: "", abstract: "", venue: "", year: "", citationCount: "" };
}

export function AdminPaperEditor({
  paper,
  onSaved,
  onMerged,
  onClose,
}: {
  paper: AdminPaper;
  onSaved: (patch: Partial<AdminPaper>) => void;
  onMerged: (removedId: string) => void;
  onClose: () => void;
}) {
  const [detail, setDetail] = useState<PaperDetail>(emptyDetail);
  const [loaded, setLoaded] = useState(false);
  const [loading, setLoading] = useState(false);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);

  // 合并相关
  const [duplicateId, setDuplicateId] = useState("");
  const [merging, setMerging] = useState(false);

  async function loadDetail() {
    if (loaded || loading) return;
    setLoading(true);
    setError(null);
    try {
      const res = await fetch(`/api/v1/papers/${encodeURIComponent(paper.id)}`, {
        cache: "no-store",
      });
      if (!res.ok) throw new Error("加载论文详情失败");
      const body = (await res.json()) as {
        paper?: {
          title?: string;
          authors?: { name: string; affiliation?: string }[];
          abstract?: string;
          venue?: string;
          publishedYear?: number;
          citationCount?: number;
        };
      };
      const p = body.paper;
      if (!p) throw new Error("响应缺少 paper 字段");
      setDetail({
        title: p.title ?? "",
        authorsText: (p.authors ?? [])
          .map((a) => (a.affiliation ? `${a.name} | ${a.affiliation}` : a.name))
          .join("\n"),
        abstract: p.abstract ?? "",
        venue: p.venue ?? "",
        year: p.publishedYear ? String(p.publishedYear) : "",
        citationCount: p.citationCount ? String(p.citationCount) : "",
      });
      setLoaded(true);
    } catch (err) {
      setError(err instanceof Error ? err.message : "加载失败，请稍后重试。");
    } finally {
      setLoading(false);
    }
  }

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

  async function handleSave() {
    setError(null);
    setNotice(null);

    const authors = parseAuthors(detail.authorsText);
    if (!detail.title.trim()) {
      setError("标题不能为空。");
      return;
    }
    if (authors.length === 0) {
      setError("至少填写一位作者。");
      return;
    }
    const year = detail.year.trim() ? Number(detail.year) : undefined;
    if (year !== undefined && (!Number.isInteger(year) || year < 1000 || year > 2100)) {
      setError("年份格式不正确，请填 4 位年份。");
      return;
    }
    const citations = detail.citationCount.trim() ? Number(detail.citationCount) : undefined;
    if (citations !== undefined && (!Number.isInteger(citations) || citations < 0)) {
      setError("引用数必须是非负整数。");
      return;
    }

    setSaving(true);
    try {
      await updatePaper(paper.id, {
        title: detail.title.trim(),
        authors,
        abstract: detail.abstract.trim(),
        venue: detail.venue.trim(),
        publishedYear: year,
        citationCount: citations,
      });
      onSaved({
        title: detail.title.trim(),
        venue: detail.venue.trim(),
        authorCount: authors.length,
        citationCount: citations ?? paper.citationCount,
      });
      setNotice("已保存。");
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "保存失败，请稍后重试。");
    } finally {
      setSaving(false);
    }
  }

  async function handleMerge() {
    setError(null);
    setNotice(null);
    const dup = duplicateId.trim();
    if (!dup) {
      setError("请填写要合并掉的论文 ID。");
      return;
    }
    if (dup === paper.id) {
      setError("不能把论文合并到自己。");
      return;
    }
    if (
      !window.confirm(
        `确定把论文 ${dup} 合并进当前论文？\n\n` +
          `重复记录的帖子、标签、收藏与文档引用会迁移过来，然后重复记录被删除。此操作不可撤销。`,
      )
    ) {
      return;
    }

    setMerging(true);
    try {
      const result = await mergePapers(paper.id, dup);
      const moved = result.moved;
      const summary = [
        moved?.posts ? `${moved.posts} 条帖子` : null,
        moved?.tags ? `${moved.tags} 个标签` : null,
        moved?.libraryItems ? `${moved.libraryItems} 项资料库` : null,
        moved?.collectionItems ? `${moved.collectionItems} 项策展` : null,
        moved?.docReferences ? `${moved.docReferences} 处引用` : null,
      ]
        .filter(Boolean)
        .join("、");
      setNotice(summary ? `合并完成，已迁移 ${summary}。` : "合并完成。");
      setDuplicateId("");
      onMerged(dup);
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "合并失败，请稍后重试。");
    } finally {
      setMerging(false);
    }
  }

  return (
    <div className="mt-2 rounded-[8px] border border-line-strong bg-surface-2 p-3">
      {!loaded ? (
        <div className="flex items-center gap-2">
          <button
            type="button"
            onClick={() => void loadDetail()}
            disabled={loading}
            className="rounded-[4px] bg-ink px-3 py-1.5 font-mono text-[0.72rem] text-paper disabled:opacity-50"
          >
            {loading ? "加载中…" : "展开编辑"}
          </button>
          <button
            type="button"
            onClick={onClose}
            className="rounded-[4px] border border-line-strong px-3 py-1.5 font-mono text-[0.72rem] text-ink-2 hover:text-ink"
          >
            收起
          </button>
        </div>
      ) : (
        <div className="flex flex-col gap-2.5">
          <Field label="标题">
            <input
              value={detail.title}
              onChange={(e) => setDetail({ ...detail, title: e.target.value })}
              maxLength={512}
              className={inputClass}
            />
          </Field>

          <Field label="作者" hint="每行一位，可写「姓名 | 机构」">
            <textarea
              value={detail.authorsText}
              onChange={(e) => setDetail({ ...detail, authorsText: e.target.value })}
              rows={3}
              className={`${inputClass} resize-y font-mono text-[0.78rem]`}
            />
          </Field>

          <div className="grid gap-2.5 sm:grid-cols-3">
            <Field label="出处">
              <input
                value={detail.venue}
                onChange={(e) => setDetail({ ...detail, venue: e.target.value })}
                className={inputClass}
              />
            </Field>
            <Field label="年份">
              <input
                value={detail.year}
                onChange={(e) => setDetail({ ...detail, year: e.target.value })}
                inputMode="numeric"
                maxLength={4}
                className={inputClass}
              />
            </Field>
            <Field label="引用数">
              <input
                value={detail.citationCount}
                onChange={(e) => setDetail({ ...detail, citationCount: e.target.value })}
                inputMode="numeric"
                className={inputClass}
              />
            </Field>
          </div>

          <Field label="摘要">
            <textarea
              value={detail.abstract}
              onChange={(e) => setDetail({ ...detail, abstract: e.target.value })}
              rows={4}
              className={`${inputClass} resize-y`}
            />
          </Field>

          <div className="flex flex-wrap items-center gap-2 border-t border-line pt-2.5">
            <button
              type="button"
              onClick={() => void handleSave()}
              disabled={saving}
              className="rounded-[4px] bg-seal px-3.5 py-1.5 font-mono text-[0.72rem] font-semibold text-white hover:bg-seal-deep disabled:opacity-50"
            >
              {saving ? "保存中…" : "保存元数据"}
            </button>
            <button
              type="button"
              onClick={onClose}
              className="rounded-[4px] border border-line-strong px-3 py-1.5 font-mono text-[0.72rem] text-ink-2 hover:text-ink"
            >
              收起
            </button>
          </div>

          {/* 合并区：单独一块，避免和日常编辑混在一起误点。 */}
          <div className="mt-1 rounded-[6px] border border-warn/40 bg-warn-wash/40 p-2.5">
            <p className="mb-1.5 font-mono text-[0.68rem] tracking-[0.06em] text-warn uppercase">
              合并重复论文
            </p>
            <p className="mb-2 text-[0.74rem] leading-relaxed text-ink-2">
              把另一篇重复记录的关联迁移到当前论文，然后删除那条记录。不可撤销。
            </p>
            <div className="flex flex-wrap items-center gap-2">
              <input
                value={duplicateId}
                onChange={(e) => setDuplicateId(e.target.value)}
                placeholder="重复论文的 ID（UUID）"
                className={`${inputClass} min-w-[240px] flex-1 font-mono text-[0.74rem]`}
              />
              <button
                type="button"
                onClick={() => void handleMerge()}
                disabled={merging}
                className="rounded-[4px] border border-warn/60 bg-surface px-3 py-1.5 font-mono text-[0.72rem] text-warn hover:bg-warn-wash disabled:opacity-50"
              >
                {merging ? "合并中…" : "合并"}
              </button>
            </div>
          </div>
        </div>
      )}

      {error && (
        <p role="alert" className="mt-2 rounded-[4px] border border-danger/40 bg-danger-wash px-2.5 py-1.5 text-[0.76rem] text-danger">
          {error}
        </p>
      )}
      {notice && (
        <p role="status" className="mt-2 rounded-[4px] border border-good/40 bg-good-wash px-2.5 py-1.5 text-[0.76rem] text-good">
          {notice}
        </p>
      )}
    </div>
  );
}

const inputClass =
  "w-full rounded-[4px] border border-line-strong bg-surface px-2.5 py-1.5 text-[0.8rem] text-ink placeholder:text-ink-3 focus:border-seal focus:outline-none";

function Field({
  label,
  hint,
  children,
}: {
  label: string;
  hint?: string;
  children: React.ReactNode;
}) {
  return (
    <label className="flex flex-col gap-1">
      <span className="font-mono text-[0.66rem] tracking-[0.06em] text-ink-3 uppercase">
        {label}
        {hint && <span className="ml-1.5 normal-case text-ink-3">· {hint}</span>}
      </span>
      {children}
    </label>
  );
}
