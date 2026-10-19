"use client";

import { useEffect, useRef, useState } from "react";

import { PostContent } from "@/ui/post/post-content";

/** 拖拽改宽的下限（px）。 */
const MIN_IMAGE_WIDTH = 80;
/** 图片还没加载完、拿不到 naturalWidth 时的上限（px）。 */
const FALLBACK_MAX_IMAGE_WIDTH = 2400;
/** 宽度按 10px 取整，避免正文里写满零头。 */
const WIDTH_STEP = 10;

/** 选中图片的外框；用类名而不是内联样式，便于重渲染后按序号重新套用。 */
const SELECTED_CLASSES = ["outline", "outline-2", "outline-primary"];

/** 右下角拖拽手柄的类名；`touch-none` 让触屏拖动不再被滚动抢走。 */
const HANDLE_CLASSES =
  "absolute -bottom-1 -right-1 h-3 w-3 cursor-nwse-resize touch-none rounded-sm border border-background bg-primary";

/** 宽度夹取：下限固定，上限不超过原图宽度。 */
function clampWidth(width: number, maxWidth: number): number {
  return Math.min(Math.max(width, MIN_IMAGE_WIDTH), Math.max(MIN_IMAGE_WIDTH, maxWidth));
}

export interface PostPreviewPaneProps {
  /** 服务端预览产出的 HTML（与发布同一条渲染管线）。 */
  html: string;
  /** 松开手柄时把宽度写回 Markdown，由 post-editor 落表单。 */
  onImageWidth: (index: number, width: number) => void;
}

/**
 * 编辑器预览面板（agent.md §8「编辑器预览」「编辑器图片」）。
 *
 * 复用 `PostContent`，因此公式与代码块复制按钮与服务端发布结果一致；
 * 在此之上给图片加「点选成图片框 + 拖右下角改宽度」的交互。
 *
 * 名字带 Pane 是为了和 `lib/api/posts` 的 `PostPreview`（预览接口出参）区分。
 *
 * 为什么大量用 DOM 操作：图片来自 `dangerouslySetInnerHTML`，React 不掌握这些节点，
 * 包裹层与手柄只能在 effect 里按序号挂上去；重渲染后按同一序号重新套用即可。
 */
export function PostPreviewPane({ html, onImageWidth }: PostPreviewPaneProps) {
  const rootRef = useRef<HTMLDivElement>(null);
  const [selectedIndex, setSelectedIndex] = useState<number | null>(null);
  /** 拖拽后的宽度按序号记住：重渲染会清掉内联样式，这里负责重新套用。 */
  const widthsRef = useRef(new Map<number, number>());
  /** 回调存 ref，effect 就不必依赖父组件每次渲染新建的函数。 */
  const onImageWidthRef = useRef(onImageWidth);
  onImageWidthRef.current = onImageWidth;
  /** 进行中的拖拽清理函数，组件卸载或重渲染时兜底移除 window 监听。 */
  const dragCleanupRef = useRef<(() => void) | null>(null);

  useEffect(() => {
    const root = rootRef.current;
    if (!root) {
      return;
    }

    const images = Array.from(root.querySelectorAll<HTMLImageElement>("img"));
    for (const [index, img] of images.entries()) {
      const stored = widthsRef.current.get(index);
      if (stored) {
        img.style.width = `${stored}px`;
      }
      // 图片是 innerHTML 生成的，包一层相对定位容器后才能把手柄钉在右下角；
      // 同一批节点在选中态变化时会再次进入这里，所以要先判断是否已经包过。
      if (img.parentElement?.dataset.imageBox === "1") {
        continue;
      }
      const box = document.createElement("span");
      box.dataset.imageBox = "1";
      // w-fit + mx-auto 保持 `.prose-blog img` 原本居中的观感，手柄贴合图片边缘。
      box.className = "relative mx-auto block w-fit";
      img.parentNode?.insertBefore(box, img);
      box.appendChild(img);
    }

    // 正文变短后旧序号可能已经不存在，直接取消选中。
    if (selectedIndex !== null && selectedIndex >= images.length) {
      setSelectedIndex(null);
      return;
    }

    const selected = selectedIndex === null ? null : (images[selectedIndex] ?? null);
    for (const img of images) {
      for (const name of SELECTED_CLASSES) {
        img.classList.toggle(name, img === selected);
      }
    }

    let handle: HTMLButtonElement | null = null;
    if (selected) {
      handle = document.createElement("button");
      handle.type = "button";
      handle.dataset.imageHandle = "1";
      handle.setAttribute("aria-label", "拖动调整图片宽度");
      handle.className = HANDLE_CLASSES;
      selected.parentElement?.appendChild(handle);
    }

    const onClick = (event: MouseEvent) => {
      const target = event.target;
      if (!(target instanceof HTMLElement)) {
        return;
      }
      if (target instanceof HTMLImageElement) {
        const index = images.indexOf(target);
        setSelectedIndex(index >= 0 ? index : null);
        return;
      }
      // 公式、代码块复制按钮与链接是内容里的交互元素，点它们不算"点空白"。
      if (target.closest("pre, a, button, .math, .katex")) {
        return;
      }
      setSelectedIndex(null);
    };

    const onPointerDown = (event: PointerEvent) => {
      const target = event.target;
      if (!(target instanceof HTMLElement) || target.dataset.imageHandle !== "1") {
        return;
      }
      const image = selected;
      if (!image) {
        return;
      }

      event.preventDefault();
      const startX = event.clientX;
      const startWidth = image.getBoundingClientRect().width;
      const maxWidth = image.naturalWidth || FALLBACK_MAX_IMAGE_WIDTH;

      // 拖动期间只改内联样式：写回 Markdown 会触发 600ms 防抖预览，不能每个 pointermove 都做。
      const onMove = (moveEvent: PointerEvent) => {
        const width = clampWidth(startWidth + (moveEvent.clientX - startX), maxWidth);
        image.style.width = `${width}px`;
      };
      const finish = (upEvent: PointerEvent, commit: boolean) => {
        dragCleanupRef.current = null;
        window.removeEventListener("pointermove", onMove);
        window.removeEventListener("pointerup", onUp);
        window.removeEventListener("pointercancel", onCancel);
        if (!commit) {
          return;
        }
        const clamped = clampWidth(startWidth + (upEvent.clientX - startX), maxWidth);
        const rounded = clampWidth(Math.round(clamped / WIDTH_STEP) * WIDTH_STEP, maxWidth);
        image.style.width = `${rounded}px`;
        const index = images.indexOf(image);
        if (index >= 0) {
          widthsRef.current.set(index, rounded);
          onImageWidthRef.current(index, rounded);
        }
      };
      const onUp = (upEvent: PointerEvent) => finish(upEvent, true);
      const onCancel = (upEvent: PointerEvent) => finish(upEvent, false);

      window.addEventListener("pointermove", onMove);
      window.addEventListener("pointerup", onUp);
      window.addEventListener("pointercancel", onCancel);
      dragCleanupRef.current = () => finish(event, false);
    };

    root.addEventListener("click", onClick);
    root.addEventListener("pointerdown", onPointerDown);

    return () => {
      root.removeEventListener("click", onClick);
      root.removeEventListener("pointerdown", onPointerDown);
      dragCleanupRef.current?.();
      dragCleanupRef.current = null;
      handle?.remove();
      for (const img of images) {
        for (const name of SELECTED_CLASSES) {
          img.classList.remove(name);
        }
      }
    };
  }, [html, selectedIndex]);

  return (
    <div ref={rootRef} className="relative">
      <PostContent html={html} />
    </div>
  );
}
