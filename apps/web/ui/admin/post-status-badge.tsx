import type { PostStatus } from "@/lib/api/posts";
import { Badge } from "@/ui/shadcn/badge";

type BadgeVariant = "default" | "muted" | "outline" | "destructive";

const STATUS_META: Record<string, { label: string; variant: BadgeVariant }> = {
  draft: { label: "草稿", variant: "muted" },
  scheduled: { label: "定时", variant: "outline" },
  published: { label: "已发布", variant: "default" },
  archived: { label: "已归档", variant: "outline" },
};

/** 文章状态徽标；未知状态原样显示，避免静默误导。 */
export function PostStatusBadge({ status }: { status: PostStatus | string }) {
  const meta = STATUS_META[status] ?? {
    label: status,
    variant: "outline" as BadgeVariant,
  };
  return <Badge variant={meta.variant}>{meta.label}</Badge>;
}
