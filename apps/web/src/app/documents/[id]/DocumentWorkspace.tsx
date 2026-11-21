"use client";

import { useCallback, useState } from "react";
import type { Editor } from "@tiptap/react";
import { useAuth } from "@/components/AuthProvider";
import { DocumentEditor } from "@/components/DocumentEditor";
import { DocSidebar } from "@/components/DocSidebar";
import { ReferencePanel } from "@/components/ReferencePanel";
import {
  EXPORT_FORMAT_LABEL,
  exportDocument,
  importDocument,
  updateDocument,
  type DocRole,
  type DocumentDTO,
  type ExportFormat,
  type ImportFormat,
} from "@/lib/documents";

/**
 * 文档工作区：标题栏 + 编辑器 + 侧栏（快照与批注）。
 *
 * 拆成客户端组件是因为标题编辑、快照创建都需要交互；
 * 页面外壳（元数据、权限判定）在 Server Component 里完成。
 */
export function DocumentWorkspace({
  document: initial,
  role,
}: {
  document: DocumentDTO;
  role: DocRole;
}) {
  const { user } = useAuth();
  const [doc, setDoc] = useState(initial);
  const [title, setTitle] = useState(initial.title);
  const [editingTitle, setEditingTitle] = useState(false);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // 导出：持有编辑器实例，点击导出时取 getHTML()。
  const [editor, setEditor] = useState<Editor | null>(null);
  const [exporting, setExporting] = useState<ExportFormat | null>(null);
  const [exportError, setExportError] = useState<string | null>(null);
  const [importError, setImportError] = useState<string | null>(null);

  const canEdit = role === "owner" || role === "editor";

  // 传给 DocumentEditor 的回调必须稳定，否则编辑器每次渲染都会重挂 effect。
  const handleEditorReady = useCallback((instance: Editor | null) => {
    setEditor(instance);
  }, []);

  // 导入：把 Word / LaTeX 转成 HTML 后写进协同文档。
  // 直接 setContent 而不是替换文件——导入后它就是一篇文章，
  // 所有协作者实时看到，接着正常编辑。
  const [importing, setImporting] = useState(false);
  const [importWarnings, setImportWarnings] = useState<string[]>([]);

  async function handleImport(file: File) {
    if (!editor) {
      setImportError("编辑器尚未就绪，请稍后再试。");
      return;
    }
    // 覆盖前先确认：导入会替换当前正文，这在协同场景下影响所有人。
    if (
      !window.confirm(
        `导入「${file.name}」将替换当前正文，且对所有协作者生效。确定继续？`,
      )
    ) {
      return;
    }
    setImporting(true);
    setImportError(null);
    setImportWarnings([]);
    try {
      const format: ImportFormat | undefined = file.name.toLowerCase().endsWith(".docx")
        ? "docx"
        : file.name.toLowerCase().endsWith(".tex") ||
            file.name.toLowerCase().endsWith(".latex")
          ? "tex"
          : undefined;
      const result = await importDocument(doc.id, file, format);
      if (!result.html.trim()) {
        throw new Error("文档没有可导入的正文。");
      }
      // 关键一步：内容通过编辑器的协同绑定直接写进 Y.Doc。
      editor.commands.setContent(result.html);
      setImportWarnings(result.warnings);
    } catch (err) {
      setImportError(err instanceof Error ? err.message : "导入失败，请稍后重试。");
    } finally {
      setImporting(false);
    }
  }

  async function handleExport(format: ExportFormat) {
    if (!editor) {
      setExportError("编辑器尚未就绪，请稍后再试。");
      return;
    }
    setExporting(format);
    setExportError(null);
    try {
      // 取当前文档的 HTML。正文是 Yjs 二进制，服务端无法解析，
      // 浏览器里的编辑器实例是最可靠的序列化入口。
      await exportDocument(doc.id, editor.getHTML(), format, doc.title);
    } catch (err) {
      setExportError(err instanceof Error ? err.message : "导出失败，请稍后重试。");
    } finally {
      setExporting(null);
    }
  }

  async function commitTitle() {
    const next = title.trim();
    if (!next || next === doc.title) {
      setTitle(doc.title);
      setEditingTitle(false);
      return;
    }
    setSaving(true);
    setError(null);
    try {
      const result = await updateDocument(doc.id, { title: next });
      setDoc(result.document);
      setEditingTitle(false);
    } catch (err) {
      setError(err instanceof Error ? err.message : "重命名失败");
      setTitle(doc.title);
    } finally {
      setSaving(false);
    }
  }

  return (
    <div className="flex flex-col gap-5">
      {/* 标题栏 */}
      <header className="border-b border-line pb-4">
        {editingTitle ? (
          <div className="flex flex-wrap items-center gap-2">
            <input
              value={title}
              onChange={(e) => setTitle(e.target.value)}
              onKeyDown={(e) => {
                if (e.key === "Enter") void commitTitle();
                if (e.key === "Escape") {
                  setTitle(doc.title);
                  setEditingTitle(false);
                }
              }}
              autoFocus
              maxLength={256}
              className="min-w-0 flex-1 rounded-[6px] border border-line-strong bg-surface px-3 py-1.5 font-display text-xl font-semibold text-ink focus:border-seal focus:outline-none"
            />
            <button
              type="button"
              onClick={() => void commitTitle()}
              disabled={saving}
              className="rounded-[6px] bg-seal px-3 py-1.5 text-sm font-semibold text-white hover:bg-seal-deep disabled:opacity-60"
            >
              {saving ? "保存中…" : "保存"}
            </button>
            <button
              type="button"
              onClick={() => {
                setTitle(doc.title);
                setEditingTitle(false);
              }}
              className="rounded-[6px] border border-line-strong px-3 py-1.5 text-sm text-ink-2 hover:border-ink-3"
            >
              取消
            </button>
          </div>
        ) : (
          <div className="flex flex-wrap items-baseline gap-3">
            <h1 className="font-display text-[clamp(1.5rem,3.5vw,2rem)] leading-tight font-semibold tracking-[-0.02em]">
              {doc.title}
            </h1>
            {canEdit && (
              <button
                type="button"
                onClick={() => setEditingTitle(true)}
                className="text-[0.78rem] text-ink-3 hover:text-ink"
              >
                重命名
              </button>
            )}
            <span className="ml-auto font-mono text-[0.7rem] text-ink-3">
              {doc.wordCount > 0 ? `${doc.wordCount} 字 · ` : ""}
              {ROLE_LABEL[role]}
            </span>
          </div>
        )}
        {error && <p className="mt-2 text-[0.78rem] text-danger">{error}</p>}

        {/*
          导出：PRD D-07（保留章节结构与公式）+ D-08（无权限用户只读且不可导出）。
          viewer 不渲染入口——服务端也会拒绝，这里只是不给无意义的按钮。
        */}
        {canEdit && (
          <div className="mt-3 flex flex-wrap items-center gap-2">
            <span className="font-mono text-[0.68rem] tracking-[0.08em] text-ink-3 uppercase">
              导出
            </span>
            {(Object.keys(EXPORT_FORMAT_LABEL) as ExportFormat[]).map((format) => (
              <button
                key={format}
                type="button"
                disabled={exporting !== null || !editor}
                onClick={() => void handleExport(format)}
                className="rounded-[3px] border border-line-strong px-2.5 py-1 text-[0.74rem] text-ink-2 hover:border-ink-3 hover:text-ink disabled:opacity-50"
              >
                {exporting === format ? "导出中…" : EXPORT_FORMAT_LABEL[format]}
              </button>
            ))}
            {exportError && <span className="text-[0.74rem] text-danger">{exportError}</span>}
          </div>
        )}

        {/*
          导入：把 Word / LaTeX 正文带进协同编辑器，之后就是一篇正常文档，
          所有协作者可继续编辑。转换在服务端完成，前端只负责写进 Y.Doc。
        */}
        {canEdit && (
          <div className="mt-2 flex flex-wrap items-center gap-2">
            <span className="font-mono text-[0.68rem] tracking-[0.08em] text-ink-3 uppercase">
              导入
            </span>
            <label
              className={`cursor-pointer rounded-[3px] border border-line-strong px-2.5 py-1 text-[0.74rem] text-ink-2 hover:border-ink-3 hover:text-ink ${
                importing || !editor ? "pointer-events-none opacity-50" : ""
              }`}
            >
              {importing ? "导入中…" : "Word (.docx) / LaTeX (.tex)"}
              <input
                type="file"
                accept=".docx,.tex,.latex"
                className="sr-only"
                disabled={importing || !editor}
                onChange={(e) => {
                  const file = e.target.files?.[0];
                  // 清空 value：否则连续选同一个文件不会触发 onChange。
                  e.target.value = "";
                  if (file) void handleImport(file);
                }}
              />
            </label>
            {importError && <span className="text-[0.74rem] text-danger">{importError}</span>}
          </div>
        )}

        {importWarnings.length > 0 && (
          <ul className="mt-2 flex flex-col gap-1 rounded-[6px] border border-warn/40 bg-warn-wash px-3 py-2">
            {importWarnings.map((warning) => (
              <li key={warning} className="text-[0.74rem] leading-relaxed text-warn">
                {warning}
              </li>
            ))}
          </ul>
        )}
      </header>

      <div className="grid gap-5 lg:grid-cols-[minmax(0,1fr)_260px]">
        <DocumentEditor
          documentId={doc.id}
          role={role}
          userName={user?.name ?? "匿名用户"}
          userId={user?.id ?? "anonymous"}
          onReady={handleEditorReady}
        />
        {/*
          侧栏：快照/批注在上，引用面板在下。
          引用面板单独成块而不是塞进 DocSidebar 的 Tab：它需要常驻可见，
          用户写正文时切格式预览参考文献的频率远高于翻批注。
        */}
        <div className="flex flex-col gap-3">
          <DocSidebar documentId={doc.id} canEdit={canEdit} />
          <ReferencePanel documentId={doc.id} canEdit={canEdit} />
        </div>
      </div>
    </div>
  );
}

const ROLE_LABEL: Record<DocRole, string> = {
  owner: "所有者",
  editor: "可编辑",
  viewer: "只读",
};
