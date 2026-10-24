"use client";

import { zodResolver } from "@hookform/resolvers/zod";
import { useState } from "react";
import { useForm } from "react-hook-form";
import useSWR from "swr";

import { describeApiError, type ApiError } from "@/lib/api/fetcher";
import {
  adminTaxonomyApi,
  type AdminSeries,
  type AdminSeriesListResult,
  type AdminTag,
  type AdminTagListResult,
  type DeletedResult,
  type TaxonomyInput,
  type TaxonomyUpdateInput,
} from "@/lib/api/taxonomy";
import { swrKeys } from "@/lib/swr/keys";
import { formatDateTime } from "@/lib/utils";
import { taxonomySchema, type TaxonomyValues } from "@/lib/zod/taxonomy";
import { Alert, AlertDescription } from "@/ui/shadcn/alert";
import { Button } from "@/ui/shadcn/button";
import { Input } from "@/ui/shadcn/input";
import { Label } from "@/ui/shadcn/label";
import { Textarea } from "@/ui/shadcn/textarea";
import { Loading } from "@/ui/shared/loading";

/** 组件支持的分类类型；标签与系列字段同形，共用这一个组件。 */
export type TaxonomyKind = "tags" | "series";

/** 分类行的联合类型；两个接口的字段完全一致，展示与编辑逻辑无需分支。 */
type TaxonomyItem = AdminTag | AdminSeries;

interface TaxonomyManagerProps {
  kind: TaxonomyKind;
}

/** 按 kind 注入的差异点：文案、SWR key 与四个接口。 */
interface TaxonomyKindConfig {
  key: string;
  label: string;
  emptyHint: string;
  list: () => Promise<AdminTagListResult | AdminSeriesListResult>;
  create: (input: TaxonomyInput) => Promise<TaxonomyItem>;
  update: (id: number, input: TaxonomyUpdateInput) => Promise<TaxonomyItem>;
  remove: (id: number) => Promise<DeletedResult>;
}

const KIND_CONFIG: Record<TaxonomyKind, TaxonomyKindConfig> = {
  tags: {
    key: swrKeys.adminTags,
    label: "标签",
    emptyHint: "还没有标签。文章可以没有标签，也可以有多个。",
    list: () => adminTaxonomyApi.tags(),
    create: (input) => adminTaxonomyApi.createTag(input),
    update: (id, input) => adminTaxonomyApi.updateTag(id, input),
    remove: (id) => adminTaxonomyApi.removeTag(id),
  },
  series: {
    key: swrKeys.adminSeries,
    label: "系列",
    emptyHint: "还没有系列。一篇文章最多属于一个系列。",
    list: () => adminTaxonomyApi.series(),
    create: (input) => adminTaxonomyApi.createSeries(input),
    update: (id, input) => adminTaxonomyApi.updateSeries(id, input),
    remove: (id) => adminTaxonomyApi.removeSeries(id),
  },
};

/** 空表单；新建与「取消编辑」都回到这一份默认值。 */
const EMPTY_VALUES: TaxonomyValues = { name: "", slug: "", description: "" };

/**
 * 标签 / 系列管理（D14）：全量列表 + 行内新建 / 编辑 + 删除。
 *
 * 删除只解除分类与文章的关联，不会删文章（api.md「分类与友链（管理端）」），
 * 所以确认文案里要写清楚，避免管理员以为会连带删文章而不敢动手。
 */
export function TaxonomyManager({ kind }: TaxonomyManagerProps) {
  const config = KIND_CONFIG[kind];
  const [editingId, setEditingId] = useState<number | null>(null);
  const [error, setError] = useState<string | null>(null);

  const { data, error: loadError, isLoading, mutate } = useSWR<
    AdminTagListResult | AdminSeriesListResult,
    ApiError
  >(config.key, () => config.list());

  const {
    register,
    handleSubmit,
    reset,
    formState: { errors, isSubmitting },
  } = useForm<TaxonomyValues>({
    resolver: zodResolver(taxonomySchema),
    defaultValues: EMPTY_VALUES,
  });

  const items = data?.list ?? [];

  function startEdit(item: TaxonomyItem) {
    setEditingId(item.id);
    setError(null);
    reset({ name: item.name, slug: item.slug, description: item.description });
  }

  function cancelEdit() {
    setEditingId(null);
    setError(null);
    reset(EMPTY_VALUES);
  }

  async function onSubmit(values: TaxonomyValues) {
    setError(null);
    try {
      if (editingId === null) {
        await config.create(values);
      } else {
        await config.update(editingId, values);
      }
      // 保存成功后回到「新建」态，避免误以为还在编辑同一行。
      cancelEdit();
      await mutate();
    } catch (caught) {
      setError(describeApiError(caught, "保存失败，请稍后重试"));
    }
  }

  async function handleDelete(item: TaxonomyItem) {
    const confirmed = window.confirm(
      `确定删除${config.label}「${item.name}」？不会删除文章，只会解除它与文章的关联。`,
    );
    if (!confirmed) {
      return;
    }
    setError(null);
    try {
      await config.remove(item.id);
      if (editingId === item.id) {
        cancelEdit();
      }
      await mutate();
    } catch (caught) {
      setError(describeApiError(caught, "删除失败，请稍后重试"));
    }
  }

  const nameErrorId = `${kind}-name-error`;
  const slugErrorId = `${kind}-slug-error`;

  return (
    <section className="space-y-6">
      <header className="space-y-2">
        <h1 className="text-xl font-semibold">{config.label}</h1>
        <p className="text-sm text-muted-foreground">
          「文章数」统计的是未删除文章（含草稿）；删除{config.label}
          只解除关联，不会删除文章。
        </p>
      </header>

      <form
        onSubmit={handleSubmit(onSubmit)}
        className="space-y-4 rounded-lg border border-border p-4"
      >
        <div className="flex flex-wrap items-center justify-between gap-2">
          <h2 className="text-base font-semibold">
            {editingId === null ? `新建${config.label}` : `编辑${config.label}`}
          </h2>
          {editingId === null ? null : (
            <Button type="button" size="sm" variant="ghost" onClick={cancelEdit}>
              取消编辑
            </Button>
          )}
        </div>

        <div className="grid gap-4 sm:grid-cols-2">
          <div className="space-y-2">
            <Label htmlFor={`${kind}-name`}>名称</Label>
            <Input
              id={`${kind}-name`}
              aria-invalid={errors.name ? true : undefined}
              aria-describedby={errors.name ? nameErrorId : undefined}
              {...register("name")}
            />
            {errors.name ? (
              <p id={nameErrorId} className="text-xs text-destructive">
                {errors.name.message}
              </p>
            ) : null}
          </div>

          <div className="space-y-2">
            <Label htmlFor={`${kind}-slug`}>slug</Label>
            <Input
              id={`${kind}-slug`}
              placeholder="留空则按英文名自动生成"
              aria-invalid={errors.slug ? true : undefined}
              aria-describedby={errors.slug ? slugErrorId : undefined}
              {...register("slug")}
            />
            {errors.slug ? (
              <p id={slugErrorId} className="text-xs text-destructive">
                {errors.slug.message}
              </p>
            ) : null}
          </div>
        </div>

        <div className="space-y-2">
          <Label htmlFor={`${kind}-description`}>描述</Label>
          <Textarea id={`${kind}-description`} rows={2} {...register("description")} />
        </div>

        <div className="flex flex-wrap items-center gap-2">
          <Button type="submit" disabled={isSubmitting}>
            {editingId === null ? "新建" : "保存"}
          </Button>
          <span className="text-xs text-muted-foreground">
            中文名必须手填 slug；slug 只能用小写字母、数字与连字符。
          </span>
        </div>
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
            {config.emptyHint}
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
                    <code className="rounded bg-muted px-1.5 py-0.5 text-xs text-muted-foreground">
                      {item.slug}
                    </code>
                    <span className="text-xs text-muted-foreground tabular-nums">
                      {item.count} 篇
                    </span>
                    <time className="text-xs text-muted-foreground">
                      {formatDateTime(item.createdAt)}
                    </time>
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
