"use client";

import { useRouter } from "next/navigation";
import { useId, useState, type FormEvent } from "react";
import { AuthError } from "@/lib/auth";
import {
  createGroup,
  GROUP_LIMITS,
  JOIN_POLICY_HINT,
  JOIN_POLICY_LABEL,
  type JoinPolicy,
} from "@/lib/groups";

/**
 * 创建群组表单。
 *
 * - 名称 1..64、简介 ≤500（与后端校验一致，按 Unicode 码点计数）；
 * - 加入方式三选一，默认「公开加入」，便于新群组被列表发现；
 * - 创建成功后直接跳到群组主页（群主视角会看到管理面板）。
 */

const POLICIES: JoinPolicy[] = ["public", "request", "invite"];

function runeLength(value: string): number {
  return [...value].length;
}

export function CreateGroupForm() {
  const router = useRouter();
  const fieldId = useId();

  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [joinPolicy, setJoinPolicy] = useState<JoinPolicy>("public");
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [fieldErrors, setFieldErrors] = useState<{ name?: string; description?: string }>({});

  const nameLength = runeLength(name.trim());
  const descriptionLength = runeLength(description);

  function validate(): boolean {
    const errors: { name?: string; description?: string } = {};
    if (nameLength === 0) errors.name = "请输入群组名称。";
    else if (nameLength > GROUP_LIMITS.nameMax) {
      errors.name = `群组名称最多 ${GROUP_LIMITS.nameMax} 个字符。`;
    }
    if (descriptionLength > GROUP_LIMITS.descriptionMax) {
      errors.description = `简介最多 ${GROUP_LIMITS.descriptionMax} 个字符。`;
    }
    setFieldErrors(errors);
    return Object.keys(errors).length === 0;
  }

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (submitting) return;
    setError(null);
    if (!validate()) return;

    setSubmitting(true);
    try {
      const group = await createGroup({
        name: name.trim(),
        description: description.trim() || undefined,
        joinPolicy,
      });
      router.push(`/groups/${encodeURIComponent(group.slug)}`);
    } catch (err) {
      if (err instanceof AuthError) {
        setError(err.status === 422 ? "提交的信息未通过校验，请检查名称与简介。" : err.message);
      } else {
        setError("创建失败，请稍后重试。");
      }
      setSubmitting(false);
    }
  }

  return (
    <form onSubmit={handleSubmit} className="flex flex-col gap-6" noValidate>
      {/* 名称 */}
      <div>
        <div className="mb-1.5 flex items-baseline justify-between gap-2">
          <label
            htmlFor={`${fieldId}-name`}
            className="font-mono text-[0.7rem] tracking-[0.1em] text-ink-3 uppercase"
          >
            群组名称
          </label>
          <span
            className={`font-mono text-[0.68rem] tabular-nums ${
              nameLength > GROUP_LIMITS.nameMax ? "text-danger" : "text-ink-3"
            }`}
          >
            {nameLength}/{GROUP_LIMITS.nameMax}
          </span>
        </div>
        <input
          id={`${fieldId}-name`}
          value={name}
          onChange={(e) => setName(e.target.value)}
          disabled={submitting}
          aria-invalid={Boolean(fieldErrors.name)}
          placeholder="例如：图神经网络共读小组"
          className="w-full rounded-[6px] border border-line-strong bg-surface px-3.5 py-2.5 text-[0.92rem] text-ink placeholder:text-ink-3 focus:border-seal focus:outline-none disabled:opacity-60"
        />
        {fieldErrors.name && (
          <p role="alert" className="mt-1.5 text-[0.78rem] text-danger">
            {fieldErrors.name}
          </p>
        )}
      </div>

      {/* 简介 */}
      <div>
        <div className="mb-1.5 flex items-baseline justify-between gap-2">
          <label
            htmlFor={`${fieldId}-description`}
            className="font-mono text-[0.7rem] tracking-[0.1em] text-ink-3 uppercase"
          >
            简介（可选）
          </label>
          <span
            className={`font-mono text-[0.68rem] tabular-nums ${
              descriptionLength > GROUP_LIMITS.descriptionMax ? "text-danger" : "text-ink-3"
            }`}
          >
            {descriptionLength}/{GROUP_LIMITS.descriptionMax}
          </span>
        </div>
        <textarea
          id={`${fieldId}-description`}
          value={description}
          onChange={(e) => setDescription(e.target.value)}
          disabled={submitting}
          rows={4}
          aria-invalid={Boolean(fieldErrors.description)}
          placeholder="这个群组关注什么方向？打算怎样协作？"
          className="w-full resize-y rounded-[6px] border border-line-strong bg-surface px-3.5 py-2.5 text-[0.9rem] leading-relaxed text-ink placeholder:text-ink-3 focus:border-seal focus:outline-none disabled:opacity-60"
        />
        {fieldErrors.description && (
          <p role="alert" className="mt-1.5 text-[0.78rem] text-danger">
            {fieldErrors.description}
          </p>
        )}
      </div>

      {/* 加入方式 */}
      <fieldset>
        <legend className="mb-2 font-mono text-[0.7rem] tracking-[0.1em] text-ink-3 uppercase">
          加入方式
        </legend>
        <div className="flex flex-col gap-2">
          {POLICIES.map((policy) => {
            const checked = joinPolicy === policy;
            return (
              <label
                key={policy}
                className={`flex cursor-pointer items-start gap-3 rounded-[6px] border px-3.5 py-3 transition-colors ${
                  checked
                    ? "border-seal bg-seal-wash/40"
                    : "border-line bg-surface hover:border-line-strong"
                }`}
              >
                <input
                  type="radio"
                  name="joinPolicy"
                  value={policy}
                  checked={checked}
                  onChange={() => setJoinPolicy(policy)}
                  disabled={submitting}
                  className="mt-1 accent-[var(--color-seal)]"
                />
                <span className="min-w-0">
                  <span className="block text-[0.88rem] font-semibold text-ink">
                    {JOIN_POLICY_LABEL[policy]}
                  </span>
                  <span className="mt-0.5 block text-[0.8rem] leading-relaxed text-ink-2">
                    {JOIN_POLICY_HINT[policy]}
                  </span>
                </span>
              </label>
            );
          })}
        </div>
      </fieldset>

      {error && (
        <div
          role="alert"
          className="flex items-start gap-2.5 rounded-[6px] border border-danger/45 bg-danger-wash px-3.5 py-3 text-[0.85rem] leading-relaxed text-danger"
        >
          <span aria-hidden className="mt-0.5 font-mono font-semibold">
            !
          </span>
          <span>{error}</span>
        </div>
      )}

      <div className="flex flex-wrap items-center gap-2 border-t border-line pt-5">
        <button
          type="submit"
          disabled={submitting}
          aria-busy={submitting}
          className="inline-flex items-center justify-center gap-2 rounded-[6px] bg-seal px-5 py-2.5 text-[0.9rem] font-semibold text-white shadow-[var(--shadow-1)] transition-colors hover:bg-seal-deep disabled:cursor-not-allowed disabled:opacity-50"
        >
          {submitting && (
            <span
              aria-hidden
              className="size-3.5 animate-spin rounded-full border-2 border-white/35 border-t-white"
            />
          )}
          {submitting ? "创建中…" : "创建群组"}
        </button>
      </div>
    </form>
  );
}
