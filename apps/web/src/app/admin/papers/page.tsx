import type { Metadata } from "next";
import { AdminPapersPanel } from "@/app/admin/papers/AdminPapersPanel";

export const metadata: Metadata = { title: "论文管理" };

export default function AdminPapersPage() {
  return <AdminPapersPanel />;
}
