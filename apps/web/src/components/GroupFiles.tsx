"use client";

import { useCallback, useEffect, useState } from "react";
import {
  GROUP_FILE_LIMITS,
  deleteGroupFile,
  fileKindLabel,
  formatFileSize,
  listGroupFiles,
  parseTags,
  saveGroupFile,
  type GroupFileDTO,
} from "@/lib/groupfiles";
import { ApiError } from "@/lib/http";
import { GroupFilePreview } from "@/components/GroupFilePreview";

/**
 * 群文件库（PRD G-05）。
 *
 * 文件库保存的是「指向已有文件的引用」，不是副本：同一个 PDF 可以同时
 * 出现在多个群里，也不会因为某篇帖子被删而消失。
 *
 * 按标签筛选走服务端（GIN 索引），不在前端过滤——否则翻页会翻出
 * 「这一页里刚好没有匹配项」的空页。
 */
export function GroupFiles({ slug, isMember }: { slug: string; isMember: boolean }) {
  const [files, setFiles] = useState<GroupFileDTO[]>([]);
  const [cursor, setCursor] = useState<string | null>(null);
  const [tag, setTag] = useState("");
  const [loading, setLoading] = useState(true);
  const [loadingMore, setLoadingMore] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(
    async (options: { tag?: string; cursor?: string | null } = {}) => {
      const appending = Boolean(options.cursor);
      if (appending) setLoadingMore(true);
      else setLoading(true);
      setError(null);
      try {
        const page = await listGroupFiles(slug, {
          tag: options.tag,
          cursor: options.cursor ?? null,
        });
        setFiles((prev) => (appending ? [...prev, ...page.files] : page.files));
        setCursor(page.nextCursor);
      } catch (err) {
        setError(err instanceof ApiError ? err.message : "加载文件库失败。");
      } finally {
        setLoading(false);
        setLoadingMore(false);
      }
    },
    [slug],
  );

  useEffect(() => {
    if (isMember) void load();
  }, [isMember, load]);

  async function handleDelete(id: string) {
    setError(null);
    try {
      await deleteGroupFile(slug, id);
      setFiles((prev) => prev.filter((file) => file.id !== id));
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "删除失败，请稍后重试。");
    }
  }

  if (!isMember) {
    return (
      <p className="rounded-[12px] border border-dashed border-line-strong bg-surface px-5 py-10 text-center text-[0.88rem] leading-relaxed text-ink-2">
        加入群组后可以查看群文件库。
      </p>
    );
  }

  return (
    <div className="flex flex-col gap-3">
      <form
        onSubmit={(e) => {
          e.preventDefault();
          void load({ tag: tag.trim() || undefined });
        }}
        className="flex gap-2"
      >
        <input
          value={tag}
          onChange={(e) => setTag(e.target.value)}
          placeholder="按标签筛选"
          className="min-w-0 flex-1 rounded-[6px] border border-line-strong bg-surface px-3 py-1.5 text-[0.84rem] text-ink focus:border-seal focus:outline-none"
        />
        <button
          type="submit"
          className="shrink-0 rounded-[6px] border border-line-strong px-3 py-1.5 text-[0.82rem] font-semibold text-ink hover:border-ink-3"
        >
          筛选
        </button>
        {tag && (
          <button
            type="button"
            onClick={() => {
              setTag("");
              void load();
            }}
            className="shrink-0 rounded-[6px] px-2 py-1.5 text-[0.82rem] text-ink-3 hover:text-ink"
          >
            清除
          </button>
        )}
      </form>

      <SaveFileForm
        slug={slug}
        onSaved={(file) => setFiles((prev) => [file, ...prev])}
        onError={setError}
      />

      {error && (
        <p role="alert" className="rounded-[6px] border border-danger/40 bg-danger-wash px-3 py-2 text-[0.8rem] text-danger">
          {error}
        </p>
      )}

      {loading ? (
        <div className="flex flex-col gap-2" aria-hidden>
          {[0, 1].map((i) => (
            <div key={i} className="h-16 animate-pulse rounded-[12px] bg-surface-2" />
          ))}
        </div>
      ) : files.length === 0 ? (
        <p className="rounded-[12px] border border-dashed border-line-strong bg-surface px-5 py-10 text-center text-[0.88rem] text-ink-2">
          {tag ? `没有标签为「${tag}」的文件。` : "文件库还是空的。从群内帖子把文件存进来即可共享。"}
        </p>
      ) : (
        <ul className="flex flex-col gap-2.5">
          {files.map((file) => (
            <li
              key={file.id}
              className="rounded-[12px] border border-line bg-surface p-4 transition-colors hover:border-line-strong"
            >
              <div className="flex items-start justify-between gap-3">
                <div className="min-w-0">
                  <h3 className="truncate font-display text-[0.98rem] font-semibold">{file.title}</h3>
                  <p className="mt-0.5 font-mono text-[0.68rem] text-ink-3">
                    {fileKindLabel(file.kind)} · {formatFileSize(file.sizeBytes)}
                    {file.pageCount > 0 ? ` · ${file.pageCount} 页` : ""}
                    {" · "}
                    {file.addedBy.name} 存入
                  </p>
                </div>
                <div className="flex shrink-0 items-center gap-1.5">
                  <GroupFilePreview file={file} pageCount={file.pageCount} />
                  <button
                    type="button"
                    onClick={() => void handleDelete(file.id)}
                    className="rounded-[3px] px-2 py-0.5 text-[0.72rem] text-danger hover:bg-danger-wash"
                  >
                    移除
                  </button>
                </div>
              </div>

              {file.description && (
                <p className="mt-1.5 text-[0.84rem] leading-relaxed text-ink-2">{file.description}</p>
              )}

              {file.tags.length > 0 && (
                <div className="mt-2 flex flex-wrap gap-1.5">
                  {file.tags.map((t) => (
                    <button
                      key={t}
                      type="button"
                      onClick={() => {
                        setTag(t);
                        void load({ tag: t });
                      }}
                      className="rounded-full bg-surface-2 px-2 py-0.5 font-mono text-[0.66rem] text-ink-2 hover:bg-seal-wash hover:text-seal"
                    >
                      {t}
                    </button>
                  ))}
                </div>
              )}
            </li>
          ))}
        </ul>
      )}

      {cursor && (
        <button
          type="button"
          onClick={() => void load({ tag: tag.trim() || undefined, cursor })}
          disabled={loadingMore}
          className="mx-auto rounded-[6px] border border-line-strong px-4 py-1.5 text-[0.82rem] font-semibold text-ink hover:border-ink-3 disabled:opacity-60"
        >
          {loadingMore ? "加载中…" : "加载更多"}
        </button>
      )}
    </div>
  );
}

/**
 * 按文件 ID 存入文件库。
 *
 * 为什么不从帖子一键存入：posts 表没有附件概念（post_attachments 至今
 * 没有写入方），帖子列表里拿不到 fileId。与其做一个点了没反应的按钮，
 * 不如给一个诚实的入口——从论文页复制文件 ID 粘进来即可。
 */
function SaveFileForm({
  slug,
  onSaved,
  onError,
}: {
  slug: string;
  onSaved: (file: GroupFileDTO) => void;
  onError: (message: string) => void;
}) {
  const [open, setOpen] = useState(false);
  const [fileId, setFileId] = useState("");
  const [title, setTitle] = useState("");
  const [description, setDescription] = useState("");
  const [tags, setTags] = useState("");
  const [saving, setSaving] = useState(false);

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    const id = fileId.trim();
    if (!id) {
      onError("请填写文件 ID。");
      return;
    }
    setSaving(true);
    try {
      const created = await saveGroupFile(slug, {
        fileId: id,
        title: title.trim() || undefined,
        description: description.trim() || undefined,
        tags: parseTags(tags),
      });
      onSaved(created);
      setFileId("");
      setTitle("");
      setDescription("");
      setTags("");
      setOpen(false);
    } catch (err) {
      // 409 = 该文件已在本群文件库，属于正常业务结果，提示得直白些。
      onError(
        err instanceof ApiError && err.status === 409
          ? "该文件已在文件库中。"
          : err instanceof ApiError
            ? err.message
            : "存入失败，请检查文件 ID 是否正确。",
      );
    } finally {
      setSaving(false);
    }
  }

  if (!open) {
    return (
      <button
        type="button"
        onClick={() => setOpen(true)}
        className="self-start rounded-[6px] border border-line-strong px-3.5 py-1.5 text-[0.82rem] font-semibold text-ink hover:border-ink-3"
      >
        + 存入文件
      </button>
    );
  }

  return (
    <form
      onSubmit={submit}
      className="flex flex-col gap-2.5 rounded-[12px] border border-line bg-surface p-4"
    >
      <input
        value={fileId}
        onChange={(e) => setFileId(e.target.value)}
        placeholder="文件 ID（从论文页或上传结果里复制）"
        className="rounded-[6px] border border-line-strong bg-paper px-3 py-1.5 font-mono text-[0.8rem] text-ink focus:border-seal focus:outline-none"
      />
      <input
        value={title}
        onChange={(e) => setTitle(e.target.value)}
        maxLength={GROUP_FILE_LIMITS.titleMax}
        placeholder="标题（可选，默认用原文件名）"
        className="rounded-[6px] border border-line-strong bg-paper px-3 py-1.5 text-[0.86rem] text-ink focus:border-seal focus:outline-none"
      />
      <input
        value={description}
        onChange={(e) => setDescription(e.target.value)}
        maxLength={GROUP_FILE_LIMITS.descriptionMax}
        placeholder="说明（可选）"
        className="rounded-[6px] border border-line-strong bg-paper px-3 py-1.5 text-[0.86rem] text-ink focus:border-seal focus:outline-none"
      />
      <input
        value={tags}
        onChange={(e) => setTags(e.target.value)}
        placeholder="标签（用逗号分隔，最多 10 个）"
        className="rounded-[6px] border border-line-strong bg-paper px-3 py-1.5 text-[0.86rem] text-ink focus:border-seal focus:outline-none"
      />
      <div className="flex gap-2">
        <button
          type="submit"
          disabled={saving}
          className="rounded-[6px] bg-seal px-3.5 py-1.5 text-[0.82rem] font-semibold text-white hover:bg-seal-deep disabled:opacity-60"
        >
          {saving ? "存入中…" : "存入"}
        </button>
        <button
          type="button"
          onClick={() => setOpen(false)}
          className="rounded-[6px] border border-line-strong px-3.5 py-1.5 text-[0.82rem] text-ink-2 hover:border-ink-3"
        >
          取消
        </button>
      </div>
    </form>
  );
}
