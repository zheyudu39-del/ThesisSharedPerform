import type { Metadata } from "next";
import { AdminFilesPanel } from "@/app/admin/files/AdminFilesPanel";

export const metadata: Metadata = { title: "文件排查" };

export default function AdminFilesPage() {
  return <AdminFilesPanel />;
}
