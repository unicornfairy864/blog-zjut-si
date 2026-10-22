import { cn } from "@/lib/utils";

/**
 * 内容分割线（agent.md §8）。
 *
 * 公开页统一在**标题区下方**（标题 + 说明/元信息）与**正文区下方**各放一条，
 * 让每个页面的"头 / 身 / 尾"三段有一样的视觉节奏。
 *
 * 用 `<hr>` 而不是给容器加 border：分割线本身是内容的一部分，会参与父容器的
 * `space-y-*` 间距，各页面之间的疏密才一致；`border-b` 则会被 padding 影响。
 */
export function Divider({ className }: { className?: string }) {
  return <hr className={cn("border-border", className)} />;
}
