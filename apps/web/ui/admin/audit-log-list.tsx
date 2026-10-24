"use client";

import { usePathname, useRouter, useSearchParams } from "next/navigation";
import { useEffect, useMemo, useState } from "react";
import useSWR from "swr";

import { ApiError } from "@/lib/api/fetcher";
import {
  adminAuditApi,
  type AdminAuditLogListResult,
} from "@/lib/api/ops";
import { swrKeys } from "@/lib/swr/keys";
import { formatDateTime } from "@/lib/utils";
import { Alert, AlertDescription } from "@/ui/shadcn/alert";
import { Button } from "@/ui/shadcn/button";
import { Input } from "@/ui/shadcn/input";
import { Label } from "@/ui/shadcn/label";
import { Loading } from "@/ui/shared/loading";
import { Pagination } from "@/ui/shared/pagination";

/** 每页条数；服务端上限 50（agent.md §5）。 */
const PAGE_SIZE = 20;

/** ipHash 是访客/来源指纹，界面只露前 8 位（agent.md §9：审计够用即可）。 */
const IP_HASH_PREFIX = 8;

/** 审计筛选；三项都空表示不过滤。 */
interface AuditFilter {
  action: string;
  targetType: string;
  targetId: string;
}

const EMPTY_FILTER: AuditFilter = { action: "", targetType: "", targetId: "" };

/** 从查询串读页码；缺省或非法值一律回第 1 页。 */
function readPage(params: { get(name: string): string | null }): number {
  const raw = Number(params.get("page"));
  return Number.isInteger(raw) && raw >= 1 ? raw : 1;
}

/**
 * 审计日志（D14）：按 action / targetType / targetId 过滤的分页表格。
 *
 * 只读视图：审计记录永久保留，界面不提供任何修改入口（agent.md §9）。
 */
export function AuditLogList() {
  const searchParams = useSearchParams();
  const router = useRouter();
  const pathname = usePathname();
  const page = readPage(searchParams);

  const [draft, setDraft] = useState<AuditFilter>(EMPTY_FILTER);
  const [applied, setApplied] = useState<AuditFilter>(EMPTY_FILTER);

  // 筛选条件拼成查询串片段，既喂 SWR key 也保证"换筛选即换缓存"。
  const filter = useMemo(() => {
    const params = new URLSearchParams();
    if (applied.action) {
      params.set("action", applied.action);
    }
    if (applied.targetType) {
      params.set("targetType", applied.targetType);
    }
    if (applied.targetId) {
      params.set("targetId", applied.targetId);
    }
    return params.toString();
  }, [applied]);

  const {
    data,
    error: loadError,
    isLoading,
  } = useSWR<AdminAuditLogListResult, ApiError>(
    swrKeys.adminAuditLogs(filter, page, PAGE_SIZE),
    () =>
      adminAuditApi.list({
        action: applied.action || undefined,
        targetType: applied.targetType || undefined,
        targetId: applied.targetId || undefined,
        page,
        pageSize: PAGE_SIZE,
      }),
  );

  const total = data?.total ?? 0;
  const totalPages = Math.max(1, Math.ceil(total / PAGE_SIZE));
  const filtered = Boolean(applied.action || applied.targetType || applied.targetId);

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

  function applyFilter() {
    setApplied({
      action: draft.action.trim(),
      targetType: draft.targetType.trim(),
      targetId: draft.targetId.trim(),
    });
    resetPage();
  }

  function clearFilter() {
    setDraft(EMPTY_FILTER);
    setApplied(EMPTY_FILTER);
    resetPage();
  }

  return (
    <section className="space-y-6">
      <header className="space-y-1">
        <h1 className="text-2xl font-semibold tracking-tight">审计日志</h1>
        <p className="text-sm text-muted-foreground">
          共 {total} 条；管理端写操作都会留痕，按创建时间倒序。
        </p>
      </header>

      <form
        className="grid items-end gap-4 sm:grid-cols-2 lg:grid-cols-4"
        onSubmit={(event) => {
          event.preventDefault();
          applyFilter();
        }}
      >
        <div className="space-y-2">
          <Label htmlFor="audit-action">action</Label>
          <Input
            id="audit-action"
            placeholder="post.publish"
            value={draft.action}
            onChange={(event) =>
              setDraft((current) => ({ ...current, action: event.target.value }))
            }
          />
        </div>
        <div className="space-y-2">
          <Label htmlFor="audit-target-type">targetType</Label>
          <Input
            id="audit-target-type"
            placeholder="post"
            value={draft.targetType}
            onChange={(event) =>
              setDraft((current) => ({
                ...current,
                targetType: event.target.value,
              }))
            }
          />
        </div>
        <div className="space-y-2">
          <Label htmlFor="audit-target-id">targetId</Label>
          <Input
            id="audit-target-id"
            placeholder="3"
            value={draft.targetId}
            onChange={(event) =>
              setDraft((current) => ({ ...current, targetId: event.target.value }))
            }
          />
        </div>
        <div className="flex flex-wrap gap-2">
          <Button type="submit" size="sm" variant="outline">
            筛选
          </Button>
          {filtered ? (
            <Button type="button" size="sm" variant="ghost" onClick={clearFilter}>
              重置
            </Button>
          ) : null}
        </div>
      </form>

      {loadError ? (
        <Alert variant="destructive">
          <AlertDescription>{loadError.message}</AlertDescription>
        </Alert>
      ) : null}

      {isLoading && !data ? <Loading label="正在加载审计日志…" /> : null}

      {data ? (
        data.list.length === 0 ? (
          <p className="rounded-lg border border-dashed border-border px-4 py-12 text-center text-sm text-muted-foreground">
            {filtered ? "没有符合筛选条件的记录。" : "还没有审计日志。"}
          </p>
        ) : (
          <div className="overflow-x-auto rounded-lg border border-border">
            <table className="w-full text-sm">
              <caption className="sr-only">审计日志列表</caption>
              <thead className="bg-muted/60 text-left text-xs text-muted-foreground">
                <tr>
                  <th scope="col" className="px-4 py-3 font-medium">
                    时间
                  </th>
                  <th scope="col" className="px-4 py-3 font-medium">
                    action
                  </th>
                  <th scope="col" className="px-4 py-3 font-medium">
                    targetType
                  </th>
                  <th scope="col" className="px-4 py-3 font-medium">
                    targetId
                  </th>
                  <th scope="col" className="px-4 py-3 font-medium">
                    adminId
                  </th>
                  <th scope="col" className="px-4 py-3 font-medium">
                    meta
                  </th>
                  <th scope="col" className="px-4 py-3 font-medium">
                    ipHash
                  </th>
                </tr>
              </thead>
              <tbody>
                {data.list.map((log) => (
                  <tr key={log.id} className="border-t border-border align-top">
                    <td className="px-4 py-3 text-muted-foreground">
                      {formatDateTime(log.createdAt)}
                    </td>
                    <td className="px-4 py-3">
                      <code className="text-xs">{log.action}</code>
                    </td>
                    <td className="px-4 py-3 text-muted-foreground">
                      {log.targetType || "—"}
                    </td>
                    <td className="px-4 py-3 text-muted-foreground">
                      {log.targetId || "—"}
                    </td>
                    <td className="px-4 py-3 text-muted-foreground">
                      {log.adminId}
                    </td>
                    <td className="px-4 py-3">
                      {log.meta && Object.keys(log.meta).length > 0 ? (
                        <details className="text-xs">
                          <summary className="cursor-pointer text-muted-foreground">
                            查看 JSON
                          </summary>
                          <pre className="mt-1 max-w-[24rem] overflow-x-auto rounded-md bg-muted/60 p-2 text-xs">
                            {JSON.stringify(log.meta, null, 2)}
                          </pre>
                        </details>
                      ) : (
                        <span className="text-muted-foreground">—</span>
                      )}
                    </td>
                    <td className="px-4 py-3">
                      <code
                        className="text-xs text-muted-foreground"
                        title="只显示前 8 位"
                      >
                        {log.ipHash ? log.ipHash.slice(0, IP_HASH_PREFIX) : "—"}
                      </code>
                    </td>
                  </tr>
                ))}
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
