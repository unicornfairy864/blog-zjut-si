import { Loader2 } from "lucide-react";

/** 整页加载态；管理端在会话确认前使用，避免内容闪烁。 */
export function Loading({ label = "加载中…" }: { label?: string }) {
  return (
    <div
      className="flex min-h-[40vh] items-center justify-center gap-2 text-sm text-muted-foreground"
      role="status"
      aria-live="polite"
    >
      <Loader2 className="h-4 w-4 animate-spin" aria-hidden="true" />
      <span>{label}</span>
    </div>
  );
}
