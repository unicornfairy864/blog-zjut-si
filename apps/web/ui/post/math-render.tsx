"use client";

import katex from "katex";
import { useEffect } from "react";

import "katex/dist/katex.min.css";

/**
 * 客户端 KaTeX（agent.md §11）。
 *
 * 服务端只输出 `<span class="math" data-tex="…">` 占位；本组件由
 * `PostContent` 动态 import，只有页面里真的出现公式时才会加载 KaTeX 与它的 CSS/字体。
 * 渲染失败时保留 TeX 原文，不抛错。
 */
export default function MathRender() {
  useEffect(() => {
    const nodes = Array.from(
      document.querySelectorAll<HTMLElement>("span.math[data-tex]"),
    );
    for (const node of nodes) {
      if (node.dataset.rendered === "1") {
        continue;
      }
      const tex = node.dataset.tex ?? "";
      if (!tex) {
        continue;
      }
      try {
        katex.render(tex, node, {
          displayMode: node.classList.contains("math-display"),
          throwOnError: false,
          strict: false,
          trust: false,
        });
        node.dataset.rendered = "1";
      } catch {
        // 保留 $…$ 原文即可。
      }
    }
  }, []);

  return null;
}
