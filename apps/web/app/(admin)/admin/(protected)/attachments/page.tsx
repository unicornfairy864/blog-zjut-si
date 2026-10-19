"use client";

import { Suspense } from "react";

import { AttachmentManager } from "@/ui/admin/attachment-manager";
import { Loading } from "@/ui/shared/loading";

/**
 * 管理端附件页（/admin/attachments）。
 *
 * 分页号放在查询串里，以复用 `ui/shared/pagination.tsx` 的链接式分页；
 * `useSearchParams` 在静态预渲染时要求有 Suspense 边界，所以这里包一层。
 */
export default function AdminAttachmentsPage() {
  return (
    <Suspense fallback={<Loading label="正在加载附件…" />}>
      <AttachmentManager />
    </Suspense>
  );
}
