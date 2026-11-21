import Link from "next/link";
import type { CollectionDTO } from "@/lib/collections";

/**
 * 策展集卡片。
 *
 * 精选流的核心单元是「集合」而不是单条帖子：编辑用一句话说明为什么
 * 这一组内容值得读。卡片因此以标题 + 编者按为主，条目数作为次要信息。
 */
export function CollectionCard({ collection }: { collection: CollectionDTO }) {
  return (
    <Link
      href={`/collections/${collection.slug}`}
      className="group flex flex-col gap-3 rounded-[12px] border border-line bg-surface p-5 transition-colors hover:border-line-strong"
    >
      <div className="flex items-start justify-between gap-3">
        <h3 className="font-display text-[1.12rem] leading-snug font-semibold tracking-[-0.01em] group-hover:text-seal">
          {collection.title}
        </h3>
        <span className="shrink-0 rounded-full bg-surface-2 px-2 py-0.5 font-mono text-[0.66rem] text-ink-3">
          {collection.itemCount} 篇
        </span>
      </div>

      {collection.description && (
        <p className="line-clamp-3 text-[0.86rem] leading-relaxed text-ink-2">
          {collection.description}
        </p>
      )}

      <div className="mt-auto flex items-center gap-2 font-mono text-[0.68rem] text-ink-3">
        <span>策展 · {collection.curator.name}</span>
        {collection.curator.verifyType === "influencer" && (
          <span className="rounded-full bg-seal-wash px-1.5 py-px text-seal">大V</span>
        )}
        <span className="ml-auto">{formatDate(collection.startsAt)}</span>
      </div>
    </Link>
  );
}

function formatDate(iso: string): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return "";
  return `${d.getFullYear()}/${d.getMonth() + 1}/${d.getDate()}`;
}
