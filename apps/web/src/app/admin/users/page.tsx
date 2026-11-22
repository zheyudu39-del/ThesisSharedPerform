import type { Metadata } from "next";
import { AdminUsersPanel } from "@/app/admin/users/AdminUsersPanel";

export const metadata: Metadata = { title: "用户管理" };

export default function AdminUsersPage() {
  return <AdminUsersPanel />;
}
