"use client";

import { zodResolver } from "@hookform/resolvers/zod";
import { useEffect, useState } from "react";
import { useForm } from "react-hook-form";
import useSWR from "swr";

import type { ApiError } from "@/lib/api/fetcher";
import { adminSettingsApi, type AdminSettings } from "@/lib/api/settings";
import { swrKeys } from "@/lib/swr/keys";
import { formatBytes } from "@/lib/utils";
import { settingsSchema, type SettingsValues } from "@/lib/zod/settings";
import { Alert, AlertDescription } from "@/ui/shadcn/alert";
import { Button } from "@/ui/shadcn/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/ui/shadcn/card";
import { Input } from "@/ui/shadcn/input";
import { Label } from "@/ui/shadcn/label";
import { Loading } from "@/ui/shared/loading";

/** 空表单；只用于首次渲染，拿到数据后会被服务端值覆盖。 */
const EMPTY_VALUES: SettingsValues = { imgBaseUrl: "", siteTimezone: "" };

/** 只读回显里的一行。 */
interface ReadonlyRow {
  label: string;
  value: string;
}

/** 只读回显分组。 */
interface ReadonlyGroup {
  title: string;
  rows: ReadonlyRow[];
}

/** 把布尔开关写成中文，避免界面上出现 true / false。 */
function describeSwitch(enabled: boolean): string {
  return enabled ? "启用" : "停用";
}

/** 把字节数写成"可读大小（原始字节）"，只读回显里两个口径都给，排查时不用换算。 */
function describeBytes(bytes: number): string {
  return `${formatBytes(bytes)}（${bytes} 字节）`;
}

/** 只读项按「运维排查时一起看」的关系分组。 */
function buildReadonlyGroups(data: AdminSettings): ReadonlyGroup[] {
  return [
    {
      title: "内容",
      rows: [{ label: "内容版本", value: String(data.contentVersion) }],
    },
    {
      // 图片与附件都由本站托管：两个目录、两个上限，改不了，只能看。
      title: "存储",
      rows: [
        { label: "图片目录", value: data.upload.dir },
        { label: "图片上限", value: describeBytes(data.upload.maxBytes) },
        { label: "附件目录", value: data.file.dir },
        { label: "附件上限", value: describeBytes(data.file.maxBytes) },
      ],
    },
    {
      title: "任务调度",
      rows: [
        { label: "调度开关", value: describeSwitch(data.task.enabled) },
        { label: "扫描周期", value: `${data.task.intervalSeconds} 秒` },
      ],
    },
    {
      title: "数据库",
      rows: [{ label: "SQLite 路径", value: data.db.path }],
    },
  ];
}

/**
 * 站点设置（D14）：只写 `imgBaseUrl` / `siteTimezone`，其余只读回显。
 *
 * 写成功会 `content_version+1`，公开读接口的 ETag 依赖它，所以提示里要点明
 * "公开读的 ETag 已随之失效"，避免管理员以为保存只影响管理端。
 * 口令属于内部密钥，服务端任何接口都不返回（agent.md §9），因此这里也没有对应字段。
 */
export function SettingsForm() {
  const [error, setError] = useState<string | null>(null);
  const [saved, setSaved] = useState<string | null>(null);

  const { data, error: loadError, isLoading, mutate } = useSWR<
    AdminSettings,
    ApiError
  >(swrKeys.adminSettings, () => adminSettingsApi.get());

  const {
    register,
    handleSubmit,
    reset,
    formState: { errors, isSubmitting },
  } = useForm<SettingsValues>({
    resolver: zodResolver(settingsSchema),
    defaultValues: EMPTY_VALUES,
  });

  // 服务端是唯一真源：拉到数据（含保存后重新校验）就把表单同步成当前值。
  useEffect(() => {
    if (data) {
      reset({ imgBaseUrl: data.imgBaseUrl, siteTimezone: data.siteTimezone });
    }
  }, [data, reset]);

  async function onSubmit(values: SettingsValues) {
    setError(null);
    setSaved(null);
    try {
      await adminSettingsApi.update(values);
      await mutate();
      setSaved("已保存（公开读的 ETag 已随之失效）");
    } catch (caught) {
      setError(caught instanceof Error ? caught.message : "保存失败，请稍后重试");
    }
  }

  return (
    <section className="space-y-6">
      <header className="space-y-2">
        <h1 className="text-xl font-semibold">设置</h1>
        <p className="text-sm text-muted-foreground">
          只有图片前缀与站点时区可改；其余是运维排查用的只读回显，口令永不下发。
        </p>
      </header>

      {isLoading && !data ? <Loading /> : null}

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

      {saved ? (
        <Alert>
          <AlertDescription>{saved}</AlertDescription>
        </Alert>
      ) : null}

      {data ? (
        <>
          <form
            onSubmit={handleSubmit(onSubmit)}
            className="space-y-4 rounded-lg border border-border p-4"
          >
            <h2 className="text-base font-semibold">可写项</h2>

            <div className="space-y-2">
              <Label htmlFor="settings-img-base-url">图片前缀（imgBaseUrl）</Label>
              <Input
                id="settings-img-base-url"
                placeholder="https://blog.zjut.si/uploads"
                aria-invalid={errors.imgBaseUrl ? true : undefined}
                aria-describedby={
                  errors.imgBaseUrl ? "settings-img-base-url-error" : undefined
                }
                {...register("imgBaseUrl")}
              />
              {errors.imgBaseUrl ? (
                <p
                  id="settings-img-base-url-error"
                  className="text-xs text-destructive"
                >
                  {errors.imgBaseUrl.message}
                </p>
              ) : (
                <p className="text-xs text-muted-foreground">
                  留空表示输出相对路径 /uploads/…（由 nginx 直出）。
                </p>
              )}
            </div>

            <div className="space-y-2">
              <Label htmlFor="settings-timezone">站点时区（siteTimezone）</Label>
              <Input
                id="settings-timezone"
                placeholder="Asia/Shanghai"
                aria-invalid={errors.siteTimezone ? true : undefined}
                aria-describedby={
                  errors.siteTimezone ? "settings-timezone-error" : undefined
                }
                {...register("siteTimezone")}
              />
              {errors.siteTimezone ? (
                <p id="settings-timezone-error" className="text-xs text-destructive">
                  {errors.siteTimezone.message}
                </p>
              ) : (
                <p className="text-xs text-muted-foreground">
                  IANA 名称；统计按它切日，名称不合法会被服务端拒绝。
                </p>
              )}
            </div>

            <Button type="submit" disabled={isSubmitting}>
              保存
            </Button>
          </form>

          <section className="space-y-3">
            <h2 className="text-base font-semibold">只读回显</h2>
            <div className="grid gap-4 sm:grid-cols-2">
              {buildReadonlyGroups(data).map((group) => (
                <Card key={group.title}>
                  <CardHeader className="p-4 pb-2">
                    <CardTitle className="text-base">{group.title}</CardTitle>
                  </CardHeader>
                  <CardContent className="p-4 pt-0">
                    <dl className="space-y-1.5 text-sm">
                      {group.rows.map((row) => (
                        <div
                          key={row.label}
                          className="flex flex-wrap items-baseline justify-between gap-x-4 gap-y-1"
                        >
                          <dt className="text-muted-foreground">{row.label}</dt>
                          <dd className="break-all font-medium">{row.value}</dd>
                        </div>
                      ))}
                    </dl>
                  </CardContent>
                </Card>
              ))}
            </div>
          </section>
        </>
      ) : null}
    </section>
  );
}
