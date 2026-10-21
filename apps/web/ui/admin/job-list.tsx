"use client";

import { usePathname, useRouter, useSearchParams } from "next/navigation";
import { useEffect, useState } from "react";
import useSWR from "swr";

import { ApiError } from "@/lib/api/fetcher";
import {
  adminJobApi,
  JOB_STATUSES,
  type AdminJob,
  type AdminJobListResult,
  type JobStatus,
  type ManualJobType,
} from "@/lib/api/ops";
import { swrKeys } from "@/lib/swr/keys";
import { formatDateTime } from "@/lib/utils";
import { Alert, AlertDescription } from "@/ui/shadcn/alert";
import { Badge } from "@/ui/shadcn/badge";
import { Button } from "@/ui/shadcn/button";
import { Loading } from "@/ui/shared/loading";
import { Pagination } from "@/ui/shared/pagination";

/** 每页条数；服务端上限 50（agent.md §5）。 */
const PAGE_SIZE = 20;

type JobBadgeVariant = "default" | "muted" | "outline" | "destructive";

/** 状态中文名与徽章样式（agent.md §4 jobs.status）。 */
const STATUS_META: Record<
  JobStatus,
  { label: string; variant: JobBadgeVariant }
> = {
  pending: { label: "待执行", variant: "outline" },
  running: { label: "执行中", variant: "default" },
  done: { label: "已完成", variant: "muted" },
  failed: { label: "失败", variant: "destructive" },
};

/** 状态筛选项；枚举取自 ops.ts 的常量，避免组件里散落魔法字符串。 */
const STATUS_FILTERS: { value: JobStatus | ""; label: string }[] = [
  { value: "", label: "全部" },
  ...JOB_STATUSES.map((status) => ({
    value: status,
    label: STATUS_META[status].label,
  })),
];

/** 任务类型中文名；手动投递之外的类型由调度器投递（agent.md §13）。 */
const TYPE_LABEL: Record<string, string> = {
  cleanup: "清理",
  backup: "备份",
  link_check: "检查外链",
  integrity_check: "校验数据库",
  publish_scheduled: "定时发布",
  media_process: "媒体处理",
  media_archive: "媒体归档",
};

/** 手动投递按钮（D13/D17：只允许 cleanup / backup / link_check / integrity_check）。 */
const MANUAL_ACTIONS: { type: ManualJobType; label: string }[] = [
  { type: "cleanup", label: "立即清理" },
  { type: "backup", label: "立即备份" },
  { type: "link_check", label: "检查外链" },
  { type: "integrity_check", label: "校验数据库" },
];

/** 从查询串读页码；缺省或非法值一律回第 1 页。 */
function readPage(params: { get(name: string): string | null }): number {
  const raw = Number(params.get("page"));
  return Number.isInteger(raw) && raw >= 1 ? raw : 1;
}

/**
 * 任务队列（D13）：状态筛选、手动投递、失败重试。
 *
 * 手动投递只用于运维与自测（`cleanup` / `backup` / `link_check`）；
 * 周期任务由调度器按站点时区投递，重启不会重复跑，所以这里不提供"改周期"的入口。
 */
export function JobList() {
  const searchParams = useSearchParams();
  const router = useRouter();
  const pathname = usePathname();
  const page = readPage(searchParams);

  const [status, setStatus] = useState<JobStatus | "">("");
  const [error, setError] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  const {
    data,
    error: loadError,
    isLoading,
    mutate,
  } = useSWR<AdminJobListResult, ApiError>(
    swrKeys.adminJobs(status, page, PAGE_SIZE),
    () => adminJobApi.list({ status, page, pageSize: PAGE_SIZE }),
  );

  const total = data?.total ?? 0;
  const totalPages = Math.max(1, Math.ceil(total / PAGE_SIZE));

  // 换筛选后当前页可能越界，退到最后一页；页码在查询串里，只能 replace。
  useEffect(() => {
    if (data && page > totalPages) {
      router.replace(`${pathname}?page=${totalPages}`, { scroll: false });
    }
  }, [data, page, totalPages, pathname, router]);

  /** 页码在查询串里，换筛选条件时要清掉 page 才回得到第 1 页。 */
  function resetPage() {
    if (searchParams.has("page")) {
      router.replace(pathname, { scroll: false });
    }
  }

  function changeStatus(next: JobStatus | "") {
    setStatus(next);
    resetPage();
  }

  async function enqueue(type: ManualJobType) {
    setBusy(true);
    setError(null);
    setNotice(null);
    try {
      await adminJobApi.enqueue(type);
      setNotice(`已投递「${TYPE_LABEL[type] ?? type}」任务。`);
      await mutate();
    } catch (caught) {
      setError(caught instanceof ApiError ? caught.message : "投递失败，请稍后重试");
    } finally {
      setBusy(false);
    }
  }

  async function retry(job: AdminJob) {
    setBusy(true);
    setError(null);
    setNotice(null);
    try {
      await adminJobApi.retry(job.id);
      setNotice(`任务 #${job.id} 已重新排队，稍后会立刻到期。`);
      await mutate();
    } catch (caught) {
      setError(caught instanceof ApiError ? caught.message : "重试失败，请稍后重试");
    } finally {
      setBusy(false);
    }
  }

  return (
    <section className="space-y-6">
      <header className="space-y-1">
        <h1 className="text-2xl font-semibold tracking-tight">任务</h1>
        <p className="text-sm text-muted-foreground">
          共 {total} 条；失败任务退避重试到 maxAttempts 后停在 failed，可手动重试。
        </p>
      </header>

      <div className="space-y-2 rounded-lg border border-border p-4">
        <h2 className="text-sm font-semibold">手动投递</h2>
        <p className="text-xs text-muted-foreground">
          只允许 cleanup / backup / link_check / integrity_check，投递后立即入队，不等周期调度。
        </p>
        <div className="flex flex-wrap gap-2">
          {MANUAL_ACTIONS.map((action) => (
            <Button
              key={action.type}
              size="sm"
              variant="outline"
              disabled={busy}
              onClick={() => void enqueue(action.type)}
            >
              {action.label}
            </Button>
          ))}
        </div>
      </div>

      <div className="flex flex-wrap gap-2" role="group" aria-label="按状态筛选">
        {STATUS_FILTERS.map((filter) => (
          <Button
            key={filter.value || "all"}
            size="sm"
            variant={status === filter.value ? "default" : "outline"}
            aria-pressed={status === filter.value}
            onClick={() => changeStatus(filter.value)}
          >
            {filter.label}
          </Button>
        ))}
      </div>

      {error ? (
        <Alert variant="destructive">
          <AlertDescription>{error}</AlertDescription>
        </Alert>
      ) : null}

      {loadError ? (
        <Alert variant="destructive">
          <AlertDescription>{loadError.message}</AlertDescription>
        </Alert>
      ) : null}

      {notice ? (
        <p className="text-sm text-muted-foreground" role="status">
          {notice}
        </p>
      ) : null}

      {isLoading && !data ? <Loading label="正在加载任务…" /> : null}

      {data ? (
        data.list.length === 0 ? (
          <p className="rounded-lg border border-dashed border-border px-4 py-12 text-center text-sm text-muted-foreground">
            {status ? "这个状态下没有任务。" : "任务队列是空的。"}
          </p>
        ) : (
          <div className="overflow-x-auto rounded-lg border border-border">
            <table className="w-full text-sm">
              <caption className="sr-only">任务列表</caption>
              <thead className="bg-muted/60 text-left text-xs text-muted-foreground">
                <tr>
                  <th scope="col" className="px-4 py-3 font-medium">
                    ID
                  </th>
                  <th scope="col" className="px-4 py-3 font-medium">
                    类型
                  </th>
                  <th scope="col" className="px-4 py-3 font-medium">
                    状态
                  </th>
                  <th scope="col" className="px-4 py-3 font-medium">
                    尝试
                  </th>
                  <th scope="col" className="px-4 py-3 font-medium">
                    执行时间
                  </th>
                  <th scope="col" className="px-4 py-3 font-medium">
                    最近错误
                  </th>
                  <th scope="col" className="px-4 py-3 text-right font-medium">
                    操作
                  </th>
                </tr>
              </thead>
              <tbody>
                {data.list.map((job) => {
                  const meta = STATUS_META[job.status];
                  return (
                    <tr key={job.id} className="border-t border-border">
                      <td className="px-4 py-3 text-muted-foreground">
                        {job.id}
                      </td>
                      <td className="px-4 py-3">
                        <p>{TYPE_LABEL[job.type] ?? job.type}</p>
                        <code
                          className="block max-w-[14rem] truncate text-xs text-muted-foreground"
                          title={job.payload}
                        >
                          {job.payload}
                        </code>
                      </td>
                      <td className="px-4 py-3">
                        <Badge variant={meta.variant}>{meta.label}</Badge>
                      </td>
                      <td className="px-4 py-3 text-muted-foreground">
                        {job.attempts} / {job.maxAttempts}
                      </td>
                      <td className="px-4 py-3 text-muted-foreground">
                        {formatDateTime(job.runAt)}
                      </td>
                      <td className="px-4 py-3">
                        {job.lastError ? (
                          <code
                            className="block max-w-[18rem] truncate text-xs text-destructive"
                            title={job.lastError}
                          >
                            {job.lastError}
                          </code>
                        ) : (
                          <span className="text-muted-foreground">—</span>
                        )}
                      </td>
                      <td className="px-4 py-3 text-right">
                        {job.status === "failed" ? (
                          <Button
                            size="sm"
                            variant="outline"
                            disabled={busy}
                            onClick={() => void retry(job)}
                          >
                            重试
                          </Button>
                        ) : (
                          <span className="text-muted-foreground">—</span>
                        )}
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
        )
      ) : null}

      {data ? (
        <Pagination
          page={page}
          pageSize={PAGE_SIZE}
          total={total}
          unit="条"
          hrefFor={(next) => `${pathname}?page=${next}`}
        />
      ) : null}
    </section>
  );
}
