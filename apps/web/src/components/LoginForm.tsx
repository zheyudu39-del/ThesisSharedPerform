"use client";

import Link from "next/link";
import { useRouter } from "next/navigation";
import { useState, type FormEvent } from "react";
import { AuthField } from "@/components/AuthField";
import { AuthError, login, validateLogin, type AuthFieldErrors } from "@/lib/auth";

/**
 * 登录表单（客户端）。
 * 校验在提交时进行；服务端 401 统一落到表单级错误，不回显「邮箱或密码错误」之外的信息。
 */
export function LoginForm() {
  const router = useRouter();

  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [fieldErrors, setFieldErrors] = useState<AuthFieldErrors>({});
  const [formError, setFormError] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (submitting) return;

    setFormError(null);
    const errors = validateLogin(email, password);
    setFieldErrors(errors);
    if (Object.keys(errors).length > 0) {
      focusFirstInvalid(errors);
      return;
    }

    setSubmitting(true);
    try {
      await login(email, password);
      router.push("/");
      router.refresh();
    } catch (error) {
      setFormError(
        error instanceof AuthError ? error.message : "登录失败，请稍后重试。",
      );
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
        id="login-email"
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
        id="login-password"
        name="password"
        label="密码"
        type="password"
        autoComplete="current-password"
        placeholder="输入密码"
        value={password}
        onChange={(e) => setPassword(e.target.value)}
        error={fieldErrors.password}
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
          {submitting ? "正在登录…" : "登录"}
        </button>

        <p className="text-center text-[0.84rem] text-ink-2">
          还没有账号？
          <Link
            href="/register"
            className="ml-1 rounded-[3px] font-semibold text-seal underline-offset-2 hover:underline"
          >
            注册
          </Link>
        </p>
      </div>
    </form>
  );
}

/** 定位到第一个出错的输入框，便于键盘与读屏用户立即修正。 */
function focusFirstInvalid(errors: AuthFieldErrors) {
  const order: (keyof AuthFieldErrors)[] = ["email", "password"];
  const first = order.find((key) => errors[key]);
  if (first) document.getElementById(`login-${first}`)?.focus();
}
