"use client";

import Link from "next/link";
import { useEffect, useId, useRef, useState, type FormEvent } from "react";
import { ApiKeyPanel } from "@/components/ApiKeyPanel";
import { useAuth } from "@/components/AuthProvider";
import { RequireAuth } from "@/components/RequireAuth";
import { SiteHeader } from "@/components/SiteHeader";
import { AuthError, type UserDTO } from "@/lib/auth";
import { PROFILE_LIMITS, updateProfile } from "@/lib/users";

/**
 * 账号设置：编辑显示名 / 简介 / 机构 / 研究方向。
 *
 * - 需登录：整页包在 RequireAuth 内，未登录会被带到登录页并在登录后回跳；
 * - 研究方向用逗号分隔输入（中文逗号、英文逗号、换行都接受），保存时拆成数组；
 * - 字符计数与后端限制一致（name ≤ 64、bio ≤ 280、institution ≤ 128、
 *   研究方向 ≤ 10 项且每项 ≤ 64），按 Unicode 码点计数；
 * - 保存成功后调用 AuthProvider.refresh()，顶栏等处立即用上新资料。
 */

const { nameMax, bioMax, institutionMax, researchFieldsMax, researchFieldMax } = PROFILE_LIMITS;

/** 按 Unicode 码点计数，与后端 utf8.RuneCountInString 对齐。 */
function runeLength(value: string): number {
  return [...value].length;
}

/** 研究方向输入 → 去重后的数组（中英文逗号、分号、换行均视为分隔符）。 */
function parseResearchFields(raw: string): string[] {
  const seen = new Set<string>();
  const result: string[] = [];
  for (const part of raw.split(/[,，;；\n]/)) {
    const field = part.trim();
    if (!field) continue;
    if (seen.has(field)) continue;
    seen.add(field);
    result.push(field);
  }
  return result;
}

type FieldErrors = {
  name?: string;
  bio?: string;
  institution?: string;
  researchFields?: string;
};

export default function SettingsPage() {
  return (
    <>
      <SiteHeader />
      <RequireAuth>
        <SettingsForm />
      </RequireAuth>
    </>
  );
}

function SettingsForm() {
  const { user, refresh } = useAuth();

  // 首次拿到用户后填充表单；之后不随 refresh 覆盖用户正在编辑的内容。
  const [name, setName] = useState("");
  const [bio, setBio] = useState("");
  const [institution, setInstitution] = useState("");
  const [fieldsInput, setFieldsInput] = useState("");
  const [initialized, setInitialized] = useState(false);

  const [fieldErrors, setFieldErrors] = useState<FieldErrors>({});
  const [formError, setFormError] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);

  const nameRef = useRef<HTMLInputElement>(null);
  const bioRef = useRef<HTMLTextAreaElement>(null);
  const institutionRef = useRef<HTMLInputElement>(null);
  const fieldsRef = useRef<HTMLInputElement>(null);

  const formId = useId();

  useEffect(() => {
    if (initialized || !user) return;
    setName(user.name);
    setBio(user.bio);
    setInstitution(user.institution);
    setFieldsInput(user.researchFields.join(", "));
    setInitialized(true);
  }, [user, initialized]);

  // 成功提示几秒后收起，不干扰继续编辑。
  useEffect(() => {
    if (!notice) return;
    const timer = window.setTimeout(() => setNotice(null), 4000);
    return () => window.clearTimeout(timer);
  }, [notice]);

  const researchFields = parseResearchFields(fieldsInput);
  const nameLength = runeLength(name);
  const bioLength = runeLength(bio);
  const institutionLength = runeLength(institution);

  function validate(): FieldErrors {
    const errors: FieldErrors = {};

    if (!name.trim()) {
      errors.name = "显示名不能为空。";
    } else if (nameLength > nameMax) {
      errors.name = `显示名最多 ${nameMax} 个字符。`;
    }

    if (bioLength > bioMax) {
      errors.bio = `简介最多 ${bioMax} 个字符。`;
    }

    if (institutionLength > institutionMax) {
      errors.institution = `机构名称最多 ${institutionMax} 个字符。`;
    }

    if (researchFields.length > researchFieldsMax) {
      errors.researchFields = `研究方向最多 ${researchFieldsMax} 项。`;
    } else if (researchFields.some((f) => runeLength(f) > researchFieldMax)) {
      errors.researchFields = `每项研究方向最多 ${researchFieldMax} 个字符。`;
    }

    return errors;
  }

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (saving) return;

    setFormError(null);
    setNotice(null);

    const errors = validate();
    setFieldErrors(errors);
    if (Object.keys(errors).length > 0) {
      focusFirstInvalid(errors, { nameRef, bioRef, institutionRef, fieldsRef });
      return;
    }

    setSaving(true);
    try {
      const updated = await updateProfile({
        name: name.trim(),
        bio: bio.trim(),
        institution: institution.trim(),
        researchFields,
      });
      applyUpdatedUser(updated, { setName, setBio, setInstitution, setFieldsInput });
      await refresh();
      setNotice("已保存。");
    } catch (err) {
      if (err instanceof AuthError && err.status === 422) {
        setFormError(err.message || "提交的资料未通过校验。");
      } else {
        setFormError(err instanceof AuthError ? err.message : "保存失败，请稍后重试。");
      }
    } finally {
      setSaving(false);
    }
  }

  return (
    <main className="mx-auto w-full max-w-2xl px-5 py-10">
      <div className="mb-8 border-b border-line pb-6">
        <p className="mb-2 font-mono text-[0.72rem] tracking-[0.16em] text-ink-3 uppercase">
          设置 · Settings
        </p>
        <h1 className="font-display text-[clamp(1.6rem,4vw,2.1rem)] leading-tight font-semibold tracking-[-0.02em]">
          编辑个人资料
        </h1>
        <p className="mt-2 text-[0.9rem] leading-relaxed text-ink-2">
          这些信息会显示在你的公开主页上。邮箱与句柄暂不支持修改。
        </p>
      </div>

      <form onSubmit={handleSubmit} className="flex flex-col gap-6" noValidate>
        <FormField
          id={`${formId}-name`}
          label="显示名"
          hint={`${nameLength} / ${nameMax}`}
          error={fieldErrors.name}
          hintTone={nameLength > nameMax ? "danger" : "muted"}
        >
          <input
            ref={nameRef}
            id={`${formId}-name`}
            name="name"
            value={name}
            onChange={(e) => {
              setName(e.target.value);
              if (fieldErrors.name) setFieldErrors((prev) => ({ ...prev, name: undefined }));
            }}
            autoComplete="name"
            disabled={saving}
            aria-invalid={fieldErrors.name ? true : undefined}
            aria-describedby={fieldErrors.name ? `${formId}-name-error` : undefined}
            className={inputClass(Boolean(fieldErrors.name))}
          />
        </FormField>

        <FormField
          id={`${formId}-bio`}
          label="简介"
          hint={`${bioLength} / ${bioMax}`}
          error={fieldErrors.bio}
          hintTone={bioLength > bioMax ? "danger" : "muted"}
        >
          <textarea
            ref={bioRef}
            id={`${formId}-bio`}
            name="bio"
            value={bio}
            onChange={(e) => {
              setBio(e.target.value);
              if (fieldErrors.bio) setFieldErrors((prev) => ({ ...prev, bio: undefined }));
            }}
            rows={4}
            placeholder="一句话介绍你的研究方向或正在做的事。"
            disabled={saving}
            aria-invalid={fieldErrors.bio ? true : undefined}
            aria-describedby={fieldErrors.bio ? `${formId}-bio-error` : undefined}
            className={`${inputClass(Boolean(fieldErrors.bio))} resize-y leading-relaxed`}
          />
        </FormField>

        <FormField
          id={`${formId}-institution`}
          label="机构"
          hint={`${institutionLength} / ${institutionMax}`}
          error={fieldErrors.institution}
          hintTone={institutionLength > institutionMax ? "danger" : "muted"}
        >
          <input
            ref={institutionRef}
            id={`${formId}-institution`}
            name="institution"
            value={institution}
            onChange={(e) => {
              setInstitution(e.target.value);
              if (fieldErrors.institution)
                setFieldErrors((prev) => ({ ...prev, institution: undefined }));
            }}
            placeholder="例如：某某大学 计算机学院"
            disabled={saving}
            aria-invalid={fieldErrors.institution ? true : undefined}
            aria-describedby={fieldErrors.institution ? `${formId}-institution-error` : undefined}
            className={inputClass(Boolean(fieldErrors.institution))}
          />
        </FormField>

        <FormField
          id={`${formId}-fields`}
          label="研究方向"
          hint={`${researchFields.length} / ${researchFieldsMax}`}
          error={fieldErrors.researchFields}
          hintTone={researchFields.length > researchFieldsMax ? "danger" : "muted"}
          help="用逗号分隔，例如：自然语言处理, 多模态学习。"
        >
          <input
            ref={fieldsRef}
            id={`${formId}-fields`}
            name="researchFields"
            value={fieldsInput}
            onChange={(e) => {
              setFieldsInput(e.target.value);
              if (fieldErrors.researchFields)
                setFieldErrors((prev) => ({ ...prev, researchFields: undefined }));
            }}
            placeholder="自然语言处理, 多模态学习"
            disabled={saving}
            aria-invalid={fieldErrors.researchFields ? true : undefined}
            aria-describedby={[
              `${formId}-fields-help`,
              fieldErrors.researchFields ? `${formId}-fields-error` : null,
            ]
              .filter(Boolean)
              .join(" ")}
            className={inputClass(Boolean(fieldErrors.researchFields))}
          />
        </FormField>

        {/* 解析预览：让用户看到逗号分隔会变成哪些标签 */}
        {researchFields.length > 0 && (
          <div className="-mt-3 flex flex-wrap gap-2" aria-live="polite">
            {researchFields.map((field) => (
              <span
                key={field}
                className="rounded-full border border-line bg-surface px-3 py-1 text-[0.76rem] text-ink-2"
              >
                {field}
              </span>
            ))}
          </div>
        )}

        {formError && (
          <div
            role="alert"
            aria-live="assertive"
            className="flex items-start gap-2.5 rounded-[6px] border border-danger/45 bg-danger-wash px-3.5 py-3 text-[0.85rem] leading-relaxed text-danger"
          >
            <span aria-hidden className="mt-0.5 font-mono font-semibold">
              !
            </span>
            <span>{formError}</span>
          </div>
        )}

        {notice && (
          <p
            role="status"
            className="rounded-[6px] border border-good/40 bg-good-wash px-3.5 py-2.5 text-[0.85rem] text-good"
          >
            {notice}
          </p>
        )}

        <div className="flex flex-wrap items-center gap-3 border-t border-line pt-5">
          <button
            type="submit"
            disabled={saving}
            aria-busy={saving}
            className="inline-flex items-center justify-center gap-2 rounded-[6px] bg-seal px-5 py-2 text-[0.9rem] font-semibold text-white shadow-[var(--shadow-1)] transition-colors hover:bg-seal-deep disabled:cursor-not-allowed disabled:opacity-60"
          >
            {saving && (
              <span
                aria-hidden
                className="size-3.5 animate-spin rounded-full border-2 border-white/35 border-t-white"
              />
            )}
            {saving ? "保存中…" : "保存"}
          </button>

          {user && (
            <Link
              href={`/u/${encodeURIComponent(user.handle)}`}
              className="rounded-[6px] border border-line-strong px-4 py-2 text-[0.88rem] font-semibold text-ink hover:border-ink-3"
            >
              查看公开主页
            </Link>
          )}
        </div>
      </form>

      {/* 开放 API：与个人资料无关，单独成块，避免混进资料表单的提交逻辑。 */}
      <div className="mt-10">
        <ApiKeyPanel />
      </div>
    </main>
  );
}

/* -------------------------------------------------------------------------- */
/* 构件                                                                        */
/* -------------------------------------------------------------------------- */

function inputClass(hasError: boolean): string {
  return `w-full rounded-[6px] border bg-surface px-3 py-2.5 text-[0.95rem] text-ink placeholder:text-ink-3 transition-colors focus:outline-none disabled:cursor-not-allowed disabled:opacity-60 ${
    hasError
      ? "border-danger focus:border-danger"
      : "border-line-strong hover:border-ink-3 focus:border-seal"
  }`;
}

function FormField({
  id,
  label,
  hint,
  hintTone = "muted",
  error,
  help,
  children,
}: {
  id: string;
  label: string;
  hint?: string;
  hintTone?: "muted" | "danger";
  error?: string;
  help?: string;
  children: React.ReactNode;
}) {
  return (
    <div className="flex flex-col gap-1.5">
      <div className="flex items-baseline justify-between gap-3">
        <label htmlFor={id} className="text-[0.88rem] font-semibold text-ink">
          {label}
        </label>
        {hint && (
          <span
            className={`font-mono text-[0.7rem] tabular-nums ${
              hintTone === "danger" ? "text-danger" : "text-ink-3"
            }`}
          >
            {hint}
          </span>
        )}
      </div>

      {children}

      {help && !error && (
        <p id={`${id}-help`} className="text-[0.76rem] text-ink-3">
          {help}
        </p>
      )}

      {error && (
        <p id={`${id}-error`} role="alert" className="flex items-start gap-1.5 text-[0.78rem] font-medium text-danger">
          <span aria-hidden className="mt-px font-mono leading-none">
            !
          </span>
          <span>{error}</span>
        </p>
      )}
    </div>
  );
}

function focusFirstInvalid(
  errors: FieldErrors,
  refs: {
    nameRef: React.RefObject<HTMLInputElement | null>;
    bioRef: React.RefObject<HTMLTextAreaElement | null>;
    institutionRef: React.RefObject<HTMLInputElement | null>;
    fieldsRef: React.RefObject<HTMLInputElement | null>;
  },
) {
  if (errors.name) refs.nameRef.current?.focus();
  else if (errors.bio) refs.bioRef.current?.focus();
  else if (errors.institution) refs.institutionRef.current?.focus();
  else if (errors.researchFields) refs.fieldsRef.current?.focus();
}

/** 用服务端返回的完整 DTO 回填表单，避免显示与服务端不一致的旧值。 */
function applyUpdatedUser(
  updated: UserDTO,
  setters: {
    setName: (v: string) => void;
    setBio: (v: string) => void;
    setInstitution: (v: string) => void;
    setFieldsInput: (v: string) => void;
  },
) {
  setters.setName(updated.name);
  setters.setBio(updated.bio ?? "");
  setters.setInstitution(updated.institution ?? "");
  setters.setFieldsInput((updated.researchFields ?? []).join(", "));
}
