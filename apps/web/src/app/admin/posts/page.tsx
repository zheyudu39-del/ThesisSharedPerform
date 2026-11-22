import type { Metadata } from "next";
import { AdminPostsPanel } from "@/app/admin/posts/AdminPostsPanel";

export const metadata: Metadata = { title: "帖子管理" };

export default function AdminPostsPage() {
  return <AdminPostsPanel />;
}
