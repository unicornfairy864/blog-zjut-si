"use client";

import {
  Bold,
  Code,
  Image as ImageIcon,
  Italic,
  Link as LinkIcon,
  List,
  Quote,
  Sigma,
  SquareCode,
  Superscript,
  Table,
  type LucideIcon,
} from "lucide-react";

import { Button } from "@/ui/shadcn/button";

/** 一次 Markdown 插入的结果：新正文 + 插入后需要恢复的光标/选区。 */
export interface MarkdownEdit {
  value: string;
  selectionStart: number;
  selectionEnd: number;
}

/**
 * 插入规则（agent.md §8「Markdown 编辑」）。
 *
 * 有选区时包裹选区内容，没有选区时插入占位文本并选中它，用户可以直接开始输入。
 */
interface MarkdownEditSpec {
  /** 无选区时插入并选中的占位文本。 */
  placeholder: string;
  /** 包裹前缀。 */
  prefix?: string;
  /** 包裹后缀。 */
  suffix?: string;
  /** 逐行前缀（引用、列表）：给选区的每一行加前缀，而不是整段包裹。 */
  linePrefix?: string;
  /** 块级片段：强制独占整行，否则代码围栏 / `$$` 会被当成行内内容。 */
  block?: boolean;
}

/**
 * 在光标处插入 Markdown 片段，或在有选区时包裹选区。
 *
 * 纯函数，只做字符串计算：调用方拿到结果后负责写回表单并恢复光标，
 * 这样工具栏既不需要知道 react-hook-form，也不会被 textarea 的受控状态卡住。
 */
export function applyMarkdownEdit(
  value: string,
  selectionStart: number,
  selectionEnd: number,
  spec: MarkdownEditSpec,
): MarkdownEdit {
  // 选区可能来自上一次渲染，先夹到当前正文范围内，避免 slice 出界。
  const start = Math.max(0, Math.min(selectionStart, value.length));
  const end = Math.max(start, Math.min(selectionEnd, value.length));
  const selected = value.slice(start, end);

  if (spec.linePrefix) {
    const target = selected || spec.placeholder;
    // 空行不加前缀，否则列表/引用中间会多出一个光秃秃的标记。
    const prefixed = target
      .split("\n")
      .map((line) => (line ? `${spec.linePrefix}${line}` : line))
      .join("\n");
    const next = `${value.slice(0, start)}${prefixed}${value.slice(end)}`;
    return {
      value: next,
      selectionStart: start,
      selectionEnd: start + prefixed.length,
    };
  }

  const prefix = spec.prefix ?? "";
  const suffix = spec.suffix ?? "";
  const inner = selected || spec.placeholder;
  // 块级片段前后各补一个换行，保证围栏与 `$$` 落在独立行上。
  const leading = spec.block && start > 0 && value[start - 1] !== "\n" ? "\n" : "";
  const trailing = spec.block && end < value.length && value[end] !== "\n" ? "\n" : "";

  const insert = `${leading}${prefix}${inner}${suffix}${trailing}`;
  const innerStart = start + leading.length + prefix.length;
  return {
    value: `${value.slice(0, start)}${insert}${value.slice(end)}`,
    selectionStart: innerStart,
    selectionEnd: innerStart + inner.length,
  };
}

/** 正文图片语法的正则；与 §11 的 `![alt](src){width=480}` 一一对应。 */
const IMAGE_PATTERN = /!\[[^\]]*\]\([^)\s]+(?:\s+"[^"]*")?\)(?:\{[^}]*\})?/g;

/**
 * 把第 `index` 张图片的显示宽度写回 Markdown（agent.md §11「图片尺寸」）。
 *
 * 只动目标图片：已有 `{…}` 时保留其它属性、只更新 `width`，没有时补一个 `{width=N}`。
 * 预览里的 `<img>` 顺序与正文里图片语法的出现顺序一致，所以用序号对应即可。
 */
export function withImageWidth(contentMd: string, index: number, width: number): string {
  // 每次新建正则：带 g 的正则在多次 replace 之间会残留 lastIndex。
  const pattern = new RegExp(IMAGE_PATTERN.source, "g");
  let current = -1;
  return contentMd.replace(pattern, (match) => {
    current += 1;
    if (current !== index) {
      return match;
    }

    const braceStart = match.lastIndexOf("{");
    if (braceStart < 0 || !match.endsWith("}")) {
      return `${match}{width=${width}}`;
    }

    const fields = match
      .slice(braceStart + 1, -1)
      .split(/\s+/)
      .filter(Boolean);
    const nextFields: string[] = [];
    let replaced = false;
    for (const field of fields) {
      if (field.startsWith("width=")) {
        nextFields.push(`width=${width}`);
        replaced = true;
      } else {
        nextFields.push(field);
      }
    }
    if (!replaced) {
      nextFields.push(`width=${width}`);
    }
    return `${match.slice(0, braceStart)}{${nextFields.join(" ")}}`;
  });
}

/** 工具栏按钮：图标 + 无障碍名称 + 插入规则。 */
interface ToolbarAction {
  label: string;
  icon: LucideIcon;
  spec: MarkdownEditSpec;
}

/** 按钮顺序参考 Typora 的常用格式区（agent.md §8）。 */
const TOOLBAR_ACTIONS: ToolbarAction[] = [
  {
    label: "粗体",
    icon: Bold,
    spec: { prefix: "**", suffix: "**", placeholder: "粗体" },
  },
  {
    label: "斜体",
    icon: Italic,
    spec: { prefix: "*", suffix: "*", placeholder: "斜体" },
  },
  {
    label: "行内代码",
    icon: Code,
    spec: { prefix: "`", suffix: "`", placeholder: "代码" },
  },
  {
    label: "代码块",
    icon: SquareCode,
    spec: { prefix: "```\n", suffix: "\n```", placeholder: "代码", block: true },
  },
  {
    label: "链接",
    icon: LinkIcon,
    spec: { prefix: "[", suffix: "](https://)", placeholder: "链接文字" },
  },
  {
    label: "引用",
    icon: Quote,
    spec: { linePrefix: "> ", placeholder: "引用" },
  },
  {
    label: "无序列表",
    icon: List,
    spec: { linePrefix: "- ", placeholder: "列表项" },
  },
  {
    label: "表格",
    icon: Table,
    spec: {
      placeholder: "| 列 1 | 列 2 |\n| --- | --- |\n| 单元格 | 单元格 |",
      block: true,
    },
  },
  {
    // 行内公式：与服务端渲染管线的 `$…$` 对应（agent.md §11）。
    label: "行内公式",
    icon: Superscript,
    spec: { prefix: "$", suffix: "$", placeholder: "x^2" },
  },
  {
    // 块级公式：`$$` 必须独占一行，交给 block 补换行。
    label: "块级公式",
    icon: Sigma,
    spec: { prefix: "$$\n", suffix: "\n$$", placeholder: "E = mc^2", block: true },
  },
];

export interface EditorToolbarProps {
  /** 正文 textarea 的 DOM id；工具栏据此读取当前选区。 */
  textareaId: string;
  /** 正文当前值，textarea 尚未挂载时用它兜底。 */
  value: string;
  /** 把插入结果交回 post-editor 写进表单并恢复光标。 */
  onInsert: (edit: MarkdownEdit) => void;
  /** 图片按钮：打开媒体库选择器。 */
  onPickImage: () => void;
  /** 上传进行中时禁用，避免插入位置与上传结果交错。 */
  disabled?: boolean;
}

/**
 * 正文工具栏（agent.md §8「Markdown 编辑」）。
 *
 * 只负责把「在光标处插入 / 包裹选区」翻译成一次 {@link MarkdownEdit}，
 * 表单值的写入由 post-editor 统一处理，避免这里也持有表单状态。
 */
export function EditorToolbar({
  textareaId,
  value,
  onInsert,
  onPickImage,
  disabled = false,
}: EditorToolbarProps) {
  /** 读取 textarea 的选区；拿不到（未挂载）时退化为追加到正文末尾。 */
  function currentSelection(): { start: number; end: number } {
    const textarea = document.getElementById(textareaId) as HTMLTextAreaElement | null;
    return {
      start: textarea?.selectionStart ?? value.length,
      end: textarea?.selectionEnd ?? value.length,
    };
  }

  function apply(spec: MarkdownEditSpec) {
    const { start, end } = currentSelection();
    onInsert(applyMarkdownEdit(value, start, end, spec));
  }

  return (
    <div
      role="toolbar"
      aria-label="Markdown 工具栏"
      className="flex flex-wrap items-center gap-1 rounded-md border border-border bg-muted/40 p-1"
    >
      {TOOLBAR_ACTIONS.map((action) => (
        <Button
          key={action.label}
          type="button"
          variant="ghost"
          size="sm"
          aria-label={action.label}
          title={action.label}
          disabled={disabled}
          onClick={() => apply(action.spec)}
        >
          <action.icon className="h-4 w-4" aria-hidden="true" />
        </Button>
      ))}
      <span aria-hidden="true" className="mx-1 h-5 w-px bg-border" />
      <Button
        type="button"
        variant="ghost"
        size="sm"
        aria-label="插入图片"
        title="插入图片"
        disabled={disabled}
        onClick={onPickImage}
      >
        <ImageIcon className="h-4 w-4" aria-hidden="true" />
      </Button>
    </div>
  );
}
