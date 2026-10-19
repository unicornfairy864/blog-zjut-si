"use client";

import { Suspense } from "react";

import { AuditLogList } from "@/ui/admin/audit-log-list";
import { Loading } from "@/ui/shared/loading";

/**
 * 管理端审计日志页（/admin/audit-logs）。
 *
 * 分页号放在查询串里，以复用 `ui/shared/pagination.tsx` 的链接式分页；
 * `useSearchParams` 在静态预渲染时要求有 Suspense 边界，所以这里包一层。
 */
export default function AdminAuditLogsPage() {
  return (
    <Suspense fallback={<Loading label="正在加载审计日志…" />}>
      <AuditLogList />
    </Suspense>
  );
}
