import { describe, expect, it } from "vitest";

import { applyMarkdownEdit, withImageWidth } from "@/ui/admin/editor-toolbar";

/**
 * 编辑器工具栏的纯函数（agent.md §8「Markdown 编辑」「编辑器图片」）。
 *
 * 只测字符串改写：DOM 侧的光标恢复与图片框拖拽由组件负责，不在这里覆盖。
 */
describe("applyMarkdownEdit", () => {
  it("无选区时插入占位文本并选中它", () => {
    const edit = applyMarkdownEdit("abc", 3, 3, {
      prefix: "**",
      suffix: "**",
      placeholder: "粗体",
    });
    expect(edit.value).toBe("abc**粗体**");
    expect(edit.value.slice(edit.selectionStart, edit.selectionEnd)).toBe("粗体");
  });

  it("有选区时包裹选区内容", () => {
    const edit = applyMarkdownEdit("abc", 0, 3, {
      prefix: "*",
      suffix: "*",
      placeholder: "斜体",
    });
    expect(edit.value).toBe("*abc*");
    expect(edit.value.slice(edit.selectionStart, edit.selectionEnd)).toBe("abc");
  });

  it("逐行前缀只给非空行加标记", () => {
    const edit = applyMarkdownEdit("第一行\n\n第二行", 0, 10, {
      linePrefix: "- ",
      placeholder: "列表项",
    });
    expect(edit.value).toBe("- 第一行\n\n- 第二行");
  });

  it("块级片段独占整行", () => {
    const edit = applyMarkdownEdit("上文", 2, 2, {
      prefix: "```\n",
      suffix: "\n```",
      placeholder: "代码",
      block: true,
    });
    expect(edit.value).toBe("上文\n```\n代码\n```");
    expect(edit.value.slice(edit.selectionStart, edit.selectionEnd)).toBe("代码");
  });

  it("选区越界时夹到正文范围内", () => {
    const edit = applyMarkdownEdit("abc", 99, 120, {
      prefix: "$",
      suffix: "$",
      placeholder: "x^2",
    });
    expect(edit.value).toBe("abc$x^2$");
  });
});

describe("withImageWidth", () => {
  it("给目标图片补上 width", () => {
    const md = "![a](https://x/1.webp)\n\n![b](https://x/2.webp)";
    expect(withImageWidth(md, 1, 480)).toBe(
      "![a](https://x/1.webp)\n\n![b](https://x/2.webp){width=480}",
    );
  });

  it("已有属性时只更新 width，保留其它属性", () => {
    const md = "![a](https://x/1.webp){height=270 width=100}";
    expect(withImageWidth(md, 0, 960)).toBe(
      "![a](https://x/1.webp){height=270 width=960}",
    );
  });

  it("带 title 的图片语法同样命中", () => {
    const md = '![a](https://x/1.webp "标题")';
    expect(withImageWidth(md, 0, 320)).toBe(
      '![a](https://x/1.webp "标题"){width=320}',
    );
  });

  it("序号不存在时原样返回", () => {
    const md = "![a](https://x/1.webp)";
    expect(withImageWidth(md, 3, 480)).toBe(md);
  });
});
