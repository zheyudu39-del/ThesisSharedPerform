import Link from "next/link";
import {
  GROUP_ROLE_LABEL,
  JOIN_POLICY_LABEL,
  type GroupDTO,
  type JoinPolicy,
} from "@/lib/groups";

/**
 * 群组列表卡片。
 *
 * 纯展示组件（无 hooks），既能被 Server Component 直接渲染，也能在客户端列表里复用。
 * 结构：封面 / 字标 → 群名 + 加入方式 → 简介 → 成员数 + 我的身份。
 */

export type GroupCardProps = {
  group: GroupDTO;
};

/** 加入方式的视觉层级：公开最轻，仅邀请最克制。 */
const POLICY_STYLE: Record<JoinPolicy, string> = {
  public: "bg-good-wash text-good",
  request: "bg-warn-wash text-warn",
  invite: "bg-surface-2 text-ink-3",
};

export function GroupCard({ group }: GroupCardProps) {
  const initial = [...group.name][0] ?? "群";

  return (
    <Link
      href={`/groups/${encodeURIComponent(group.slug)}`}
      className="group flex h-full gap-4 rounded-[12px] border border-line bg-surface p-4 shadow-[var(--shadow-1)] transition-colors hover:border-line-strong"
    >
      {/* 封面：有图用图，没有则用字标方块，保证列表整齐 */}
      <div className="relative size-14 shrink-0 overflow-hidden rounded-[6px] bg-surface-2">
        {group.coverUrl ? (
          // 封面来自用户上传，尺寸由容器约束；不用 next/image 以免新增远程域名配置。
          // eslint-disable-next-line @next/next/no-img-element
          <img
            src={group.coverUrl}
            alt=""
            className="size-full object-cover"
            loading="lazy"
          />
        ) : (
          <span className="grid size-full place-items-center bg-ink font-display text-xl font-semibold text-paper">
            {initial}
          </span>
        )}
      </div>

      <div className="min-w-0 flex-1">
        <div className="flex flex-wrap items-center gap-2">
          <h3 className="truncate font-display text-[1.05rem] leading-snug font-semibold tracking-[-0.01em] group-hover:underline">
            {group.name}
          </h3>
          <span
            className={`rounded-full px-2 py-0.5 font-mono text-[0.66rem] tracking-[0.04em] ${POLICY_STYLE[group.joinPolicy]}`}
          >
            {JOIN_POLICY_LABEL[group.joinPolicy]}
          </span>
          {group.myRole && (
            <span className="rounded-full border border-line px-2 py-0.5 font-mono text-[0.66rem] tracking-[0.04em] text-ink-2">
              {GROUP_ROLE_LABEL[group.myRole]}
            </span>
          )}
        </div>

        <p className="mt-1 line-clamp-2 min-h-[2.6em] text-[0.85rem] leading-relaxed text-ink-2">
          {group.description || "这个群组还没有简介。"}
        </p>

        <p className="mt-2 flex flex-wrap items-center gap-x-3 gap-y-1 font-mono text-[0.7rem] text-ink-3">
          <span className="tabular-nums">{group.memberCount} 名成员</span>
          {group.ownerName && <span>群主 {group.ownerName}</span>}
        </p>
      </div>
    </Link>
  );
}
