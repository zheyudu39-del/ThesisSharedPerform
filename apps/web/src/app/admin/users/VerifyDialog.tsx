"use client";

import { useEffect, useState } from "react";
import { setAdminUserVerify, type AdminUser, type VerifyType } from "@/lib/admin";
import { ApiError } from "@/lib/http";

/**
 * 认证授予弹窗（A-05）。
 *
 * 为什么用弹窗而不是表格内的下拉：认证需要同时填「类型」和「机构」两个字段，
 * 表格里塞两个控件会让行高失控；而且这是低频、需要谨慎的操作，
 * 独立弹窗能要求运营确认一遍再提交。
 *
 * 授予 influencer 会立刻让该用户获得策展资格（精选流），
 * 因此提交前明确提示这一影响。
 */

const OPTIONS: { value: VerifyType; label: string; hint: string }[] = [
  { value: "none", label: "不认证", hint: "取消现有标识" },
  { value: "scholar", label: "认证学者", hint: "显示「学者」徽章" },
  { value: "influencer", label: "认证大V", hint: "额外获得策展资格" },
];

export function VerifyDialog({
  user,
  onClose,
  onSaved,
}: {
  user: AdminUser;
  onClose: () => void;
  onSaved: (verifyType: string, verifyOrg: string) => void;
}) {
  const [verifyType, setVerifyType] = useState<VerifyType>(
    (user.verifyType as VerifyType) || "none",
  );
  const [verifyOrg, setVerifyOrg] = useState(user.verifyOrg || "");
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // Esc 关闭：弹窗是模态的，不给键盘用户留出口会很别扭。
  useEffect(() => {
    function onKey(e: KeyboardEvent) {
      if (e.key === "Escape") onClose();
    }
    document.addEventListener("keydown", onKey);
    return () => document.removeEventListener("keydown", onKey);
  }, [onClose]);

  async function handleSave() {
    setError(null);
    const org = verifyType === "none" ? "" : verifyOrg.trim();
    if (verifyType !== "none" && !org) {
      setError("请填写认证机构。");
      return;
    }
    setSaving(true);
    try {
      await setAdminUserVerify(user.id, verifyType, org);
      onSaved(verifyType, org);
      onClose();
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "保存失败，请稍后重试。");
    } finally {
      setSaving(false);
    }
  }

  return (
    <div
      className="fixed inset-0 z-50 flex items-center justify-center bg-ink/40 p-4"
      role="dialog"
      aria-modal="true"
      aria-labelledby="verify-title"
      onClick={(e) => {
        // 点遮罩关闭；点内容区不关闭。
        if (e.target === e.currentTarget) onClose();
      }}
    >
      <div className="w-full max-w-md rounded-[12px] border border-line bg-surface p-5 shadow-[var(--shadow-2,0_12px_32px_rgba(0,0,0,0.18))]">
        <h2 id="verify-title" className="font-display text-[1.1rem] font-semibold">
          认证标识
        </h2>
        <p className="mt-1 font-mono text-[0.72rem] text-ink-3">
          @{user.handle} · {user.name}
        </p>

        <fieldset className="mt-4 flex flex-col gap-1.5">
          <legend className="mb-1.5 font-mono text-[0.66rem] tracking-[0.06em] text-ink-3 uppercase">
            标识类型
          </legend>
          {OPTIONS.map((opt) => (
            <label
              key={opt.value}
              className={`flex cursor-pointer items-start gap-2.5 rounded-[6px] border px-3 py-2 transition-colors ${
                verifyType === opt.value
                  ? "border-seal bg-seal-wash"
                  : "border-line bg-surface hover:border-line-strong"
              }`}
            >
              <input
                type="radio"
                name="verify-type"
                value={opt.value}
                checked={verifyType === opt.value}
                onChange={() => setVerifyType(opt.value)}
                className="mt-0.5 size-3.5 accent-[var(--color-seal)]"
              />
              <span className="min-w-0">
                <span className="block text-[0.84rem] font-medium text-ink">{opt.label}</span>
                <span className="block font-mono text-[0.66rem] text-ink-3">{opt.hint}</span>
              </span>
            </label>
          ))}
        </fieldset>

        {verifyType !== "none" && (
          <label className="mt-3 flex flex-col gap-1">
            <span className="font-mono text-[0.66rem] tracking-[0.06em] text-ink-3 uppercase">
              认证机构
            </span>
            <input
              value={verifyOrg}
              onChange={(e) => setVerifyOrg(e.target.value)}
              maxLength={128}
              placeholder="例如：清华大学计算机系"
              className="w-full rounded-[6px] border border-line-strong bg-surface px-3 py-2 text-[0.84rem] text-ink placeholder:text-ink-3 focus:border-seal focus:outline-none"
            />
          </label>
        )}

        {verifyType === "influencer" && (
          <p className="mt-3 rounded-[6px] border border-warn/40 bg-warn-wash px-3 py-2 text-[0.76rem] leading-relaxed text-warn">
            授予大V后，该用户将立即获得<b>策展资格</b>——可以创建并维护精选集。
            请确认这是你要的权限。
          </p>
        )}

        {error && (
          <p role="alert" className="mt-3 rounded-[6px] border border-danger/40 bg-danger-wash px-3 py-2 text-[0.8rem] text-danger">
            {error}
          </p>
        )}

        <div className="mt-4 flex justify-end gap-2">
          <button
            type="button"
            onClick={onClose}
            className="rounded-[6px] border border-line-strong px-3.5 py-1.5 text-[0.82rem] text-ink-2 hover:text-ink"
          >
            取消
          </button>
          <button
            type="button"
            onClick={() => void handleSave()}
            disabled={saving}
            className="rounded-[6px] bg-seal px-4 py-1.5 text-[0.82rem] font-semibold text-white hover:bg-seal-deep disabled:opacity-50"
          >
            {saving ? "保存中…" : "保存"}
          </button>
        </div>
      </div>
    </div>
  );
}
