import type { Metadata } from "next";
import { AdminOverviewPanel } from "@/app/admin/AdminOverviewPanel";

export const metadata: Metadata = { title: "概览" };

export default function AdminOverviewPage() {
  return <AdminOverviewPanel />;
}
