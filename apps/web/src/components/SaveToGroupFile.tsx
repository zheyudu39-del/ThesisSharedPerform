"use client";

import { useState } from "react";
import { GROUP_FILE_LIMITS, parseTags, saveGroupFile } from "@/lib/groupfiles";

/**
 * 「存入群文件库」按钮（PRD G-05）。
 *
 * 独立成组件而不是直接改进 PostCard：
 *   - PostCard 同时被多个改动占用，往里加状态容易冲突；
 *   - 这个按钮只在「群组页的帖子列表」里有意义（公开时间线里的帖子
 *     不属于任何群，无处可存），由页面决定是否渲染即可。
 *
 * 接线方式（任选其一）：
 *   <SaveToGroupFile groupSlug={slug} fileId={file.id} />
 * 或在群组页帖子列表的附件行尾挂载。
 *
 * 展开后可以改标题、描述与标签；不填时后端回退到原始文件名。
 */
export function SaveToGroupFile({
  groupSlug,
  fileId,
  className,
}: {
  groupSlug: string;
  fileId: string;
  className?: string;
}) {
  const [open, setOpen] = useState(false);
  const [title, setTitle] = useState("");
  const [description, setDescription] = useState("");
  const [tags, setTags] = useState("");
  const [saving, setSaving] = useState(false);
  const [done, setDone] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function handleSave() {
    setSaving(true);
    setError(null);
    try {
      await saveGroupFile(groupSlug, {
        fileId,
        title: title.trim() || undefined,
        description: description.trim() || undefined,
        tags: parseTags(tags),
      });
      setDone(true);
      setOpen(false);
    } catch (err) {
      // 409 是「已存入」，对用户来说是成功语义，单独提示。
      const message = err instanceof Error ? err.message : "存入失败";
      setError(message);
    } finally {
      setSaving(false);
    }
  }

  if (done) {
    return (
      <span className={`text-[0.74rem] text-ink-3 ${className ?? ""}`}>已存入文件库</span>
    );
  }

  if (!open) {
    return (
      <button
        type="button"
        onClick={() => setOpen(true)}
        className={`rounded-[3px] border border-line-strong px-2 py-0.5 text-[0.72rem] text-ink-2 hover:border-ink-3 hover:text-ink ${className ?? ""}`}
      >
        存入文件库
      </button>
    );
  }

  return (
    <div className={`flex flex-col gap-1.5 rounded-[6px] border border-line bg-surface-2 p-2.5 ${className ?? ""}`}>
      <input
        value={title}
        onChange={(e) => setTitle(e.target.value)}
        maxLength={GROUP_FILE_LIMITS.titleMax}
        placeholder="标题（留空用文件名）"
        className="rounded-[4px] border border-line bg-surface px-1.5 py-1 text-[0.74rem] text-ink focus:border-seal focus:outline-none"
      />
      <input
        value={description}
        onChange={(e) => setDescription(e.target.value)}
        maxLength={GROUP_FILE_LIMITS.descriptionMax}
        placeholder="一句话描述（可选）"
        className="rounded-[4px] border border-line bg-surface px-1.5 py-1 text-[0.74rem] text-ink focus:border-seal focus:outline-none"
      />
      <input
        value={tags}
        onChange={(e) => setTags(e.target.value)}
        placeholder="标签，用逗号分隔（如 NLP, 综述）"
        className="rounded-[4px] border border-line bg-surface px-1.5 py-1 text-[0.74rem] text-ink focus:border-seal focus:outline-none"
      />
      {error && <p className="text-[0.72rem] text-danger">{error}</p>}
      <div className="flex items-center gap-2">
        <button
          type="button"
          onClick={() => void handleSave()}
          disabled={saving}
          className="rounded-[3px] bg-seal px-2.5 py-1 text-[0.72rem] font-semibold text-white hover:bg-seal-deep disabled:opacity-50"
        >
          {saving ? "存入中…" : "确认存入"}
        </button>
        <button
          type="button"
          onClick={() => setOpen(false)}
          className="rounded-[3px] border border-line-strong px-2.5 py-1 text-[0.72rem] text-ink-2 hover:border-ink-3"
        >
          取消
        </button>
      </div>
    </div>
  );
}
