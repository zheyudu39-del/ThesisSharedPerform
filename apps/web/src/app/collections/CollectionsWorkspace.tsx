"use client";

import { useCallback, useEffect, useState } from "react";
import { useAuth } from "@/components/AuthProvider";
import { CollectionCard } from "@/components/CollectionCard";
import {
  ApiError,
  createCollection,
  listCollections,
  type CollectionDTO,
} from "@/lib/collections";

/**
 * 精选策展列表工作区。
 *
 * 列表本身匿名可读（供 SSR 抓取与分享链接直接打开），因此这里不做登录门禁；
 * 只有「新建策展集」需要登录且需要策展资格——资格由服务端判定，
 * 前端对普通用户直接隐藏入口，但不把它当作安全边界。
 */
export function CollectionsWorkspace() {
  const { status, user } = useAuth();
  const [collections, setCollections] = useState<CollectionDTO[]>([]);
  const [cursor, setCursor] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const [loadingMore, setLoadingMore] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [showCreate, setShowCreate] = useState(false);

  const canCurate =
    user !== null && (user.role === "admin" || user.verifyType === "influencer");

  const load = useCallback(async (next?: string | null) => {
    if (next) setLoadingMore(true);
    else setLoading(true);
    setError(null);
    try {
      const page = await listCollections(next);
      setCollections((prev) => (next ? [...prev, ...page.collections] : page.collections));
      setCursor(page.nextCursor);
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "精选列表加载失败，请稍后重试。");
    } finally {
      setLoading(false);
      setLoadingMore(false);
    }
  }, []);

  useEffect(() => {
    void load(null);
  }, [load]);

  function handleCreated(created: CollectionDTO) {
    setShowCreate(false);
    setCollections((prev) => [created, ...prev]);
  }

  return (
    <main className="mx-auto max-w-4xl px-5 py-10">
      <div className="mb-8 flex flex-wrap items-end justify-between gap-4 border-b border-line pb-7">
        <div>
          <p className="mb-3 font-mono text-[0.72rem] tracking-[0.16em] text-ink-3 uppercase">
            精选 · Curated
          </p>
          <h1 className="font-display text-[clamp(1.8rem,4vw,2.6rem)] leading-[1.05] font-semibold tracking-[-0.03em]">
            编辑读过的，值得你读的
          </h1>
          <p className="mt-3 max-w-[62ch] text-[1rem] text-ink-2">
            不做算法推荐。每个栏目由一位运营或认证大V 挑选并写下推荐理由。
          </p>
        </div>

        {canCurate && (
          <button
            type="button"
            onClick={() => setShowCreate((v) => !v)}
            className="rounded-[6px] bg-seal px-4 py-2 text-[0.86rem] font-semibold text-white hover:bg-seal-deep"
          >
            {showCreate ? "取消" : "+ 新建策展集"}
          </button>
        )}
      </div>

      {showCreate && canCurate && (
        <CreateCollectionForm onCreated={handleCreated} onCancel={() => setShowCreate(false)} />
      )}

      {status === "authenticated" && !canCurate && (
        <p className="mb-4 rounded-[6px] border border-line bg-surface-2 px-3.5 py-2.5 text-[0.8rem] text-ink-2">
          策展资格仅限运营与认证大V。想推荐内容，可以在帖子里 @ 编辑，或先申请认证。
        </p>
      )}

      {error && (
        <p role="alert" className="mb-4 rounded-[6px] border border-danger/40 bg-danger-wash px-3.5 py-2.5 text-[0.82rem] text-danger">
          {error}
        </p>
      )}

      {loading ? (
        <div className="grid gap-3 sm:grid-cols-2" aria-hidden>
          {[0, 1, 2, 3].map((i) => (
            <div key={i} className="h-40 animate-pulse rounded-[12px] bg-surface-2" />
          ))}
        </div>
      ) : collections.length === 0 ? (
        <p className="rounded-[12px] border border-dashed border-line-strong bg-surface px-5 py-14 text-center text-[0.9rem] leading-relaxed text-ink-2">
          还没有上架的策展集。
          {canCurate ? "点右上角新建第一个栏目。" : "编辑们正在准备中。"}
        </p>
      ) : (
        <>
          <ul className="grid gap-3 sm:grid-cols-2">
            {collections.map((collection) => (
              <li key={collection.id}>
                <CollectionCard collection={collection} />
              </li>
            ))}
          </ul>

          {cursor && (
            <button
              type="button"
              onClick={() => void load(cursor)}
              disabled={loadingMore}
              className="mx-auto mt-6 block rounded-[6px] border border-line-strong px-4 py-1.5 text-[0.84rem] font-semibold text-ink hover:border-ink-3 disabled:opacity-60"
            >
              {loadingMore ? "加载中…" : "加载更多"}
            </button>
          )}
        </>
      )}
    </main>
  );
}

function CreateCollectionForm({
  onCreated,
  onCancel,
}: {
  onCreated: (created: CollectionDTO) => void;
  onCancel: () => void;
}) {
  const [title, setTitle] = useState("");
  const [description, setDescription] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    if (!title.trim()) {
      setError("请填写标题。");
      return;
    }
    setSubmitting(true);
    setError(null);
    try {
      const created = await createCollection({
        title: title.trim(),
        description: description.trim() || undefined,
      });
      onCreated(created);
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "创建失败，请稍后重试。");
      setSubmitting(false);
    }
  }

  return (
    <form
      onSubmit={submit}
      className="mb-6 flex flex-col gap-3 rounded-[12px] border border-line bg-surface p-5"
    >
      <h2 className="font-display text-[1.05rem] font-semibold">新建策展集</h2>

      <label className="flex flex-col gap-1.5">
        <span className="font-mono text-[0.7rem] tracking-[0.08em] text-ink-3 uppercase">标题</span>
        <input
          value={title}
          onChange={(e) => setTitle(e.target.value)}
          maxLength={128}
          placeholder="例如：本周必读 · 多模态"
          className="rounded-[6px] border border-line-strong bg-paper px-3 py-2 text-[0.9rem] text-ink focus:border-seal focus:outline-none"
        />
      </label>

      <label className="flex flex-col gap-1.5">
        <span className="font-mono text-[0.7rem] tracking-[0.08em] text-ink-3 uppercase">
          简介（可选）
        </span>
        <textarea
          value={description}
          onChange={(e) => setDescription(e.target.value)}
          maxLength={500}
          rows={3}
          placeholder="用一两句话说明这个栏目为什么值得读。"
          className="resize-y rounded-[6px] border border-line-strong bg-paper px-3 py-2 text-[0.9rem] text-ink focus:border-seal focus:outline-none"
        />
        <span className="self-end font-mono text-[0.68rem] text-ink-3">{description.length}/500</span>
      </label>

      {error && <p className="text-[0.8rem] text-danger">{error}</p>}

      <div className="flex gap-2">
        <button
          type="submit"
          disabled={submitting}
          className="rounded-[6px] bg-seal px-4 py-1.5 text-[0.86rem] font-semibold text-white hover:bg-seal-deep disabled:opacity-60"
        >
          {submitting ? "创建中…" : "创建"}
        </button>
        <button
          type="button"
          onClick={onCancel}
          className="rounded-[6px] border border-line-strong px-4 py-1.5 text-[0.86rem] text-ink-2 hover:border-ink-3"
        >
          取消
        </button>
      </div>
    </form>
  );
}
