"use client";

import { useEffect, useState } from "react";
import type { Editor } from "@tiptap/react";

/**
 * 文档大纲面板（PRD D-01）。
 *
 * 编辑器本来就有 heading 节点（StarterKit 自带 1-6 级），但没有任何
 * 地方能一眼看到文章结构。这里按 heading 层级生成大纲，点击跳到对应位置。
 *
 * 为什么不用 TipTap 的 tableOfContents 扩展：那个扩展会往文档里插入
 * 一个真实节点，而正文是 Yjs 共享状态——插入的节点会同步给所有协作者，
 * 相当于替别人改了文档。这里只读 editor.state.doc，不写入。
 */

type Heading = {
  level: number;
  text: string;
  pos: number;
};

const MAX_HEADINGS = 200;

/** 从文档里抽出所有 heading。 */
function collectHeadings(editor: Editor): Heading[] {
  const out: Heading[] = [];
  editor.state.doc.descendants((node, pos) => {
    if (node.type.name === "heading") {
      const text = node.textContent.trim();
      if (text) {
        out.push({ level: node.attrs.level as number, text, pos });
      }
      if (out.length >= MAX_HEADINGS) return false;
    }
    return true;
  });
  return out;
}

export function DocOutline({ editor }: { editor: Editor | null }) {
  const [headings, setHeadings] = useState<Heading[]>([]);

  useEffect(() => {
    if (!editor) {
      setHeadings([]);
      return;
    }
    const refresh = () => setHeadings(collectHeadings(editor));
    refresh();
    // 正文变化（包括协作者的编辑）都要刷新大纲。
    editor.on("update", refresh);
    return () => {
      editor.off("update", refresh);
    };
  }, [editor]);

  if (!editor || headings.length === 0) {
    return null;
  }

  return (
    <section className="rounded-[12px] border border-line bg-surface p-3.5">
      <h2 className="mb-2 font-mono text-[0.68rem] tracking-[0.1em] text-ink-3 uppercase">
        大纲 · {headings.length}
      </h2>
      <nav aria-label="文档大纲">
        <ul className="flex flex-col gap-0.5">
          {headings.map((h, i) => (
            <li key={`${h.pos}-${i}`}>
              <button
                type="button"
                onClick={() => {
                  // 把光标移到标题开头并滚动到可见区域。
                  editor.chain().focus().setTextSelection(h.pos + 1).scrollIntoView().run();
                }}
                className="block w-full truncate rounded-[3px] py-0.5 pr-1 text-left text-[0.78rem] text-ink-2 hover:bg-seal-wash hover:text-seal"
                // 每深一级缩进 0.75rem，上限 5 级避免过窄。
                style={{ paddingLeft: `${(Math.min(h.level, 5) - 1) * 0.75}rem` }}
                title={h.text}
              >
                {h.text}
              </button>
            </li>
          ))}
        </ul>
      </nav>
    </section>
  );
}
