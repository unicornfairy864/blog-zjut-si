import { cn } from "@/lib/utils";

/**
 * 骨架块：等待内容时占位。
 *
 * 用 `bg-muted` + `animate-pulse` 而不是图片或 shim：颜色只走 token（§8），
 * 深浅色主题都不用额外适配。
 */
export function Skeleton({ className }: { className?: string }) {
  return <div className={cn("animate-pulse rounded bg-muted", className)} />;
}
