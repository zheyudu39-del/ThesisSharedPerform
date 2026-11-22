import type { Metadata } from "next";
import { AdminCommentsPanel } from "@/app/admin/comments/AdminCommentsPanel";

export const metadata: Metadata = { title: "评论管理" };

export default function AdminCommentsPage() {
  return <AdminCommentsPanel />;
}
