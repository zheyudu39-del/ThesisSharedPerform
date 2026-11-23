"use client";

import Link from "next/link";
import { useCallback, useEffect, useState } from "react";
import {
  deleteAdminPost,
  listAdminPosts,
  restoreAdminPost,
  type AdminPost,
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
 * 帖子管理：搜索、查看、软删除与恢复。
 *
 * 删除是软删除（写 deleted_at），因此可以恢复；「显示已删除」开关让运营
 * 能找到误删的内容。真正的物理删除不在后台提供——帖子可能被他人转发引用，
 * 硬删会留下悬空引用。
 */

const VISIBILITY_LABEL: Record<string, string> = {
  public: "公开",
  followers: "关注者",
  group: "群内",
  private: "私密",
};

export function AdminPostsPanel() {
  const [query, setQuery] = useState("");
  const [includeDeleted, setIncludeDeleted] = useState(false);
  const [posts, setPosts] = useState<AdminPost[]>([]);
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
        const page = await listAdminPosts({
          q: query.trim() || undefined,
          cursor: next,
          limit: 20,
          includeDeleted,
        });
        setPosts((prev) => (append ? [...prev, ...page.posts] : page.posts));
        setCursor(page.nextCursor);
      } catch (err) {
        setError(err instanceof ApiError ? err.message : "帖子列表加载失败，请稍后重试。");
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

  async function handleDelete(post: AdminPost) {
    if (actingId) return;
    if (!window.confirm(`确定删除 @${post.authorHandle} 的这条帖子？删除后可恢复。`)) return;

    setActingId(post.id);
    setError(null);
    setNotice(null);
    try {
      await deleteAdminPost(post.id);
      const now = new Date().toISOString();
      setPosts((prev) =>
        prev.map((p) => (p.id === post.id ? { ...p, deletedAt: now } : p)),
      );
      setNotice("帖子已删除（可恢复）。");
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "删除失败，请稍后重试。");
    } finally {
      setActingId(null);
    }
  }

  async function handleRestore(post: AdminPost) {
    if (actingId) return;
    setActingId(post.id);
    setError(null);
    setNotice(null);
    try {
      await restoreAdminPost(post.id);
      setPosts((prev) =>
        prev.map((p) => (p.id === post.id ? { ...p, deletedAt: null } : p)),
      );
      setNotice("帖子已恢复。");
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "恢复失败，请稍后重试。");
    } finally {
      setActingId(null);
    }
  }

  return (
    <>
      <AdminPageHeader
        eyebrow="Posts"
        title="帖子管理"
        description="删除为软删除，内容对前台立即不可见，但保留记录以便恢复与审计。"
      />

      <AdminSearch
        value={query}
        onChange={setQuery}
        onSubmit={() => void load(null, false)}
        placeholder="搜索帖子正文"
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
      ) : posts.length === 0 ? (
        <AdminEmpty>没有匹配的帖子。</AdminEmpty>
      ) : (
        <ul className="flex flex-col gap-2">
          {posts.map((p, i) => {
            const deleted = p.deletedAt !== null;
            const busy = actingId === p.id;
            return (
              <li
                key={p.id}
                style={{ animationDelay: `${Math.min(i, 8) * 30}ms` }}
                className={`anim-rise rounded-[10px] border border-line bg-surface px-3.5 py-3 ${
                  deleted ? "opacity-60" : ""
                }`}
              >
                <div className="mb-1.5 flex flex-wrap items-center gap-2">
                  <Link
                    href={`/u/${encodeURIComponent(p.authorHandle)}`}
                    className="text-[0.82rem] font-medium text-ink hover:text-seal hover:underline"
                  >
                    {p.authorName}
                  </Link>
                  <span className="font-mono text-[0.7rem] text-ink-3">@{p.authorHandle}</span>
                  <Badge label={VISIBILITY_LABEL[p.visibility] ?? p.visibility} />
                  {p.type === "repost" && <Badge label="转发" tone="neutral" />}
                  {p.groupId && <Badge label="群内" tone="neutral" />}
                  {deleted && <Badge label="已删除" tone="danger" />}
                  <span className="ml-auto font-mono text-[0.68rem] text-ink-3">
                    {formatDateTime(p.createdAt)}
                  </span>
                </div>

                <p className="mb-2 line-clamp-3 text-[0.84rem] leading-relaxed text-ink-2">
                  {p.excerpt || "（无正文）"}
                </p>

                <div className="flex flex-wrap items-center gap-3">
                  <span className="font-mono text-[0.68rem] text-ink-3 tabular-nums">
                    ♥ {p.likeCount} · 💬 {p.commentCount} · ⇄ {p.repostCount}
                  </span>
                  {p.paperId && (
                    <Link
                      href={`/papers/${p.paperId}`}
                      className="font-mono text-[0.68rem] text-ink-3 hover:text-seal"
                    >
                      关联论文 →
                    </Link>
                  )}
                  <div className="ml-auto flex items-center gap-1.5">
                    <Link
                      href={`/posts/${p.id}`}
                      className="rounded-[4px] border border-line-strong px-2 py-1 font-mono text-[0.68rem] text-ink-2 hover:border-ink-3 hover:text-ink"
                    >
                      查看
                    </Link>
                    {deleted ? (
                      <button
                        type="button"
                        disabled={busy}
                        onClick={() => void handleRestore(p)}
                        className="rounded-[4px] border border-good/40 px-2 py-1 font-mono text-[0.68rem] text-good hover:bg-good-wash disabled:opacity-40"
                      >
                        恢复
                      </button>
                    ) : (
                      <button
                        type="button"
                        disabled={busy}
                        onClick={() => void handleDelete(p)}
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
