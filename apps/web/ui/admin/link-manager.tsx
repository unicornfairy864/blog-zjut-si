"use client";

import { zodResolver } from "@hookform/resolvers/zod";
import { useState } from "react";
import { useForm } from "react-hook-form";
import useSWR from "swr";

import { describeApiError, type ApiError } from "@/lib/api/fetcher";
import {
  adminTaxonomyApi,
  LINK_STATUSES,
  type AdminLink,
  type AdminLinkListResult,
  type LinkStatus,
} from "@/lib/api/taxonomy";
import { swrKeys } from "@/lib/swr/keys";
import { formatDateTime } from "@/lib/utils";
import { linkSchema, type LinkValues } from "@/lib/zod/taxonomy";
import { FieldSelect } from "@/ui/admin/field-select";
import { Alert, AlertDescription } from "@/ui/shadcn/alert";
import { Badge } from "@/ui/shadcn/badge";
import { Button } from "@/ui/shadcn/button";
import { Input } from "@/ui/shadcn/input";
import { Label } from "@/ui/shadcn/label";
import { Textarea } from "@/ui/shadcn/textarea";
import { Loading } from "@/ui/shared/loading";

/** 状态中文名；公开端只出 active。 */
const STATUS_LABELS: Record<LinkStatus, string> = {
  active: "公开",
  hidden: "隐藏",
};

/** 空表单；新建与「取消编辑」都回到这一份默认值。 */
const EMPTY_VALUES: LinkValues = {
  name: "",
  url: "",
  avatar: "",
  description: "",
  sort: "0",
  status: LINK_STATUSES[0],
};

/**
 * 友链管理（D14）：全量列表 + 新建 / 编辑 + 删除。
 *
 * 管理端列出全部记录（含 hidden），公开页只渲染 active，所以界面上要写清这一点，
 * 否则管理员会以为隐藏的友链"没生效"。
 */
export function LinkManager() {
  const [editingId, setEditingId] = useState<number | null>(null);
  const [error, setError] = useState<string | null>(null);

  const { data, error: loadError, isLoading, mutate } = useSWR<
    AdminLinkListResult,
    ApiError
  >(swrKeys.adminLinks, () => adminTaxonomyApi.links());

  const {
    register,
    handleSubmit,
    reset,
    formState: { errors, isSubmitting },
  } = useForm<LinkValues>({
    resolver: zodResolver(linkSchema),
    defaultValues: EMPTY_VALUES,
  });

  const items = data?.list ?? [];

  function startEdit(item: AdminLink) {
    setEditingId(item.id);
    setError(null);
    reset({
      name: item.name,
      url: item.url,
      avatar: item.avatar,
      description: item.description,
      sort: String(item.sort),
      status: item.status,
    });
  }

  function cancelEdit() {
    setEditingId(null);
    setError(null);
    reset(EMPTY_VALUES);
  }

  async function onSubmit(values: LinkValues) {
    setError(null);
    // 表单里的 sort 是字符串（原生 input），提交前转成契约里的整数。
    const input = {
      name: values.name,
      url: values.url,
      avatar: values.avatar,
      description: values.description,
      sort: values.sort === "" ? 0 : Number(values.sort),
      status: values.status,
    };
    try {
      if (editingId === null) {
        await adminTaxonomyApi.createLink(input);
      } else {
        await adminTaxonomyApi.updateLink(editingId, input);
      }
      cancelEdit();
      await mutate();
    } catch (caught) {
      setError(describeApiError(caught, "保存失败，请稍后重试"));
    }
  }

  async function handleDelete(item: AdminLink) {
    if (!window.confirm(`确定删除友链「${item.name}」？`)) {
      return;
    }
    setError(null);
    try {
      await adminTaxonomyApi.removeLink(item.id);
      if (editingId === item.id) {
        cancelEdit();
      }
      await mutate();
    } catch (caught) {
      setError(describeApiError(caught, "删除失败，请稍后重试"));
    }
  }

  return (
    <section className="space-y-6">
      <header className="space-y-2">
        <h1 className="text-xl font-semibold">友链</h1>
        <p className="text-sm text-muted-foreground">
          这里能看到全部友链；公开页面只显示「公开」的条目，「隐藏」仅用于临时下架。
        </p>
      </header>

      <form
        onSubmit={handleSubmit(onSubmit)}
        className="space-y-4 rounded-lg border border-border p-4"
      >
        <div className="flex flex-wrap items-center justify-between gap-2">
          <h2 className="text-base font-semibold">
            {editingId === null ? "新建友链" : "编辑友链"}
          </h2>
          {editingId === null ? null : (
            <Button type="button" size="sm" variant="ghost" onClick={cancelEdit}>
              取消编辑
            </Button>
          )}
        </div>

        <div className="grid gap-4 sm:grid-cols-2">
          <div className="space-y-2">
            <Label htmlFor="link-name">名称</Label>
            <Input
              id="link-name"
              aria-invalid={errors.name ? true : undefined}
              aria-describedby={errors.name ? "link-name-error" : undefined}
              {...register("name")}
            />
            {errors.name ? (
              <p id="link-name-error" className="text-xs text-destructive">
                {errors.name.message}
              </p>
            ) : null}
          </div>

          <div className="space-y-2">
            <Label htmlFor="link-url">链接</Label>
            <Input
              id="link-url"
              placeholder="https://example.com"
              aria-invalid={errors.url ? true : undefined}
              aria-describedby={errors.url ? "link-url-error" : undefined}
              {...register("url")}
            />
            {errors.url ? (
              <p id="link-url-error" className="text-xs text-destructive">
                {errors.url.message}
              </p>
            ) : null}
          </div>

          <div className="space-y-2">
            <Label htmlFor="link-avatar">头像</Label>
            <Input
              id="link-avatar"
              placeholder="https://example.com/avatar.png"
              aria-invalid={errors.avatar ? true : undefined}
              aria-describedby={errors.avatar ? "link-avatar-error" : undefined}
              {...register("avatar")}
            />
            {errors.avatar ? (
              <p id="link-avatar-error" className="text-xs text-destructive">
                {errors.avatar.message}
              </p>
            ) : null}
          </div>

          <div className="space-y-2">
            <Label htmlFor="link-sort">排序</Label>
            <Input
              id="link-sort"
              type="number"
              min={0}
              step={1}
              aria-invalid={errors.sort ? true : undefined}
              aria-describedby={errors.sort ? "link-sort-error" : undefined}
              {...register("sort")}
            />
            {errors.sort ? (
              <p id="link-sort-error" className="text-xs text-destructive">
                {errors.sort.message}
              </p>
            ) : (
              <p className="text-xs text-muted-foreground">
                数字越小越靠前；相同则按创建顺序。
              </p>
            )}
          </div>

          <div className="space-y-2">
            <Label htmlFor="link-status">状态</Label>
            <FieldSelect id="link-status" {...register("status")}>
              {LINK_STATUSES.map((status) => (
                <option key={status} value={status}>
                  {STATUS_LABELS[status]}
                </option>
              ))}
            </FieldSelect>
          </div>

          <div className="space-y-2">
            <Label htmlFor="link-description">描述</Label>
            <Textarea
              id="link-description"
              rows={2}
              {...register("description")}
            />
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
            还没有友链。
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
                    <span className="font-medium">{item.name}</span>
                    <Badge variant={item.status === "active" ? "default" : "outline"}>
                      {STATUS_LABELS[item.status]}
                    </Badge>
                    <span className="text-xs text-muted-foreground tabular-nums">
                      排序 {item.sort}
                    </span>
                    <time className="text-xs text-muted-foreground">
                      {formatDateTime(item.createdAt)}
                    </time>
                  </p>
                  <p className="text-xs text-muted-foreground">
                    <a
                      href={item.url}
                      target="_blank"
                      rel="noopener noreferrer"
                      className="underline-offset-4 hover:underline"
                    >
                      {item.url}
                    </a>
                  </p>
                  {item.description ? (
                    <p className="text-xs text-muted-foreground">
                      {item.description}
                    </p>
                  ) : null}
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
