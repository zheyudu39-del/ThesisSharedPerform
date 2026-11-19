/**
 * 朱印身份标识：认证学者 / 大V。
 * 对应 PRD「认证标识」与「大V 标识」，是页面里唯一使用印红的高饱和元素。
 */

type Variant = "scholar" | "influencer";

const GLYPH: Record<Variant, string> = {
  scholar: "認",
  influencer: "★",
};

const LABEL: Record<Variant, string> = {
  scholar: "认证学者",
  influencer: "大V",
};

export function SealBadge({ variant = "scholar" }: { variant?: Variant }) {
  return (
    <span className="inline-flex items-center gap-1.5 rounded-full bg-seal-wash py-0.5 pr-2 pl-1 text-[0.72rem] font-semibold text-seal">
      <span className="grid size-3.5 place-items-center rounded-[2px] bg-seal font-display text-[9px] leading-none font-semibold text-white shadow-[inset_0_0_0_1px_rgba(255,255,255,0.3)]">
        {GLYPH[variant]}
      </span>
      {LABEL[variant]}
    </span>
  );
}
