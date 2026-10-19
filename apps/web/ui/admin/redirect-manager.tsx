"use client";

import { zodResolver } from "@hookform/resolvers/zod";
import { useState } from "react";
import { useForm } from "react-hook-form";
import useSWR from "swr";

import type { ApiError } from "@/lib/api/fetcher";
import {
  adminTaxonomyApi,
  REDIRECT_STATUSES,
  type AdminRedirect,
  type AdminRedirectListResult,
  type RedirectStatus,
} from "@/lib/api/taxonomy";
import { swrKeys } from "@/lib/swr/keys";
import { formatDateTime } from "@/lib/utils";
import {
  redirectSchema,
  redirectStatusValues,
  type RedirectStatusValue,
  type RedirectValues,
} from "@/lib/zod/taxonomy";
import { FieldSelect } from "@/ui/admin/field-select";
import { Alert, AlertDescription } from "@/ui/shadcn/alert";
import { Badge } from "@/ui/shadcn/badge";
import { Button } from "@/ui/shadcn/button";
import { Input } from "@/ui/shadcn/input";
import { Label } from "@/ui/shadcn/label";
import { Loading } from "@/ui/shared/loading";

/** `REDIRECT_STATUSES` 的顺序是 [永久, 临时]，这里按名字解构以免出现裸数字。 */
const [PERMANENT_REDIRECT, TEMPORARY_REDIRECT] = REDIRECT_STATUSES;

/** 状态中文名。 */
const STATUS_LABELS: Record<RedirectStatusValue, string> = {
  "301": "301 永久跳转",
  "302": "302 临时跳转",
};

/** 原生 select 只给字符串，提交前转回契约里的数字码。 */
function toRedirectStatus(value: RedirectStatusValue): RedirectStatus {
  return String(TEMPORARY_REDIRECT) === value
    ? TEMPORARY_REDIRECT
    : PERMANENT_REDIRECT;
}

/** 反向转换：把接口返回的数字码放回表单字段。 */
function toStatusValue(status: RedirectStatus): RedirectStatusValue {
  return status === TEMPORARY_REDIRECT
    ? redirectStatusValues[1]
    : redirectStatusValues[0];
}

/** 空表单；新建与「取消编辑」都回到这一份默认值。 */
const EMPTY_VALUES: RedirectValues = {
  fromPath: "",
  toPath: "",
  status: redirectStatusValues[0],
};

/**
 * 跳转管理（D14）：全量列表 + 新建 / 编辑 + 删除。
 *
 * 命中发生在公开边缘：按 `from_path` 精确匹配后直接 301/302 并 `hits+1`，
 * 不经过 Next（api.md「跳转与附件」），所以改 slug 后的老链接不会 404。
 */
export function RedirectManager() {
  const [editingId, setEditingId] = useState<number | null>(null);
  const [error, setError] = useState<string | null>(null);

  const { data, error: loadError, isLoading, mutate } = useSWR<
    AdminRedirectListResult,
    ApiError
  >(swrKeys.adminRedirects, () => adminTaxonomyApi.redirects());

  const {
    register,
    handleSubmit,
    reset,
    formState: { errors, isSubmitting },
  } = useForm<RedirectValues>({
    resolver: zodResolver(redirectSchema),
    defaultValues: EMPTY_VALUES,
  });

  const items = data?.list ?? [];

  function startEdit(item: AdminRedirect) {
    setEditingId(item.id);
    setError(null);
    reset({
      fromPath: item.fromPath,
      toPath: item.toPath,
      status: toStatusValue(item.status),
    });
  }

  function cancelEdit() {
    setEditingId(null);
    setError(null);
    reset(EMPTY_VALUES);
  }

  async function onSubmit(values: RedirectValues) {
    setError(null);
    const input = {
      fromPath: values.fromPath,
      toPath: values.toPath,
      status: toRedirectStatus(values.status),
    };
    try {
      if (editingId === null) {
        await adminTaxonomyApi.createRedirect(input);
      } else {
        await adminTaxonomyApi.updateRedirect(editingId, input);
      }
      cancelEdit();
      await mutate();
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : "保存失败，请稍后重试");
    }
  }

  async function handleDelete(item: AdminRedirect) {
    if (!window.confirm(`确定删除跳转「${item.fromPath}」？`)) {
      return;
    }
    setError(null);
    try {
      await adminTaxonomyApi.removeRedirect(item.id);
      if (editingId === item.id) {
        cancelEdit();
      }
      await mutate();
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : "删除失败，请稍后重试");
    }
  }

  return (
    <section className="space-y-6">
      <header className="space-y-2">
        <h1 className="text-xl font-semibold">跳转</h1>
        <p className="text-sm text-muted-foreground">
          公开边缘按原路径精确匹配后直接 301/302 并累加命中数，不经过 Next；
          改过 slug 的老链接靠这里兜住。
        </p>
      </header>

      <form
        onSubmit={handleSubmit(onSubmit)}
        className="space-y-4 rounded-lg border border-border p-4"
      >
        <div className="flex flex-wrap items-center justify-between gap-2">
          <h2 className="text-base font-semibold">
            {editingId === null ? "新建跳转" : "编辑跳转"}
          </h2>
          {editingId === null ? null : (
            <Button type="button" size="sm" variant="ghost" onClick={cancelEdit}>
              取消编辑
            </Button>
          )}
        </div>

        <div className="grid gap-4 sm:grid-cols-2">
          <div className="space-y-2">
            <Label htmlFor="redirect-from">原路径</Label>
            <Input
              id="redirect-from"
              placeholder="/posts/old-slug"
              aria-invalid={errors.fromPath ? true : undefined}
              aria-describedby={errors.fromPath ? "redirect-from-error" : undefined}
              {...register("fromPath")}
            />
            {errors.fromPath ? (
              <p id="redirect-from-error" className="text-xs text-destructive">
                {errors.fromPath.message}
              </p>
            ) : null}
          </div>

          <div className="space-y-2">
            <Label htmlFor="redirect-to">目标路径</Label>
            <Input
              id="redirect-to"
              placeholder="/posts/new-slug"
              aria-invalid={errors.toPath ? true : undefined}
              aria-describedby={errors.toPath ? "redirect-to-error" : undefined}
              {...register("toPath")}
            />
            {errors.toPath ? (
              <p id="redirect-to-error" className="text-xs text-destructive">
                {errors.toPath.message}
              </p>
            ) : null}
          </div>

          <div className="space-y-2">
            <Label htmlFor="redirect-status">状态码</Label>
            <FieldSelect id="redirect-status" {...register("status")}>
              {redirectStatusValues.map((status) => (
                <option key={status} value={status}>
                  {STATUS_LABELS[status]}
                </option>
              ))}
            </FieldSelect>
          </div>
        </div>

        <Button type="submit" disabled={isSubmitting}>
          {editingId === null ? "新建" : "保存"}
        </Button>
      </form>

      {error ? (
        <Alert variant="destructive">
          <AlertDescription>{error}</AlertDescription>
        </Alert>
      ) : null}

      {isLoading && !data ? <Loading /> : null}

      {loadError ? (
        <Alert variant="destructive">
          <AlertDescription>{loadError.message}</AlertDescription>
        </Alert>
      ) : null}

      {data ? (
        items.length === 0 ? (
          <p className="rounded-lg border border-dashed border-border px-4 py-12 text-center text-sm text-muted-foreground">
            还没有跳转规则。
          </p>
        ) : (
          <ul className="space-y-3">
            {items.map((item) => (
              <li
                key={item.id}
                className="flex flex-wrap items-start justify-between gap-3 rounded-lg border border-border p-4"
              >
                <div className="min-w-0 space-y-1">
                  <p className="flex flex-wrap items-center gap-x-3 gap-y-1 text-sm">
                    <code className="rounded bg-muted px-1.5 py-0.5 text-xs">
                      {item.fromPath}
                    </code>
                    <span aria-hidden="true" className="text-muted-foreground">
                      →
                    </span>
                    <code className="rounded bg-muted px-1.5 py-0.5 text-xs">
                      {item.toPath}
                    </code>
                  </p>
                  <p className="flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-muted-foreground">
                    <Badge variant="outline">
                      {STATUS_LABELS[toStatusValue(item.status)]}
                    </Badge>
                    <span className="tabular-nums">命中 {item.hits} 次</span>
                    <time>{formatDateTime(item.createdAt)}</time>
                  </p>
                </div>

                <div className="flex gap-2">
                  <Button
                    type="button"
                    size="sm"
                    variant="outline"
                    onClick={() => startEdit(item)}
                  >
                    编辑
                  </Button>
                  <Button
                    type="button"
                    size="sm"
                    variant="destructive"
                    onClick={() => void handleDelete(item)}
                  >
                    删除
                  </Button>
                </div>
              </li>
            ))}
          </ul>
        )
      ) : null}
    </section>
  );
}
