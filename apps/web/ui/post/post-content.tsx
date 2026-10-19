"use client";

import dynamic from "next/dynamic";
import { useEffect, useRef, useState } from "react";

import { cn } from "@/lib/utils";

/** KaTeX 只在页面真的有公式时加载（agent.md §8 / §11）。 */
const MathRender = dynamic(() => import("./math-render"), { ssr: false });

export interface PostContentProps {
  html: string;
  className?: string;
}

/**
 * 正文渲染容器。
 *
 * HTML 由服务端 goldmark + bluemonday 产出（已过滤），这里只做两件客户端增强：
 * 1. 有公式时懒加载 KaTeX 替换占位 span；
 * 2. 给每个代码块加复制按钮（无 JS 时正文仍然完整可读）。
 */
export function PostContent({ html, className }: PostContentProps) {
  const containerRef = useRef<HTMLDivElement>(null);
  const [hasMath, setHasMath] = useState(false);

  useEffect(() => {
    const root = containerRef.current;
    if (!root) {
      return;
    }

    setHasMath(root.querySelector("span.math[data-tex]") !== null);

    const cleanups: Array<() => void> = [];
    const blocks = Array.from(root.querySelectorAll<HTMLPreElement>("pre"));
    for (const block of blocks) {
      if (block.dataset.copyReady === "1") {
        continue;
      }
      block.dataset.copyReady = "1";

      const button = document.createElement("button");
      button.type = "button";
      button.textContent = "复制";
      button.className = "code-copy";
      button.setAttribute("aria-label", "复制代码");

      const onClick = () => {
        const code = block.querySelector("code");
        const text = (code ?? block).textContent ?? "";
        void navigator.clipboard?.writeText(text).then(
          () => {
            button.textContent = "已复制";
            window.setTimeout(() => {
              button.textContent = "复制";
            }, 1500);
          },
          () => {
            button.textContent = "复制失败";
          },
        );
      };

      button.addEventListener("click", onClick);
      block.appendChild(button);
      cleanups.push(() => {
        button.removeEventListener("click", onClick);
        button.remove();
        delete block.dataset.copyReady;
      });
    }

    return () => {
      for (const cleanup of cleanups) {
        cleanup();
      }
    };
  }, [html]);

  return (
    <>
      <div
        ref={containerRef}
        className={cn("prose-blog", className)}
        // 内容来自服务端渲染 + bluemonday 白名单过滤，不是用户直传的 HTML（§9 / §11）。
        dangerouslySetInnerHTML={{ __html: html }}
      />
      {/*
        key={html}：编辑器预览会随输入替换整段 HTML，而 MathRender 只在挂载时渲染一次；
        换 key 强制重挂载，新增的公式才会被 KaTeX 替换（公开页 html 固定，不受影响）。
      */}
      {hasMath ? <MathRender key={html} /> : null}
    </>
  );
}
