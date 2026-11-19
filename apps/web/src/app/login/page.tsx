import type { Metadata } from "next";
import { Suspense } from "react";
import { AuthShell } from "@/components/AuthShell";
import { LoginForm } from "@/components/LoginForm";

export const metadata: Metadata = {
  title: "登录",
  description: "登录 Thesis Shared，继续以人的判断筛选前沿论文。",
};

export default function LoginPage() {
  return (
    <AuthShell
      eyebrow="账号 · Sign in"
      title="回到你的关注流"
      lede="用邮箱和密码登录，继续阅读你关注的人挑选的论文。"
      footer="令牌保存在本地浏览器，仅用于 M1 阶段。"
    >
      {/* useSearchParams 需要 Suspense 边界，否则整页退化为动态渲染。 */}
      <Suspense fallback={<FormSkeleton />}>
        <LoginForm />
      </Suspense>
    </AuthShell>
  );
}

function FormSkeleton() {
  return (
    <div className="flex flex-col gap-5" aria-hidden>
      <div className="h-16 animate-pulse rounded-[6px] bg-surface-2" />
      <div className="h-16 animate-pulse rounded-[6px] bg-surface-2" />
      <div className="h-10 animate-pulse rounded-[6px] bg-surface-2" />
    </div>
  );
}
