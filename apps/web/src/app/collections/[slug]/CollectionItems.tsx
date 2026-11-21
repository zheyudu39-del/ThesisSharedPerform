"use client";

import Link from "next/link";
import { useState } from "react";
import { PaperPreviewCard } from "@/components/PaperPreviewCard";
import { PostCard } from "@/components/PostCard";
import {
  ApiError,
  addCollectionItem,
  removeCollectionItem,
  reorderCollectionItem,
  type CollectionItemDTO,
} from "@/lib/collections";

/**
 * 策展条目列表 + 编辑面板。
 *
 * 条目在服务端渲染（便于爬虫抓取），这里只接管编辑操作：
 * 增删与排序都直接调用写接口，成功后由父级 router.refresh() 重取。
 *
 * 阅读态与编辑态刻意分开：默认是干净的阅读列表，
 * 只有 canEdit 时右侧才出现「编辑条目」抽屉，避免把管理控件混进阅读流。
 */
export function CollectionItems({
  slug,
  items,
  canEdit,
}: {
  slug: string;
  items: CollectionItemDTO[];
  canEdit: boolean;
}) {
  const [editing, setEditing] = useState(false);
  const [localItems, setLocalItems] = useState(items);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  async function handleRemove(itemId: string) {
    setBusy(true);
    setError(null);
    try {
      await removeCollectionItem(slug, itemId);
      setLocalItems((prev) => prev.filter((item) => item.id !== itemId));
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "移除失败，请稍后重试。");
    } finally {
      setBusy(false);
    }
  }

  async function handleMove(itemId: string, position: number) {
    setBusy(true);
    setError(null);
    try {
      await reorderCollectionItem(slug, itemId, position);
      setLocalItems((prev) => {
        const next = prev.map((item) => (item.id === itemId ? { ...item, position } : item));
        return [...next].sort((a, b) => a.position - b.position);
      });
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "调整顺序失败。");
    } finally {
      setBusy(false);
    }
  }

  if (localItems.length === 0) {
    return (
      <p className="rounded-[12px] border border-dashed border-line-strong bg-surface px-5 py-14 text-center text-[0.9rem] text-ink-2">
        这个栏目还没有内容。
      </p>
    );
  }

  return (
    <div className="flex flex-col gap-4">
      {canEdit && (
        <div className="flex items-center justify-between rounded-[6px] border border-line bg-surface-2 px-3.5 py-2">
          <p className="font-mono text-[0.72rem] text-ink-3">
            {editing ? "编辑模式：可调整顺序与移除条目" : "你是这个栏目的策展人"}
          </p>
          <button
            type="button"
            onClick={() => setEditing((v) => !v)}
            className="rounded-[3px] px-2 py-0.5 text-[0.76rem] font-semibold text-seal hover:bg-seal-wash"
          >
            {editing ? "完成" : "编辑条目"}
          </button>
        </div>
      )}

      {error && (
        <p role="alert" className="rounded-[6px] border border-danger/40 bg-danger-wash px-3 py-2 text-[0.8rem] text-danger">
          {error}
        </p>
      )}

      <ol className="flex flex-col gap-4">
        {localItems.map((item, index) => (
          <li key={item.id} className="flex gap-4">
            {/* 序号：策展是有顺序的，序号本身是叙事的一部分 */}
            <span className="mt-1 shrink-0 font-mono text-[0.9rem] text-ink-3 tabular-nums">
              {String(index + 1).padStart(2, "0")}
            </span>

            <div className="min-w-0 flex-1">
              {item.note && (
                <p className="mb-2 border-l-2 border-seal/40 pl-3 text-[0.86rem] leading-relaxed text-ink-2 italic">
                  {item.note}
                </p>
              )}

              {item.post && <PostCard post={item.post} />}

              {item.paper && (
                <div className="rounded-[12px] border border-line bg-surface p-4">
                  <PaperPreviewCard paper={item.paper} compact />
                  <Link
                    href={`/papers/${item.paper.id}`}
                    className="mt-2 inline-block font-mono text-[0.72rem] text-seal hover:underline"
                  >
                    查看论文详情 →
                  </Link>
                </div>
              )}

              {!item.post && !item.paper && (
                <p className="rounded-[6px] border border-dashed border-line-strong px-3 py-4 text-[0.8rem] text-ink-3">
                  该内容已被作者删除或设为不可见。
                </p>
              )}

              {editing && canEdit && (
                <div className="mt-2 flex gap-2">
                  <button
                    type="button"
                    disabled={busy || index === 0}
                    onClick={() => void handleMove(item.id, Math.max(0, item.position - 1))}
                    className="rounded-[3px] border border-line-strong px-2 py-0.5 text-[0.72rem] text-ink-2 hover:border-ink-3 disabled:opacity-40"
                  >
                    ↑ 上移
                  </button>
                  <button
                    type="button"
                    disabled={busy || index === localItems.length - 1}
                    onClick={() => void handleMove(item.id, item.position + 1)}
                    className="rounded-[3px] border border-line-strong px-2 py-0.5 text-[0.72rem] text-ink-2 hover:border-ink-3 disabled:opacity-40"
                  >
                    ↓ 下移
                  </button>
                  <button
                    type="button"
                    disabled={busy}
                    onClick={() => void handleRemove(item.id)}
                    className="ml-auto rounded-[3px] px-2 py-0.5 text-[0.72rem] text-danger hover:bg-danger-wash disabled:opacity-40"
                  >
                    移除
                  </button>
                </div>
              )}
            </div>
          </li>
        ))}
      </ol>

      {editing && canEdit && <AddItemForm slug={slug} onAdded={(item) => setLocalItems((prev) => [...prev, item])} />}
    </div>
  );
}

/**
 * 添加条目：按 ID 引用已有的帖子或论文。
 *
 * 不做站内搜索选择器是刻意的取舍——策展是低频、深思熟虑的操作，
 * 编辑通常手里已经有链接；做一个完整的搜索选择器成本远高于收益。
 * 从帖子/论文详情页复制 ID 即可。
 */
function AddItemForm({ slug, onAdded }: { slug: string; onAdded: (item: CollectionItemDTO) => void }) {
  const [kind, setKind] = useState<"post" | "paper">("post");
  const [id, setId] = useState("");
  const [note, setNote] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    const trimmed = id.trim();
    if (!trimmed) {
      setError("请填写内容 ID。");
      return;
    }
    setSubmitting(true);
    setError(null);
    try {
      const created = await addCollectionItem(slug, {
        [kind === "post" ? "postId" : "paperId"]: trimmed,
        note: note.trim() || undefined,
      });
      onAdded(created);
      setId("");
      setNote("");
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "添加失败，请检查 ID 是否正确。");
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <form
      onSubmit={submit}
      className="flex flex-col gap-2.5 rounded-[12px] border border-line bg-surface p-4"
    >
      <h3 className="font-display text-[0.95rem] font-semibold">添加条目</h3>

      <div className="flex gap-1 rounded-[6px] border border-line bg-surface-2 p-1">
        {(["post", "paper"] as const).map((value) => (
          <button
            key={value}
            type="button"
            onClick={() => setKind(value)}
            className={`flex-1 rounded-[3px] px-2 py-1 text-[0.78rem] ${
              kind === value ? "bg-surface font-semibold text-ink" : "text-ink-2"
            }`}
          >
            {value === "post" ? "帖子" : "论文"}
          </button>
        ))}
      </div>

      <input
        value={id}
        onChange={(e) => setId(e.target.value)}
        placeholder={kind === "post" ? "帖子 ID" : "论文 ID"}
        className="rounded-[6px] border border-line-strong bg-paper px-3 py-1.5 font-mono text-[0.8rem] text-ink focus:border-seal focus:outline-none"
      />

      <input
        value={note}
        onChange={(e) => setNote(e.target.value)}
        maxLength={280}
        placeholder="推荐语（可选）"
        className="rounded-[6px] border border-line-strong bg-paper px-3 py-1.5 text-[0.86rem] text-ink focus:border-seal focus:outline-none"
      />

      {error && <p className="text-[0.78rem] text-danger">{error}</p>}

      <button
        type="submit"
        disabled={submitting}
        className="self-start rounded-[6px] bg-seal px-3.5 py-1.5 text-[0.82rem] font-semibold text-white hover:bg-seal-deep disabled:opacity-60"
      >
        {submitting ? "添加中…" : "添加"}
      </button>
    </form>
  );
}
