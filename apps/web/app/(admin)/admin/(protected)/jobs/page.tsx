"use client";

import { Suspense } from "react";

import { JobList } from "@/ui/admin/job-list";
import { Loading } from "@/ui/shared/loading";

/**
 * 管理端任务页（/admin/jobs）。
 *
 * 分页号放在查询串里，以复用 `ui/shared/pagination.tsx` 的链接式分页；
 * `useSearchParams` 在静态预渲染时要求有 Suspense 边界，所以这里包一层。
 */
export default function AdminJobsPage() {
  return (
    <Suspense fallback={<Loading label="正在加载任务…" />}>
      <JobList />
    </Suspense>
  );
}
