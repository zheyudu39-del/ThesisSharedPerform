"use client";

import Collaboration from "@tiptap/extension-collaboration";
import CollaborationCaret from "@tiptap/extension-collaboration-caret";
import { EditorContent, useEditor, type Editor } from "@tiptap/react";
import StarterKit from "@tiptap/starter-kit";
import { BlockMath, InlineMath } from "@tiptap/extension-mathematics";
import Image from "@tiptap/extension-image";
import { Table } from "@tiptap/extension-table";
import { TableRow } from "@tiptap/extension-table-row";
import { TableCell } from "@tiptap/extension-table-cell";
import { TableHeader } from "@tiptap/extension-table-header";
import { useEffect, useMemo, useRef, useState } from "react";
import * as Y from "yjs";
import { HocuspocusProvider } from "@hocuspocus/provider";
import { getCollabToken, type CollabToken } from "@/lib/documents";

/**
 * 协作文档编辑器（TipTap + Yjs + Hocuspocus）。
 *
 * 连接流程：
 *   1. 向 Go API 换取短期协同令牌（会话在 HttpOnly Cookie 里，JS 读不到）；
 *   2. 用 @hocuspocus/provider 连接协同服务，文档名 = 文档 UUID；
 *      注意：不能用 y-websocket —— Hocuspocus 4.x 使用自己的握手协议，
 *      标准 y-protocol 客户端虽然能建立 WebSocket，但服务端钩子不会被触发。
 *   3. TipTap 通过 Collaboration 扩展把 Y.Doc 绑定到编辑器。
 *
 * 降级：取不到令牌或连接失败时，编辑器变为只读并给出提示，
 * 而不是白屏——用户至少能看到内容（若已有快照则从服务端加载）。
 */

type ConnectionState = "connecting" | "connected" | "disconnected" | "unavailable";

/** 与 apps/ws/src/reload.js 的 ROLLBACK_SIGNAL 保持一致。 */
const ROLLBACK_SIGNAL = "thesis:rollback";

export type DocumentEditorProps = {
  documentId: string;
  /** 当前用户在文档中的角色。viewer 只能看。 */
  role: "owner" | "editor" | "viewer";
  /** 显示名，用于协作者光标标签。 */
  userName: string;
  /** 用户 ID，用于生成稳定的光标颜色。 */
  userId: string;
  /**
   * 编辑器就绪回调。
   *
   * 导出需要 editor.getHTML()：正文是 Yjs 二进制，服务端没有解析实现，
   * 浏览器里的 TipTap 实例是最可靠的序列化入口。父组件通过这个回调
   * 拿到实例，避免把整个编辑器状态提升到父级。
   */
  onReady?: (editor: Editor | null) => void;
};

/** 由用户 ID 派生一个稳定且饱和度适中的光标颜色。 */
function cursorColor(userId: string): string {
  let hash = 0;
  for (let i = 0; i < userId.length; i++) {
    hash = (hash * 31 + userId.charCodeAt(i)) | 0;
  }
  const hue = Math.abs(hash) % 360;
  return `hsl(${hue} 65% 48%)`;
}

export function DocumentEditor({
  documentId,
  role,
  userName,
  userId,
  onReady,
}: DocumentEditorProps) {
  const [collab, setCollab] = useState<CollabToken | null>(null);
  const [state, setState] = useState<ConnectionState>("connecting");
  const [error, setError] = useState<string | null>(null);
  const [peers, setPeers] = useState<string[]>([]);

  const editable = role === "owner" || role === "editor";

  // generation 每次「回滚重建」自增，用于强制重建 Y.Doc 与 provider。
  //
  // 为什么不能用 useMemo 固定一个 Y.Doc：Yjs 是 CRDT，本地文档保留着
  // 自己的编辑历史。回滚后若只重连，Yjs 会把服务端快照与本地状态**合并**，
  // 本地编辑依然胜出——用户看到的还是回滚前的内容。
  // 只有丢弃本地 Y.Doc、从空文档重新同步，回滚才真正生效。
  const [generation, setGeneration] = useState(0);
  const ydoc = useMemo(() => new Y.Doc(), [generation]);
  const providerRef = useRef<HocuspocusProvider | null>(null);
  const [providerReady, setProviderReady] = useState(false);

  // 1) 换取协同令牌。
  useEffect(() => {
    let cancelled = false;
    async function load() {
      try {
        const token = await getCollabToken(documentId);
        if (!cancelled) setCollab(token);
      } catch (err) {
        if (cancelled) return;
        setState("unavailable");
        setError(
          err instanceof Error ? err.message : "无法获取协同令牌，编辑器已降级为只读。",
        );
      }
    }
    void load();
    return () => {
      cancelled = true;
    };
  }, [documentId]);

  // 2) 建立 WebSocket 连接。
  useEffect(() => {
    if (!collab) return;

    const provider = new HocuspocusProvider({
      url: collab.wsUrl,
      name: documentId,
      document: ydoc,
      token: collab.token,
      onConnect: () => setState("connected"),
      onDisconnect: () => setState("disconnected"),
      onSynced: () => setState("connected"),
      onAuthenticationFailed: () => {
        setState("unavailable");
        setError("协同鉴权失败，编辑器已降级为只读。");
      },
      // 服务端回滚快照后广播此信号。丢弃本地 Y.Doc 并重建 provider，
      // 从服务端的最新快照重新同步——这是回滚能真正生效的唯一途径。
      onStateless: ({ payload }) => {
        if (payload !== ROLLBACK_SIGNAL) return;
        setState("connecting");
        setError(null);
        // 断开当前连接；下一个 effect 会用新的 Y.Doc 重新建连。
        providerRef.current?.destroy();
        providerRef.current = null;
        setProviderReady(false);
        setGeneration((g) => g + 1);
      },
    });

    // 协作者列表（awareness）。
    const updatePeers = () => {
      const names = Array.from(provider.awareness?.getStates().values() ?? [])
        .map((s) => (s as { user?: { name?: string } }).user?.name)
        .filter((n): n is string => Boolean(n));
      setPeers(Array.from(new Set(names)));
    };
    provider.awareness?.on("change", updatePeers);
    updatePeers();

    provider.awareness?.setLocalStateField("user", {
      name: userName,
      color: cursorColor(userId),
    });

    providerRef.current = provider;
    setProviderReady(true);

    return () => {
      providerRef.current = null;
      setProviderReady(false);
      provider.awareness?.off("change", updatePeers);
      provider.destroy();
    };
  }, [collab, documentId, ydoc, userName, userId]);

  // 3) 绑定 TipTap。
  const editor = useEditor(
    {
      editable,
      extensions: [
        // CRDT 自己管理历史，必须关掉 StarterKit 的 undoRedo（TipTap 3 的写法）。
        StarterKit.configure({ undoRedo: false }),
        // 数学公式：块级与行内两种，由 KaTeX 渲染。
        // 节点名固定为 blockMath / inlineMath，导入时的 HTML 必须用这两个标签，
        // 否则 TipTap 无法识别。
        BlockMath,
        InlineMath,
        // 图片：导入 Word 时内嵌的图。地址是 /api/v1/documents/{id}/assets/{assetId}，
        // 由服务端按文档权限校验后转发内容。
        Image.configure({ inline: false, allowBase64: false }),
        // 表格：四件套必须一起注册，缺一个都会让表格节点无法解析。
        Table.configure({ resizable: false }),
        TableRow,
        TableHeader,
        TableCell,
        Collaboration.configure({ document: ydoc }),
        // 光标扩展依赖 provider，必须等 WebSocket 建好再挂。
        ...(providerReady && providerRef.current
          ? [CollaborationCaret.configure({ provider: providerRef.current })]
          : []),
      ],
      editorProps: {
        attributes: {
          class:
            "prose-editor min-h-[420px] px-6 py-5 text-[0.95rem] leading-[1.8] focus:outline-none",
        },
      },
    },
    [ydoc, editable, providerReady],
  );

  // 把编辑器实例交给父组件（用于导出）。卸载时回调 null，
  // 防止父组件在编辑器销毁后仍持有过期实例。
  useEffect(() => {
    onReady?.(editor ?? null);
    return () => onReady?.(null);
  }, [editor, onReady]);

  return (
    <div className="flex flex-col gap-3">
      <EditorStatus state={state} peers={peers} editable={editable} error={error} />
      <div className="overflow-hidden rounded-[12px] border border-line bg-surface shadow-[var(--shadow-1)]">
        <EditorContent editor={editor} />
      </div>
    </div>
  );
}

function EditorStatus({
  state,
  peers,
  editable,
  error,
}: {
  state: ConnectionState;
  peers: string[];
  editable: boolean;
  error: string | null;
}) {
  const label: Record<ConnectionState, string> = {
    connecting: "正在连接协同服务…",
    connected: "已连接",
    disconnected: "连接已断开，正在重连…",
    unavailable: "协同服务不可用",
  };
  const tone: Record<ConnectionState, string> = {
    connecting: "bg-warn-wash text-warn",
    connected: "bg-good-wash text-good",
    disconnected: "bg-warn-wash text-warn",
    unavailable: "bg-danger-wash text-danger",
  };

  return (
    <div className="flex flex-wrap items-center gap-2.5">
      <span className={`inline-flex items-center gap-1.5 rounded-full px-2.5 py-1 text-[0.72rem] font-semibold ${tone[state]}`}>
        <span className="size-1.5 rounded-full bg-current" aria-hidden />
        {label[state]}
      </span>

      {!editable && (
        <span className="rounded-full bg-surface-2 px-2.5 py-1 font-mono text-[0.7rem] text-ink-3">
          只读
        </span>
      )}

      {peers.length > 0 && (
        <span className="font-mono text-[0.7rem] text-ink-3">
          在线 {peers.length} 人：{peers.join("、")}
        </span>
      )}

      {error && (
        <span className="text-[0.75rem] leading-relaxed text-danger">{error}</span>
      )}
    </div>
  );
}
