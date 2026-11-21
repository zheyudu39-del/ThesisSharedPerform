import type { Metadata } from "next";
import { AdminReportsWorkspace } from "@/app/admin/reports/AdminReportsWorkspace";
import { SiteHeader } from "@/components/SiteHeader";

/**
 * 运营后台 · 举报审核队列（PRD F-06）。
 *
 * 页面本身不做角色判定：是否可见由客户端工作区根据登录用户的 role 决定，
 * 真正的授权在 Go 服务层（非 admin 请求 /reports 一律 403）。
 * 客户端隐藏入口只是体验优化，不是安全边界。
 */
export const metadata: Metadata = {
  title: "举报审核",
  description: "运营后台：查看并处理用户举报。",
  // 后台页面不应进入搜索引擎索引。
  robots: { index: false, follow: false },
};

export default function AdminReportsPage() {
  return (
    <>
      <SiteHeader />
      <AdminReportsWorkspace />
    </>
  );
}
