import type { Metadata } from "next";
import { AdminReportsWorkspace } from "@/app/admin/reports/AdminReportsWorkspace";

/**
 * 运营后台 · 举报审核队列（PRD F-06）。
 *
 * 外壳（侧边栏、403 判定）由 app/admin/layout.tsx 提供，这里只渲染内容。
 * 真正的授权在 Go 服务层：非 admin 请求 /reports 一律 403，
 * 客户端隐藏入口只是体验优化，不是安全边界。
 */
export const metadata: Metadata = { title: "举报审核" };

export default function AdminReportsPage() {
  return <AdminReportsWorkspace />;
}
