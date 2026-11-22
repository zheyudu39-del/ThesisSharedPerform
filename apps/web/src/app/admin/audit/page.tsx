import type { Metadata } from "next";
import { AdminAuditPanel } from "@/app/admin/audit/AdminAuditPanel";

export const metadata: Metadata = { title: "操作审计" };

export default function AdminAuditPage() {
  return <AdminAuditPanel />;
}
