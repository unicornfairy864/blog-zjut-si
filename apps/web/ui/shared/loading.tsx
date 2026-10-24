import { Loader2 } from "lucide-react";

import { cn } from "@/lib/utils";

/** 纯转圈图标：给"已有自己的文案与配色"的位置用（按钮里、行内提示里）。 */
export function Spinner({ className }: { className?: string }) {
  return <Loader2 className={cn("animate-spin", className)} aria-hidden="true" />;
}

export interface LoadingProps {
  label?: string;
  /** 紧凑模式：贴在按钮旁、表单里这类行内位置用，不占 40vh 高度。 */
  compact?: boolean;
  className?: string;
}

/**
 * 等待动画：转圈 + 文案。
 *
 * 管理端在会话确认、SWR 取数、Suspense 兜底处使用；紧凑模式用于行内提示。
 * `role="status"` + `aria-live="polite"` 让读屏也能知道"正在加载"，而不是只看到转圈。
 */
export function Loading({ label = "加载中…", compact = false, className }: LoadingProps) {
  const content = (
    <>
      <Spinner className={compact ? "h-3.5 w-3.5" : "h-4 w-4"} />
      <span>{label}</span>
    </>
  );

  // 紧凑模式渲染成 span：它常被放进 <p>/<span> 里，用 div 会破坏行内容器。
  if (compact) {
    return (
      <span
        role="status"
        aria-live="polite"
        className={cn(
          "inline-flex items-center gap-1.5 text-xs text-muted-foreground",
          className,
        )}
      >
        {content}
      </span>
    );
  }

  return (
    <div
      role="status"
      aria-live="polite"
      className={cn(
        "flex min-h-[40vh] items-center justify-center gap-2 text-sm text-muted-foreground",
        className,
      )}
    >
      {content}
    </div>
  );
}
