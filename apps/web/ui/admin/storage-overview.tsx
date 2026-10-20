"use client";

import { useState } from "react";
import useSWR from "swr";

import { ApiError } from "@/lib/api/fetcher";
import {
  adminStorageApi,
  describeStorageKind,
  type StorageMissing,
  type StorageOverview,
} from "@/lib/api/storage";
import { swrKeys } from "@/lib/swr/keys";
import { formatBytes } from "@/lib/utils";
import { Alert, AlertDescription } from "@/ui/shadcn/alert";
import { Button } from "@/ui/shadcn/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/ui/shadcn/card";
import { Loading } from "@/ui/shared/loading";

/** 一张统计卡片：主数值 + 一句解释。 */
interface StatCardProps {
  label: string;
  value: string;
  detail: string;
}

/** 统计卡片；数值用等宽数字，多张卡片并排时小数点能对齐。 */
function StatCard({ label, value, detail }: StatCardProps) {
  return (
    <Card>
      <CardHeader className="p-4 pb-1">
        <CardTitle className="text-sm font-medium text-muted-foreground">
          {label}
        </CardTitle>
      </CardHeader>
      <CardContent className="p-4 pt-0">
        <p className="text-2xl font-semibold tabular-nums">{value}</p>
        <p className="text-xs text-muted-foreground">{detail}</p>
      </CardContent>
    </Card>
  );
}

/** 把缺失文件的归属写成"媒体 #3"这种形式；两边都没有时给破折号。 */
function describeMissingOwner(item: StorageMissing): string {
  if (item.mediaId) {
    return `媒体 #${item.mediaId}`;
  }
  if (item.attachmentId) {
    return `附件 #${item.attachmentId}`;
  }
  return "—";
}

/**
 * 存储管理（/admin/storage）：占用统计 + 孤儿文件清理 + 缺失文件对账。
 *
 * 孤儿清理是不可逆的写操作，必须二次确认；缺失文件只做展示——文件不在磁盘上，
 * 删记录还是补文件需要人工判断，前端不提供一键处理。
 */
export function StorageManager() {
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);

  const {
    data,
    error: loadError,
    isLoading,
    mutate,
  } = useSWR<StorageOverview, ApiError>(swrKeys.adminStorage, () =>
    adminStorageApi.overview(),
  );

  async function handleCleanup() {
    if (!data) {
      return;
    }
    const confirmed = window.confirm(
      `确定清理 ${data.orphanCount} 个孤儿文件（${formatBytes(data.orphanBytes)}）？\n这些文件没有任何记录引用，删掉之后无法恢复。`,
    );
    if (!confirmed) {
      return;
    }

    setBusy(true);
    setError(null);
    setNotice(null);
    try {
      const result = await adminStorageApi.cleanup();
      const freed = `已清理 ${result.removedFiles} 个文件，释放 ${formatBytes(result.removedBytes)}`;
      setNotice(
        result.failedFiles > 0
          ? `${freed}；另有 ${result.failedFiles} 个删除失败，请检查目录权限。`
          : `${freed}。`,
      );
      await mutate();
    } catch (caught) {
      setError(caught instanceof ApiError ? caught.message : "清理失败，请稍后重试");
    } finally {
      setBusy(false);
    }
  }

  const orphans = data?.orphans ?? [];
  const missing = data?.missing ?? [];

  return (
    <section className="space-y-8">
      <header className="space-y-1">
        <h1 className="text-2xl font-semibold tracking-tight">存储</h1>
        <p className="text-sm text-muted-foreground">
          图片与附件都由本站托管；这里对账磁盘与数据库，并清理没有任何记录引用的孤儿文件。
        </p>
        {data ? (
          <p className="break-all font-mono text-xs text-muted-foreground">
            图片目录：{data.uploadDir} · 附件目录：{data.fileDir}
          </p>
        ) : null}
      </header>

      {loadError ? (
        <Alert variant="destructive">
          <AlertDescription>{loadError.message}</AlertDescription>
        </Alert>
      ) : null}

      {error ? (
        <Alert variant="destructive">
          <AlertDescription>{error}</AlertDescription>
        </Alert>
      ) : null}

      {notice ? (
        <Alert>
          <AlertDescription>{notice}</AlertDescription>
        </Alert>
      ) : null}

      {isLoading && !data ? <Loading label="正在统计存储占用…" /> : null}

      {data ? (
        <>
          <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
            <StatCard
              label="原图"
              value={formatBytes(data.originalBytes)}
              detail={`${data.mediaCount} 张图片`}
            />
            <StatCard
              label="四档变体"
              value={formatBytes(data.variantBytes)}
              detail="每张图最多 4 档 WebP"
            />
            <StatCard
              label="附件"
              value={formatBytes(data.attachmentBytes)}
              detail={`本地 ${data.linkedAttachmentCount} 个 · 共 ${data.attachmentCount} 个`}
            />
            <StatCard
              label="合计"
              value={formatBytes(data.totalBytes)}
              detail="原图 + 变体 + 附件，不含孤儿"
            />
            <StatCard
              label="孤儿文件"
              value={String(data.orphanCount)}
              detail={`${formatBytes(data.orphanBytes)} 可清理`}
            />
            <StatCard
              label="缺失文件"
              value={String(data.missingCount)}
              detail="库里有记录、磁盘上没有"
            />
            <StatCard
              label="已删原图"
              value={String(data.originalsRemoved)}
              detail="只保留四档变体"
            />
          </div>

          <section className="space-y-3">
            <div className="flex flex-wrap items-end justify-between gap-3">
              <div className="space-y-1">
                <h2 className="text-base font-semibold">孤儿文件</h2>
                <p className="text-sm text-muted-foreground">
                  磁盘上有、库里没有任何记录引用的文件；清理后不可恢复。
                </p>
              </div>
              <Button
                type="button"
                variant="destructive"
                onClick={() => void handleCleanup()}
                disabled={busy || data.orphanCount === 0}
              >
                {busy ? "清理中…" : "清理孤儿文件"}
              </Button>
            </div>

            {orphans.length === 0 ? (
              <p className="rounded-lg border border-dashed border-border px-4 py-8 text-center text-sm text-muted-foreground">
                没有孤儿文件，磁盘与数据库是对得上的。
              </p>
            ) : (
              <div className="space-y-2">
                <div className="overflow-x-auto rounded-lg border border-border">
                  <table className="w-full text-sm">
                    <caption className="sr-only">孤儿文件列表</caption>
                    <thead className="bg-muted/60 text-left text-xs text-muted-foreground">
                      <tr>
                        <th scope="col" className="px-4 py-3 font-medium">
                          路径
                        </th>
                        <th scope="col" className="px-4 py-3 font-medium">
                          大小
                        </th>
                        <th scope="col" className="px-4 py-3 font-medium">
                          类型
                        </th>
                      </tr>
                    </thead>
                    <tbody>
                      {orphans.map((orphan) => (
                        <tr key={orphan.path} className="border-t border-border">
                          <td className="px-4 py-2">
                            <code className="break-all text-xs">{orphan.path}</code>
                          </td>
                          <td className="px-4 py-2 text-muted-foreground">
                            {formatBytes(orphan.bytes)}
                          </td>
                          <td className="px-4 py-2 text-muted-foreground">
                            {describeStorageKind(orphan.kind)}
                          </td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
                <p className="text-xs text-muted-foreground">
                  {`最多列出 200 条，当前显示 ${orphans.length} / ${data.orphanCount} 条；清理时按全量处理。`}
                </p>
              </div>
            )}
          </section>

          <section className="space-y-3">
            <div className="space-y-1">
              <h2 className="text-base font-semibold">缺失文件</h2>
              <p className="text-sm text-muted-foreground">
                库里有记录、磁盘上没有对应文件；需要人工判断是补文件还是删记录，这里只做展示。
              </p>
            </div>

            {missing.length === 0 ? (
              <p className="rounded-lg border border-dashed border-border px-4 py-8 text-center text-sm text-muted-foreground">
                没有缺失文件，记录都还能找到对应的磁盘文件。
              </p>
            ) : (
              <div className="space-y-2">
                <div className="overflow-x-auto rounded-lg border border-border">
                  <table className="w-full text-sm">
                    <caption className="sr-only">缺失文件列表</caption>
                    <thead className="bg-muted/60 text-left text-xs text-muted-foreground">
                      <tr>
                        <th scope="col" className="px-4 py-3 font-medium">
                          类型
                        </th>
                        <th scope="col" className="px-4 py-3 font-medium">
                          storageKey
                        </th>
                        <th scope="col" className="px-4 py-3 font-medium">
                          归属
                        </th>
                      </tr>
                    </thead>
                    <tbody>
                      {missing.map((item) => (
                        <tr
                          key={`${item.kind}-${item.storageKey}`}
                          className="border-t border-border"
                        >
                          <td className="px-4 py-2 text-muted-foreground">
                            {describeStorageKind(item.kind)}
                          </td>
                          <td className="px-4 py-2">
                            <code className="break-all text-xs">
                              {item.storageKey || "—"}
                            </code>
                          </td>
                          <td className="px-4 py-2 text-muted-foreground">
                            {describeMissingOwner(item)}
                          </td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </div>
                <p className="text-xs text-muted-foreground">
                  {`最多列出 100 条，当前显示 ${missing.length} / ${data.missingCount} 条。`}
                </p>
              </div>
            )}
          </section>
        </>
      ) : null}
    </section>
  );
}
