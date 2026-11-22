import type { Metadata } from "next";
import { AdminGroupsPanel } from "@/app/admin/groups/AdminGroupsPanel";

export const metadata: Metadata = { title: "群组管理" };

export default function AdminGroupsPage() {
  return <AdminGroupsPanel />;
}
