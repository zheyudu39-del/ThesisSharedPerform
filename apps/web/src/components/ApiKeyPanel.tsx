"use client";

import { useCallback, useEffect, useState } from "react";
import {
  ApiError,
  SCOPE_LABEL,
  createApiKey,
  listApiKeys,
  revokeApiKey,
  type ApiKeyDTO,
  type ApiScope,
} from "@/lib/apikeys";

const ALL_SCOPES: ApiScope[] = ["read:papers", "read:posts", "read:users"];

/**
 * API Key 管理面板（设置页内嵌）。
 *
 * 关键交互约束：明文只在创建响应里出现一次，服务端不保存。
 * 因此创建成功后用一个「只此一次」的提示块展示完整 Key，
 * 并明确告知用户关闭后无法再次查看。
 */
export function ApiKeyPanel() {
  const [keys, setKeys] = useState<ApiKeyDTO[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [creating, setCreating] = useState(false);
  const [newKey, setNewKey] = useState<string | null>(null);
  const [copied, setCopied] = useState(false);

  const load = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      setKeys(await listApiKeys());
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "加载 API Key 失败。");
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    void load();
  }, [load]);

  async function handleRevoke(id: string) {
    setError(null);
    try {
      await revokeApiKey(id);
      setKeys((prev) => prev.filter((k) => k.id !== id));
    } catch (err) {
      setError(err instanceof ApiError ? err.message : "撤销失败，请稍后重试。");
    }
  }

  async function copyKey() {
    if (!newKey) return;
    try {
      await navigator.clipboard.writeText(newKey);
      setCopied(true);
      window.setTimeout(() => setCopied(false), 2000);
    } catch {
      // 剪贴板不可用（非安全上下文 / 权限被拒）：用户仍可手动选中复制。
    }
  }

  return (
    <section className="rounded-[12px] border border-line bg-surface p-5">
      <div className="mb-4">
        <h2 className="font-display text-[1.05rem] font-semibold">开放 API</h2>
        <p className="mt-1 max-w-[62ch] text-[0.84rem] leading-relaxed text-ink-2">
          用 API Key 以只读方式访问公开的论文、帖子与用户资料，适合做文献聚合或个人站点嵌入。
          明文密钥只显示一次，服务端只保存哈希。
        </p>
      </div>

      {newKey && (
        <div className="mb-4 rounded-[6px] border border-seal/40 bg-seal-wash p-3.5">
          <p className="mb-1.5 text-[0.8rem] font-semibold text-seal-deep">
            请立即复制并妥善保存 —— 关闭后无法再次查看
          </p>
          <div className="flex items-center gap-2">
            <code className="min-w-0 flex-1 overflow-x-auto rounded-[3px] border border-seal/30 bg-paper px-2 py-1.5 font-mono text-[0.78rem] text-ink">
              {newKey}
            </code>
            <button
              type="button"
              onClick={() => void copyKey()}
              className="shrink-0 rounded-[3px] bg-seal px-2.5 py-1.5 text-[0.76rem] font-semibold text-white hover:bg-seal-deep"
            >
              {copied ? "已复制" : "复制"}
            </button>
            <button
              type="button"
              onClick={() => setNewKey(null)}
              className="shrink-0 rounded-[3px] border border-seal/30 px-2.5 py-1.5 text-[0.76rem] text-seal-deep hover:bg-seal/10"
            >
              我已保存
            </button>
          </div>
        </div>
      )}

      {error && (
        <p role="alert" className="mb-3 rounded-[6px] border border-danger/40 bg-danger-wash px-3 py-2 text-[0.8rem] text-danger">
          {error}
        </p>
      )}

      {loading ? (
        <div className="flex flex-col gap-2" aria-hidden>
          {[0, 1].map((i) => (
            <div key={i} className="h-12 animate-pulse rounded-[6px] bg-surface-2" />
          ))}
        </div>
      ) : keys.length === 0 ? (
        <p className="rounded-[6px] border border-dashed border-line-strong px-4 py-6 text-center text-[0.84rem] text-ink-3">
          还没有 API Key。
        </p>
      ) : (
        <ul className="flex flex-col gap-2">
          {keys.map((key) => (
            <li
              key={key.id}
              className="flex flex-wrap items-center gap-2 rounded-[6px] border border-line px-3.5 py-2.5"
            >
              <div className="min-w-0">
                <p className="text-[0.88rem] font-medium">{key.name}</p>
                <p className="font-mono text-[0.7rem] text-ink-3">
                  {key.prefix}… · {key.scopes.map((s) => SCOPE_LABEL[s]).join(" / ")}
                  {key.lastUsedAt ? ` · 最后使用 ${formatDate(key.lastUsedAt)}` : " · 从未使用"}
                </p>
              </div>
              <button
                type="button"
                onClick={() => void handleRevoke(key.id)}
                className="ml-auto shrink-0 rounded-[3px] px-2 py-0.5 text-[0.74rem] text-danger hover:bg-danger-wash"
              >
                撤销
              </button>
            </li>
          ))}
        </ul>
      )}

      <CreateKeyForm
        creating={creating}
        onCreate={async (input) => {
          setCreating(true);
          setError(null);
          try {
            const created = await createApiKey(input);
            setNewKey(created.plaintext);
            setKeys((prev) => [created.key, ...prev]);
            return true;
          } catch (err) {
            setError(err instanceof ApiError ? err.message : "创建失败，请稍后重试。");
            return false;
          } finally {
            setCreating(false);
          }
        }}
      />
    </section>
  );
}

function CreateKeyForm({
  creating,
  onCreate,
}: {
  creating: boolean;
  onCreate: (input: { name: string; scopes: ApiScope[] }) => Promise<boolean>;
}) {
  const [open, setOpen] = useState(false);
  const [name, setName] = useState("");
  const [scopes, setScopes] = useState<ApiScope[]>(["read:papers"]);
  const [localError, setLocalError] = useState<string | null>(null);

  function toggleScope(scope: ApiScope) {
    setScopes((prev) =>
      prev.includes(scope) ? prev.filter((s) => s !== scope) : [...prev, scope],
    );
  }

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    if (!name.trim()) {
      setLocalError("请填写名称。");
      return;
    }
    if (scopes.length === 0) {
      setLocalError("至少选择一个权限范围。");
      return;
    }
    setLocalError(null);
    const ok = await onCreate({ name: name.trim(), scopes });
    if (ok) {
      setName("");
      setScopes(["read:papers"]);
      setOpen(false);
    }
  }

  if (!open) {
    return (
      <button
        type="button"
        onClick={() => setOpen(true)}
        className="mt-4 rounded-[6px] border border-line-strong px-4 py-1.5 text-[0.84rem] font-semibold text-ink hover:border-ink-3"
      >
        + 创建 API Key
      </button>
    );
  }

  return (
    <form onSubmit={submit} className="mt-4 flex flex-col gap-3 rounded-[6px] border border-line bg-surface-2 p-4">
      <label className="flex flex-col gap-1.5">
        <span className="font-mono text-[0.7rem] tracking-[0.08em] text-ink-3 uppercase">名称</span>
        <input
          value={name}
          onChange={(e) => setName(e.target.value)}
          maxLength={64}
          placeholder="例如：文献聚合脚本"
          className="rounded-[3px] border border-line-strong bg-paper px-3 py-1.5 text-[0.86rem] text-ink focus:border-seal focus:outline-none"
        />
      </label>

      <fieldset className="flex flex-col gap-1.5">
        <legend className="mb-1 font-mono text-[0.7rem] tracking-[0.08em] text-ink-3 uppercase">
          权限范围
        </legend>
        {ALL_SCOPES.map((scope) => (
          <label key={scope} className="flex items-center gap-2 text-[0.84rem] text-ink-2">
            <input
              type="checkbox"
              checked={scopes.includes(scope)}
              onChange={() => toggleScope(scope)}
              className="size-3.5 accent-[var(--color-seal,#8c2f2f)]"
            />
            {SCOPE_LABEL[scope]}
            <code className="font-mono text-[0.7rem] text-ink-3">{scope}</code>
          </label>
        ))}
      </fieldset>

      {localError && <p className="text-[0.78rem] text-danger">{localError}</p>}

      <div className="flex gap-2">
        <button
          type="submit"
          disabled={creating}
          className="rounded-[3px] bg-seal px-3.5 py-1.5 text-[0.82rem] font-semibold text-white hover:bg-seal-deep disabled:opacity-60"
        >
          {creating ? "创建中…" : "创建"}
        </button>
        <button
          type="button"
          onClick={() => setOpen(false)}
          className="rounded-[3px] border border-line-strong px-3.5 py-1.5 text-[0.82rem] text-ink-2 hover:border-ink-3"
        >
          取消
        </button>
      </div>
    </form>
  );
}

function formatDate(iso: string): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return "";
  return `${d.getMonth() + 1}/${d.getDate()} ${String(d.getHours()).padStart(2, "0")}:${String(
    d.getMinutes(),
  ).padStart(2, "0")}`;
}
