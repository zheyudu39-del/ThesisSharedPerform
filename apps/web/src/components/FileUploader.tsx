"use client";

import { useCallback, useEffect, useId, useRef, useState } from "react";
import { AuthError } from "@/lib/auth";
import {
  ACCEPT_ATTRIBUTE,
  formatBytes,
  MAX_FILE_BYTES,
  uploadFile,
  validateUpload,
  type FileDTO,
} from "@/lib/files";

/**
 * 文件上传器：拖拽或点击选择 → 客户端预检 → 三步直传 → 回调 FileDTO。
 *
 * 进度条依赖 XMLHttpRequest 的 upload.onprogress（fetch 拿不到上传进度），
 * 具体实现在 lib/files.ts 的 uploadToS3 中。
 *
 * 组件只负责单文件、单次上传；多文件与业务关联（发帖、论文挂载）由调用方决定。
 */

type UploadState =
  | { phase: "idle" }
  | { phase: "uploading"; percent: number; name: string; size: number }
  | { phase: "done"; file: FileDTO }
  | { phase: "error"; message: string };

export type FileUploaderProps = {
  /** 上传并 complete 成功后的回调。 */
  onUploaded?: (file: FileDTO) => void;
  /** 自定义提示文案（例如「上传 PDF 以便内联预览」）。 */
  hint?: string;
  /** 禁用（未登录或父级正在提交）。 */
  disabled?: boolean;
  /** 紧凑模式：用于发帖框等空间有限的位置。 */
  compact?: boolean;
};

export function FileUploader({
  onUploaded,
  hint,
  disabled = false,
  compact = false,
}: FileUploaderProps) {
  const [state, setState] = useState<UploadState>({ phase: "idle" });
  const [dragging, setDragging] = useState(false);
  const inputRef = useRef<HTMLInputElement>(null);
  const abortRef = useRef<AbortController | null>(null);
  const dragDepth = useRef(0);
  const mountedRef = useRef(true);
  const fieldId = useId();

  useEffect(() => {
    mountedRef.current = true;
    return () => {
      mountedRef.current = false;
      abortRef.current?.abort();
    };
  }, []);

  const busy = state.phase === "uploading";
  const locked = disabled || busy;

  const startUpload = useCallback(
    async (file: File) => {
      if (locked) return;

      const validation = validateUpload(file);
      if (!validation.ok) {
        setState({ phase: "error", message: validation.message });
        return;
      }

      abortRef.current?.abort();
      const controller = new AbortController();
      abortRef.current = controller;

      setState({
        phase: "uploading",
        percent: 0,
        name: file.name,
        size: file.size,
      });

      try {
        const uploaded = await uploadFile(file, {
          signal: controller.signal,
          onProgress: (percent) => {
            if (!mountedRef.current) return;
            setState((prev) =>
              prev.phase === "uploading" ? { ...prev, percent } : prev,
            );
          },
        });
        if (!mountedRef.current) return;
        setState({ phase: "done", file: uploaded });
        onUploaded?.(uploaded);
      } catch (err) {
        if (!mountedRef.current) return;
        if (err instanceof AuthError && err.code === "upload_aborted") {
          setState({ phase: "idle" });
          return;
        }
        setState({
          phase: "error",
          message:
            err instanceof AuthError ? err.message : "上传失败，请稍后重试。",
        });
      } finally {
        if (abortRef.current === controller) abortRef.current = null;
        if (inputRef.current) inputRef.current.value = "";
      }
    },
    [locked, onUploaded],
  );

  function handleFiles(list: FileList | null) {
    const file = list?.[0];
    if (file) void startUpload(file);
  }

  /* 拖拽：dragenter/leave 会成对触发于子元素，用计数避免闪烁。 */
  function onDragEnter(event: React.DragEvent) {
    if (locked) return;
    event.preventDefault();
    dragDepth.current += 1;
    setDragging(true);
  }
  function onDragOver(event: React.DragEvent) {
    if (locked) return;
    event.preventDefault();
    event.dataTransfer.dropEffect = "copy";
  }
  function onDragLeave(event: React.DragEvent) {
    event.preventDefault();
    dragDepth.current = Math.max(0, dragDepth.current - 1);
    if (dragDepth.current === 0) setDragging(false);
  }
  function onDrop(event: React.DragEvent) {
    event.preventDefault();
    dragDepth.current = 0;
    setDragging(false);
    if (locked) return;
    handleFiles(event.dataTransfer.files);
  }

  function reset() {
    abortRef.current?.abort();
    abortRef.current = null;
    setState({ phase: "idle" });
    if (inputRef.current) inputRef.current.value = "";
  }

  const hintText = hint ?? `支持 PDF、图片、Office、Markdown · 单文件 ≤ ${formatBytes(MAX_FILE_BYTES)}`;

  return (
    <div className="flex flex-col gap-2">
      <label htmlFor={`${fieldId}-input`} className="sr-only">
        选择要上传的文件
      </label>
      <input
        id={`${fieldId}-input`}
        ref={inputRef}
        type="file"
        accept={ACCEPT_ATTRIBUTE}
        disabled={locked}
        onChange={(e) => handleFiles(e.target.files)}
        className="sr-only"
      />

      {state.phase === "uploading" ? (
        <div
          className={`rounded-[6px] border border-line bg-surface-2 px-3.5 ${compact ? "py-2.5" : "py-3.5"}`}
          role="status"
          aria-live="polite"
        >
          <div className="mb-2 flex items-center gap-2">
            <span
              aria-hidden
              className="size-3.5 shrink-0 animate-spin rounded-full border-2 border-line-strong border-t-seal"
            />
            <span className="min-w-0 flex-1 truncate text-[0.82rem] text-ink">
              {state.name}
            </span>
            <span className="shrink-0 font-mono text-[0.72rem] tabular-nums text-ink-3">
              {formatBytes(state.size)}
            </span>
            <button
              type="button"
              onClick={reset}
              className="shrink-0 rounded-[3px] px-1.5 py-0.5 font-mono text-[0.68rem] text-ink-3 hover:bg-surface hover:text-danger"
            >
              取消
            </button>
          </div>
          <div
            className="h-1.5 w-full overflow-hidden rounded-full bg-line"
            role="progressbar"
            aria-valuemin={0}
            aria-valuemax={100}
            aria-valuenow={state.percent}
            aria-label="上传进度"
          >
            <div
              className="h-full rounded-full bg-seal transition-[width] duration-200 ease-out"
              style={{ width: `${state.percent}%` }}
            />
          </div>
          <p className="mt-1.5 text-right font-mono text-[0.68rem] tabular-nums text-ink-3">
            {state.percent < 100 ? `上传中 ${state.percent}%` : "正在校验并转码…"}
          </p>
        </div>
      ) : state.phase === "done" ? (
        <div className="flex items-center gap-2.5 rounded-[6px] border border-good/40 bg-good-wash px-3.5 py-2.5">
          <svg
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            strokeWidth="2"
            className="size-4 shrink-0 text-good"
            aria-hidden
          >
            <path d="m20 6-11 11-5-5" />
          </svg>
          <span className="min-w-0 flex-1 truncate text-[0.82rem] text-ink">
            {state.file.originalName}
          </span>
          <span className="shrink-0 font-mono text-[0.68rem] text-ink-3">
            {formatBytes(state.file.sizeBytes)}
          </span>
          <button
            type="button"
            onClick={reset}
            className="shrink-0 rounded-[3px] px-1.5 py-0.5 font-mono text-[0.68rem] text-ink-2 hover:bg-surface hover:text-ink"
          >
            换一个
          </button>
        </div>
      ) : (
        <button
          type="button"
          onClick={() => inputRef.current?.click()}
          onDragEnter={onDragEnter}
          onDragOver={onDragOver}
          onDragLeave={onDragLeave}
          onDrop={onDrop}
          disabled={locked}
          aria-describedby={`${fieldId}-hint`}
          className={`flex flex-col items-center justify-center gap-1.5 rounded-[6px] border border-dashed px-4 text-center transition-colors disabled:cursor-not-allowed disabled:opacity-55 ${
            compact ? "py-3" : "py-6"
          } ${
            dragging
              ? "border-seal bg-seal-wash"
              : "border-line-strong bg-surface hover:border-ink-3 hover:bg-surface-2"
          }`}
        >
          <svg
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            strokeWidth="1.6"
            className={`${compact ? "size-4" : "size-5"} text-ink-3`}
            aria-hidden
          >
            <path d="M12 16V4" />
            <path d="m7 9 5-5 5 5" />
            <path d="M4 16v2a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2v-2" />
          </svg>
          <span className="text-[0.84rem] font-medium text-ink">
            {dragging ? "松开即可上传" : "拖拽文件到这里，或点击选择"}
          </span>
          {!compact && <span className="font-mono text-[0.68rem] text-ink-3">{hintText}</span>}
        </button>
      )}

      <p id={`${fieldId}-hint`} className={compact ? "sr-only" : "font-mono text-[0.68rem] text-ink-3"}>
        {hintText}
      </p>

      {state.phase === "error" && (
        <p
          role="alert"
          className="flex items-start gap-1.5 rounded-[6px] border border-danger/40 bg-danger-wash px-3 py-2 text-[0.8rem] leading-relaxed text-danger"
        >
          <span aria-hidden className="mt-0.5 font-mono font-semibold">
            !
          </span>
          <span>{state.message}</span>
        </p>
      )}
    </div>
  );
}
