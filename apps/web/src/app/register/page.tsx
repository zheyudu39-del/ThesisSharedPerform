import type { Metadata } from "next";
import { Suspense } from "react";
import { AuthShell } from "@/components/AuthShell";
import { RegisterForm } from "@/components/RegisterForm";

export const metadata: Metadata = {
  title: "注册",
  description: "创建 Thesis Shared 账号，加入以人的判断筛选前沿论文的社区。",
};

export default function RegisterPage() {
  return (
    <AuthShell
      eyebrow="账号 · Join"
      title="建立一个学术身份"
      lede="设置邮箱、密码与公开句柄。句柄会出现在你的主页与分享链接中。"
      footer="注册成功后会自动登录，并跳转到关注流。"
    >
      {/* useSearchParams 需要 Suspense 边界，否则整页退化为动态渲染。 */}
      <Suspense fallback={<FormSkeleton />}>
        <RegisterForm />
      </Suspense>
    </AuthShell>
  );
}

function FormSkeleton() {
  return (
    <div className="flex flex-col gap-5" aria-hidden>
      {[0, 1, 2, 3].map((i) => (
        <div key={i} className="h-16 animate-pulse rounded-[6px] bg-surface-2" />
      ))}
      <div className="h-10 animate-pulse rounded-[6px] bg-surface-2" />
    </div>
  );
}
