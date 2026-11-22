import type { Metadata } from "next";
import { AdminShell } from "@/components/AdminShell";

/**
 * 运营后台布局。
 *
 * 与前台的区别：固定侧边栏 + 更紧凑的信息密度，视觉上明确「这不是普通用户界面」。
 * 权限不在这一层判定——布局是服务端组件，拿不到登录态；真正的授权在
 * Go 服务层，前端由 AdminShell 在客户端按 role 隐藏入口与内容。
 */
export const metadata: Metadata = {
  title: { default: "运营后台", template: "%s · 运营后台" },
  // 后台页面不应进入搜索引擎索引。
  robots: { index: false, follow: false },
};

export default function AdminLayout({ children }: { children: React.ReactNode }) {
  return <AdminShell>{children}</AdminShell>;
}
