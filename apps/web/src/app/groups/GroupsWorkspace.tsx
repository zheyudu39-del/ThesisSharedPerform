"use client";

import Link from "next/link";
import { useCallback, useEffect, useRef, useState } from "react";
import { useAuth } from "@/components/AuthProvider";
import { GroupCard } from "@/components/GroupCard";
import { AuthError } from "@/lib/auth";
import { listGroups, type GroupDTO } from "@/lib/groups";

/**
 * 群组列表工作区（客户端）。
 *
 * 公开群组列表 + 游标翻页；顶部提供「创建群组」入口（需登录）。
 * 列表本身匿名可读，因此不套 RequireAuth；只有创建动作要求登录。
 */
export function GroupsWorkspace() {
  const { status } = useAuth();
  const [groups, setGroups] = useState<GroupDTO[]>([]);
  const [nextCursor, setNextCursor] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const [loadingMore, setLoadingMore] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const mounted = useRef(true);
  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
    };
  }, []);

  const load = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const page = await listGroups(null, 20);
      if (!mounted.current) return;
      setGroups(page.groups);
      setNextCursor(page.nextCursor);
    } catch (err) {
      if (!mounted.current) return;
      setError(err instanceof AuthError ? err.message : "群组列表加载失败，请稍后重试。");
    } finally {
      if (mounted.current) setLoading(false);
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  async function loadMore() {
    if (!nextCursor || loadingMore) return;
    setLoadingMore(true);
    try {
      const page = await listGroups(nextCursor, 20);
      if (!mounted.current) return;
      setGroups((prev) => {
        const seen = new Set(prev.map((g) => g.id));
        return [...prev, ...page.groups.filter((g) => !seen.has(g.id))];
      });
      setNextCursor(page.nextCursor);
    } catch (err) {
      if (!mounted.current) return;
      setError(err instanceof AuthError ? err.message : "加载更多失败，请稍后重试。");
    } finally {
      if (mounted.current) setLoadingMore(false);
    }
  }

  return (
    <main className="mx-auto w-full max-w-4xl px-5 py-10">
      <div className="mb-8 flex flex-wrap items-end justify-between gap-4 border-b border-line pb-7">
        <div className="min-w-0">
          <p className="mb-3 font-mono text-[0.72rem] tracking-[0.16em] text-ink-3 uppercase">
            群组 · Groups
          </p>
          <h1 className="font-display text-[clamp(1.7rem,4vw,2.3rem)] leading-[1.08] font-semibold tracking-[-0.03em] text-balance">
            与同行一起读、一起写
          </h1>
          <p className="mt-3 max-w-[58ch] text-[0.95rem] leading-relaxed text-ink-2">
            群组把关注同一个方向的学者聚在一起：共享文献、讨论方法、协作成稿。
            下面列出所有公开群组，可直接加入。
          </p>
        </div>

        <Link
          href="/groups/new"
          className="inline-flex shrink-0 items-center gap-1.5 rounded-[6px] bg-seal px-4 py-2 text-[0.88rem] font-semibold text-white shadow-[var(--shadow-1)] transition-colors hover:bg-seal-deep"
        >
          <PlusIcon />
          创建群组
        </Link>
      </div>

      {error && (
        <div
          role="alert"
          className="mb-6 flex items-start gap-2.5 rounded-[6px] border border-danger/45 bg-danger-wash px-3.5 py-3 text-[0.85rem] leading-relaxed text-danger"
        >
          <span aria-hidden className="mt-0.5 font-mono font-semibold">
            !
          </span>
          <span className="flex-1">{error}</span>
          <button
            type="button"
            onClick={() => void load()}
            className="shrink-0 font-semibold underline underline-offset-2"
          >
            重试
          </button>
        </div>
      )}

      {loading ? (
        <div className="flex flex-col gap-3" aria-hidden>
          {[0, 1, 2].map((i) => (
            <div key={i} className="h-[104px] animate-pulse rounded-[12px] bg-surface-2" />
          ))}
        </div>
      ) : groups.length === 0 ? (
        <div className="rounded-[12px] border border-dashed border-line-strong bg-surface px-5 py-12 text-center">
          <p className="font-display text-[1.1rem] font-semibold">还没有公开群组</p>
          <p className="mx-auto mt-2 max-w-[40ch] text-[0.88rem] leading-relaxed text-ink-2">
            {status === "authenticated"
              ? "成为第一个创建群组的人，把同方向的学者聚到一起。"
              : "登录后可以创建群组，把同方向的学者聚到一起。"}
          </p>
          <Link
            href="/groups/new"
            className="mt-5 inline-flex rounded-[6px] bg-seal px-4 py-1.5 text-[0.85rem] font-semibold text-white hover:bg-seal-deep"
          >
            创建群组
          </Link>
        </div>
      ) : (
        <>
          <ul className="flex flex-col gap-3">
            {groups.map((group) => (
              <li key={group.id}>
                <GroupCard group={group} />
              </li>
            ))}
          </ul>

          {nextCursor && (
            <button
              type="button"
              onClick={loadMore}
              disabled={loadingMore}
              className="mt-5 w-full rounded-[6px] border border-line-strong py-2.5 text-[0.85rem] font-semibold text-ink transition-colors hover:border-ink-3 disabled:cursor-not-allowed disabled:opacity-60"
            >
              {loadingMore ? "加载中…" : "加载更多群组"}
            </button>
          )}
        </>
      )}

      <p className="mt-8 font-mono text-[0.72rem] leading-relaxed text-ink-3">
        这里只展示公开群组。申请加入与仅邀请的群组需要通过邀请码或群内成员分享的链接进入。
      </p>
    </main>
  );
}

function PlusIcon() {
  return (
    <svg
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="2"
      strokeLinecap="round"
      className="size-4"
      aria-hidden
    >
      <path d="M12 5v14M5 12h14" />
    </svg>
  );
}
