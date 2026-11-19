"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { useState, type FormEvent } from "react";
import { AuthField } from "@/components/AuthField";
import {
  AuthError,
  register,
  validateRegister,
  type AuthFieldErrors,
} from "@/lib/auth";

/**
 * 注册表单（客户端）。
 * 409 中的 email_taken / handle_taken 归到对应字段，其余错误落到表单级提示。
 */
export function RegisterForm() {
  const router = useRouter();

  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [name, setName] = useState("");
  const [handle, setHandle] = useState("");
  const [fieldErrors, setFieldErrors] = useState<AuthFieldErrors>({});
  const [formError, setFormError] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (submitting) return;

    setFormError(null);
    const errors = validateRegister({ email, password, name, handle });
    setFieldErrors(errors);
    if (Object.keys(errors).length > 0) {
      focusFirstInvalid(errors);
      return;
    }

    setSubmitting(true);
    try {
      await register(email, password, name, handle);
      router.push("/");
      router.refresh();
    } catch (error) {
      if (error instanceof AuthError) {
        const field = codeToField(error.code);
        if (field) {
          setFieldErrors({ [field]: error.message });
          document.getElementById(`register-${field}`)?.focus();
        } else {
          setFormError(error.message);
        }
      } else {
        setFormError("注册失败，请稍后重试。");
      }
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <form noValidate onSubmit={handleSubmit} className="flex flex-col gap-5">
      {formError && (
        <div
          role="alert"
          aria-live="assertive"
          className="flex items-start gap-2.5 rounded-[6px] border border-danger/45 bg-danger-wash px-3.5 py-3 text-[0.85rem] leading-relaxed text-danger"
        >
          <span aria-hidden="true" className="mt-0.5 font-mono text-[0.8rem] font-semibold">
            !
          </span>
          <span>{formError}</span>
        </div>
      )}

      <AuthField
        id="register-email"
        name="email"
        label="邮箱"
        type="email"
        inputMode="email"
        autoComplete="email"
        placeholder="name@example.com"
        value={email}
        onChange={(e) => setEmail(e.target.value)}
        error={fieldErrors.email}
        disabled={submitting}
        autoFocus
      />

      <AuthField
        id="register-password"
        name="password"
        label="密码"
        type="password"
        autoComplete="new-password"
        placeholder="至少 8 位字符"
        hint="≥ 8 位"
        value={password}
        onChange={(e) => setPassword(e.target.value)}
        error={fieldErrors.password}
        disabled={submitting}
      />

      <AuthField
        id="register-name"
        name="name"
        label="显示名"
        type="text"
        autoComplete="name"
        placeholder="例如 林正则"
        hint="1–64 字"
        maxLength={64}
        value={name}
        onChange={(e) => setName(e.target.value)}
        error={fieldErrors.name}
        disabled={submitting}
      />

      <AuthField
        id="register-handle"
        name="handle"
        label="句柄"
        type="text"
        autoComplete="username"
        placeholder="例如 linzz"
        hint="3–32 · a–z 0–9 _ -"
        maxLength={32}
        autoCapitalize="none"
        autoCorrect="off"
        spellCheck={false}
        value={handle}
        onChange={(e) => setHandle(e.target.value)}
        error={fieldErrors.handle}
        disabled={submitting}
      />

      <div className="flex flex-col gap-3 pt-1">
        <button
          type="submit"
          disabled={submitting}
          aria-busy={submitting}
          className="inline-flex w-full items-center justify-center gap-2 rounded-[6px] bg-seal px-4 py-2.5 text-[0.95rem] font-semibold text-white shadow-[var(--shadow-1)] transition-colors duration-150 hover:bg-seal-deep focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-seal disabled:cursor-not-allowed disabled:opacity-60"
        >
          {submitting && (
            <span
              aria-hidden="true"
              className="size-3.5 animate-spin rounded-full border-2 border-white/35 border-t-white"
            />
          )}
          {submitting ? "正在创建账号…" : "创建账号"}
        </button>

        <p className="text-center text-[0.84rem] text-ink-2">
          已有账号？
          <Link
            href="/login"
            className="ml-1 rounded-[3px] font-semibold text-seal underline-offset-2 hover:underline"
          >
            登录
          </Link>
        </p>
      </div>
    </form>
  );
}

function codeToField(code: string): "email" | "handle" | null {
  if (code === "email_taken") return "email";
  if (code === "handle_taken") return "handle";
  return null;
}

function focusFirstInvalid(errors: AuthFieldErrors) {
  const order: (keyof AuthFieldErrors)[] = ["email", "password", "name", "handle"];
  const first = order.find((key) => errors[key]);
  if (first) document.getElementById(`register-${first}`)?.focus();
}
