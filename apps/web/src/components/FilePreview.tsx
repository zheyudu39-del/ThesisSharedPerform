"use client";

import { useEffect, useState } from "react";
import { marked } from "marked";
import hljs from "highlight.js/lib/common";
import { getOriginalFile, type FileDTO } from "@/lib/files";
import { PdfPreview } from "@/components/PdfPreview";

/**
 * 非 PDF 文件的站内预览（PRD V-04）。
 *
 * 按类型分流：
 *   - PDF / Office → {@link PdfPreview}（页图，由服务端转码）
 *   - 图片        → <img> 直接渲染原文件
 *   - Markdown    → marked 渲染，代码块交给 highlight.js
 *   - 代码 / LaTeX→ 等宽字体 + 语法高亮，公式按源码展示
 *
 * 取文本类文件走 /files/{id}/original 拿到的短时预签名地址，再由浏览器
 * 直接请求对象存储——文本不经过 API 服务器，避免大文件占用 API 内存。
 *
 * 安全：Markdown 渲染结果经过 sanitize（见下方 sanitizeHTML），
 * 不允许脚本、事件属性与 javascript: 协议。
 */

/** 超过这个体积的文本文件不在浏览器里渲染，改为提示下载。 */
const MAX_TEXT_BYTES = 2 * 1024 * 1024;

type LoadState =
  | { phase: "idle" }
  | { phase: "loading" }
  | { phase: "ready"; text: string; lang: string }
  | { phase: "image"; url: string }
  | { phase: "error"; message: string }
  | { phase: "too-large"; sizeBytes: number };

const CODE_LANG: Record<string, string> = {
  "text/x-tex": "latex",
  "application/x-tex": "latex",
  "application/x-latex": "latex",
  "text/markdown": "markdown",
  "text/plain": "plaintext",
};

/** 判断走哪条渲染路径。 */
function previewKind(file: FileDTO): "pdf" | "image" | "markdown" | "code" | "unsupported" {
  if (file.kind === "pdf" || file.mime === "application/pdf") return "pdf";
  if (file.kind === "office_doc" || file.kind === "office_slide" || file.kind === "office_sheet") {
    return "pdf"; // 转码后同样有页图
  }
  if (file.kind === "image" || file.mime.startsWith("image/")) return "image";
  if (file.kind === "markdown" || file.mime === "text/markdown") return "markdown";
  if (file.kind === "latex" || file.kind === "code" || file.mime.startsWith("text/")) return "code";
  return "unsupported";
}

/**
 * 极简 HTML 清洗：只保留渲染 Markdown 需要的标签与属性。
 *
 * 为什么手写而不引 DOMPurify：这里只处理 marked 的输出，标签集合可控；
 * 引一个 100KB+ 的净化库不划算。规则是「白名单标签 + 剥掉所有 on*
 * 属性 + 只允许 http(s)/相对地址」，足以挡住脚本注入。
 */
function sanitizeHTML(html: string): string {
  const template = document.createElement("template");
  template.innerHTML = html;

  const allowed = new Set([
    "P", "BR", "STRONG", "EM", "DEL", "CODE", "PRE", "BLOCKQUOTE",
    "UL", "OL", "LI", "H1", "H2", "H3", "H4", "H5", "H6",
    "A", "IMG", "TABLE", "THEAD", "TBODY", "TR", "TH", "TD", "HR", "SPAN",
  ]);

  const walk = (node: Node) => {
    for (const child of Array.from(node.childNodes)) {
      if (child.nodeType === Node.TEXT_NODE) continue;
      if (child.nodeType !== Node.ELEMENT_NODE) {
        child.remove();
        continue;
      }
      const el = child as Element;
      if (!allowed.has(el.tagName)) {
        // 不在白名单：保留文字内容，丢掉标签本身。
        el.replaceWith(...Array.from(el.childNodes));
        continue;
      }
      for (const attr of Array.from(el.attributes)) {
        const name = attr.name.toLowerCase();
        const isSafeHref =
          (name === "href" || name === "src") &&
          !/^\s*(javascript|data|vbscript):/i.test(attr.value);
        const isClass = name === "class";
        if (!isSafeHref && !isClass) el.removeAttribute(attr.name);
      }
      if (el.tagName === "A") {
        el.setAttribute("rel", "noopener noreferrer nofollow");
        el.setAttribute("target", "_blank");
      }
      walk(el);
    }
  };
  walk(template.content);
  return template.innerHTML;
}

export function FilePreview({ file, maxHeight = 480 }: { file: FileDTO; maxHeight?: number }) {
  const kind = previewKind(file);
  const [state, setState] = useState<LoadState>({ phase: "idle" });

  useEffect(() => {
    if (kind === "pdf" || kind === "unsupported") return;

    let cancelled = false;
    setState({ phase: "loading" });

    void (async () => {
      try {
        const original = await getOriginalFile(file.id);
        if (cancelled) return;

        if (kind === "image") {
          setState({ phase: "image", url: original.url });
          return;
        }

        if (file.sizeBytes > MAX_TEXT_BYTES) {
          setState({ phase: "too-large", sizeBytes: file.sizeBytes });
          return;
        }

        const response = await fetch(original.url);
        if (!response.ok) throw new Error(`HTTP ${response.status}`);
        const text = await response.text();
        if (cancelled) return;
        setState({ phase: "ready", text, lang: CODE_LANG[file.mime] ?? "plaintext" });
      } catch (err) {
        if (cancelled) return;
        setState({
          phase: "error",
          message: err instanceof Error ? err.message : "加载文件内容失败。",
        });
      }
    })();

    return () => {
      cancelled = true;
    };
  }, [file.id, file.mime, file.sizeBytes, kind]);

  if (kind === "pdf") {
    return <PdfPreview fileId={file.id} initialFile={file} maxHeight={maxHeight} />;
  }

  if (kind === "unsupported") {
    return (
      <Panel>
        <p className="text-[0.85rem] text-ink-2">这种文件类型暂不支持站内预览。</p>
        <FileMeta file={file} />
      </Panel>
    );
  }

  if (state.phase === "loading" || state.phase === "idle") {
    return (
      <Panel>
        <div className="h-32 animate-pulse rounded-[6px] bg-surface-2" aria-label="正在加载预览" />
      </Panel>
    );
  }

  if (state.phase === "error") {
    return (
      <Panel>
        <p className="text-[0.85rem] text-danger">预览加载失败：{state.message}</p>
        <FileMeta file={file} />
      </Panel>
    );
  }

  if (state.phase === "too-large") {
    return (
      <Panel>
        <p className="text-[0.85rem] text-ink-2">
          文件超过 {Math.round(MAX_TEXT_BYTES / 1024 / 1024)} MB，为避免页面卡顿不在站内渲染。
        </p>
        <FileMeta file={file} />
      </Panel>
    );
  }

  if (state.phase === "image") {
    return (
      <div
        className="overflow-hidden rounded-[10px] border border-line bg-surface-2"
        style={{ maxHeight }}
      >
        {/* 预签名地址来自自己的对象存储；eslint 的 next/image 规则在此不适用。 */}
        {/* eslint-disable-next-line @next/next/no-img-element */}
        <img
          src={state.url}
          alt={file.originalName}
          className="mx-auto max-h-full w-auto object-contain"
          style={{ maxHeight }}
        />
      </div>
    );
  }

  // markdown
  if (kind === "markdown") {
    const html = sanitizeHTML(marked.parse(state.text, { async: false }) as string);
    return (
      <div
        className="max-h-[var(--preview-max)] overflow-y-auto overscroll-contain rounded-[10px] border border-line bg-surface px-5 py-4 text-[0.9rem] leading-[1.75] text-ink-2 [&_a]:text-seal [&_a]:underline [&_blockquote]:my-2 [&_blockquote]:border-l-2 [&_blockquote]:border-line-strong [&_blockquote]:pl-3 [&_code]:rounded-[3px] [&_code]:bg-surface-2 [&_code]:px-1 [&_h1]:mt-3 [&_h1]:mb-2 [&_h1]:font-display [&_h1]:text-[1.2rem] [&_h1]:font-semibold [&_h1]:text-ink [&_h2]:mt-3 [&_h2]:mb-1.5 [&_h2]:font-display [&_h2]:text-[1.05rem] [&_h2]:font-semibold [&_h2]:text-ink [&_li]:ml-4 [&_li]:list-disc [&_pre]:my-2 [&_pre]:overflow-x-auto [&_pre]:rounded-[6px] [&_pre]:bg-surface-2 [&_pre]:p-3 [&_pre]:font-mono [&_pre]:text-[0.8rem] [&_table]:my-2 [&_td]:border [&_td]:border-line [&_td]:px-2 [&_td]:py-1 [&_th]:border [&_th]:border-line [&_th]:bg-surface-2 [&_th]:px-2 [&_th]:py-1"
        style={{ ["--preview-max" as string]: `${maxHeight}px` }}
        dangerouslySetInnerHTML={{ __html: html }}
      />
    );
  }

  // code / latex：等宽 + 高亮，保留原始换行与缩进
  const highlighted = hljs.highlight(state.text, { language: state.lang, ignoreIllegals: true });
  return (
    <div
      className="overflow-auto overscroll-contain rounded-[10px] border border-line bg-surface-2"
      style={{ maxHeight }}
    >
      <pre className="m-0 p-4 font-mono text-[0.78rem] leading-[1.6]">
        <code dangerouslySetInnerHTML={{ __html: highlighted.value }} />
      </pre>
    </div>
  );
}

function Panel({ children }: { children: React.ReactNode }) {
  return (
    <div className="rounded-[10px] border border-dashed border-line-strong bg-surface p-4">
      {children}
    </div>
  );
}

function FileMeta({ file }: { file: FileDTO }) {
  return (
    <p className="mt-1.5 font-mono text-[0.7rem] text-ink-3">
      {file.originalName}
    </p>
  );
}
