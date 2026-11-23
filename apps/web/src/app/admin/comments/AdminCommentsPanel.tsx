"use client";

import Link from "next/link";
import { useCallback, useEffect, useState } from "react";
import {
  deleteAdminComment,
  listAdminComments,
  restoreAdminComment,
  type AdminComment,
} from "@/lib/admin";
import { ApiError } from "@/lib/http";
import {
  AdminEmpty,
  AdminError,
  AdminPageHeader,
  AdminSearch,
  AdminSkeleton,
  Badge,
  LoadMore,
} from "@/components/AdminUI";

/**
 * 评论管理：搜索、软删除与恢复。
 *
 * 与帖子管理的差别：评论挂在帖子下，删除后要同步减少帖子的 comment_count
 * （服务端在同一个 CTE 里完成）。这里的「查看」跳到所属帖子，因为评论
 * 没有独立详情页。
 */

const VISIBILITY_LABEL: Record<string, string> = {
  public: "公开",
  followers: "关注者",
  group: "群内",
  private: "私密",
};

export function AdminCommentsPanel() {
  const [query, setQuery] = useState("");
  const [includeDeleted, setIncludeDeleted] = useState(false);
  const [comments, setComments] = useState<AdminComment[]>([]);
  const [cursor, setCursor] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const [loadingMore, setLoadingMore] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [actingId, setActingId] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);

  const load = useCallback(
    async (next: string | null, append: boolean) => {
      if (append) setLoadingMore(true);
      else setLoading(true);
      setError(null);
      try {
        const page = await listAdminComments({
          q: query.trim() || undefined,
          cursor: next,
          limit: 20,
          includeDeleted,
        });
        setComments((prev) => (append ? [...prev, ...page.comments] : page.comments));
        setCursor(page.nextCursor);
      } catch (err) {
        setError(err instanceof ApiError ? err.message : "评论列表加载失败，请稍后重试。");
      } finally {
        setLoading(false);
        setLoadingMore(false);
      }
    },
    [query, includeDeleted],
  );

  useEffect(() => {
    void load(null, false);
  }, [load]);

  async function handleDelete(comment: AdminComment) {
    if (actingId) return;
    if (!window.confirm(`确定删除 @${comment.authorHandle} 的这条评论？删除后可恢复。`)) return;

    setActingId(comment.id);
    setError(null);
    setNotice(null);
    try {
      await deleteAdminComment(comment.id);
      setComments((prev) =>
        prev.map((c) =>
          c.id === comment.id ? { ...c, deletedAt: new Date().toISOString() } : c,
        ),
      );
      setNotice("评论已删除（可恢复）。");
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "删除失败，请稍后重试。");
    } finally {
      setActingId(null);
    }
  }

  async function handleRestore(comment: AdminComment) {
    if (actingId) return;
    setActingId(comment.id);
    setError(null);
    setNotice(null);
    try {
      await restoreAdminComment(comment.id);
      setComments((prev) =>
        prev.map((c) => (c.id === comment.id ? { ...c, deletedAt: null } : c)),
      );
      setNotice("评论已恢复。");
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "恢复失败，请稍后重试。");
    } finally {
      setActingId(null);
    }
  }

  return (
    <>
      <AdminPageHeader
        eyebrow="Comments"
        title="评论管理"
        description="删除为软删除，同时会减少所属帖子的评论计数；可恢复。"
      />

      <AdminSearch
        value={query}
        onChange={setQuery}
        onSubmit={() => void load(null, false)}
        placeholder="搜索评论正文"
      >
        <label className="flex items-center gap-1.5 rounded-[6px] border border-line-strong bg-surface px-2.5 py-2 text-[0.8rem] text-ink-2">
          <input
            type="checkbox"
            checked={includeDeleted}
            onChange={(e) => setIncludeDeleted(e.target.checked)}
            className="size-3.5 accent-[var(--color-seal)]"
          />
          显示已删除
        </label>
      </AdminSearch>

      {error && <AdminError message={error} onRetry={() => void load(null, false)} />}
      {notice && (
        <p role="status" className="mb-4 rounded-[6px] border border-good/40 bg-good-wash px-3.5 py-2.5 text-[0.82rem] text-good">
          {notice}
        </p>
      )}

      {loading ? (
        <AdminSkeleton rows={5} />
      ) : comments.length === 0 ? (
        <AdminEmpty>没有匹配的评论。</AdminEmpty>
      ) : (
        <ul className="flex flex-col gap-2">
          {comments.map((c, i) => {
            const deleted = c.deletedAt !== null;
            const busy = actingId === c.id;
            return (
              <li
                key={c.id}
                style={{ animationDelay: `${Math.min(i, 8) * 30}ms` }}
                className={`anim-rise rounded-[10px] border border-line bg-surface px-3.5 py-3 ${
                  deleted ? "opacity-60" : ""
                }`}
              >
                <div className="mb-1.5 flex flex-wrap items-center gap-2">
                  <Link
                    href={`/u/${encodeURIComponent(c.authorHandle)}`}
                    className="text-[0.82rem] font-medium text-ink hover:text-seal hover:underline"
                  >
                    {c.authorName}
                  </Link>
                  <span className="font-mono text-[0.7rem] text-ink-3">@{c.authorHandle}</span>
                  {c.parentId && <Badge label="回复" tone="neutral" />}
                  <Badge label={VISIBILITY_LABEL[c.postVisibility] ?? c.postVisibility} />
                  {c.postDeleted && <Badge label="原帖已删" tone="warn" />}
                  {deleted && <Badge label="已删除" tone="danger" />}
                  <span className="ml-auto font-mono text-[0.68rem] text-ink-3">
                    {formatDateTime(c.createdAt)}
                  </span>
                </div>

                <p className="mb-2 line-clamp-3 text-[0.84rem] leading-relaxed text-ink-2">
                  {c.excerpt || "（无正文）"}
                </p>

                <div className="flex flex-wrap items-center gap-3">
                  <span className="font-mono text-[0.68rem] text-ink-3 tabular-nums">
                    ♥ {c.likeCount}
                  </span>
                  {!c.postDeleted && (
                    <Link
                      href={`/posts/${c.postId}`}
                      className="font-mono text-[0.68rem] text-ink-3 hover:text-seal"
                    >
                      查看原帖 →
                    </Link>
                  )}
                  <div className="ml-auto flex items-center gap-1.5">
                    {deleted ? (
                      <button
                        type="button"
                        disabled={busy}
                        onClick={() => void handleRestore(c)}
                        className="rounded-[4px] border border-good/40 px-2 py-1 font-mono text-[0.68rem] text-good hover:bg-good-wash disabled:opacity-40"
                      >
                        恢复
                      </button>
                    ) : (
                      <button
                        type="button"
                        disabled={busy}
                        onClick={() => void handleDelete(c)}
                        className="rounded-[4px] border border-danger/40 px-2 py-1 font-mono text-[0.68rem] text-danger hover:bg-danger-wash disabled:opacity-40"
                      >
                        删除
                      </button>
                    )}
                  </div>
                </div>
              </li>
            );
          })}
        </ul>
      )}

      <LoadMore cursor={cursor} loading={loadingMore} onClick={() => void load(cursor, true)} />
    </>
  );
}

function formatDateTime(iso: string): string {
  if (!iso) return "—";
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return "—";
  return d.toLocaleString("zh-CN", {
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
  });
}
