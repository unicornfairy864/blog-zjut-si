"use client";

import { zodResolver } from "@hookform/resolvers/zod";
import { useEffect, useState } from "react";
import { useForm } from "react-hook-form";
import useSWR from "swr";

import type { ApiError } from "@/lib/api/fetcher";
import { describeApiError } from "@/lib/api/fetcher";
import {
  adminSettingsApi,
  type AdminNotify,
  type AdminNotifyChannel,
  type AdminSettings,
  type AdminSettingsInput,
} from "@/lib/api/settings";
import { swrKeys } from "@/lib/swr/keys";
import { formatBytes, formatDateTime } from "@/lib/utils";
import { settingsSchema, type SettingsValues } from "@/lib/zod/settings";
import { FieldSelect } from "@/ui/admin/field-select";
import { Alert, AlertDescription } from "@/ui/shadcn/alert";
import { Button } from "@/ui/shadcn/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/ui/shadcn/card";
import { Input } from "@/ui/shadcn/input";
import { Label } from "@/ui/shadcn/label";
import { Loading } from "@/ui/shared/loading";

/** 空表单；只用于首次渲染，拿到数据后会被服务端值覆盖。 */
const EMPTY_VALUES: SettingsValues = {
  imgBaseUrl: "",
  siteTimezone: "",
  notify: {
    enabled: false,
    channel: "webhook",
    webhookUrl: "",
    clearWebhookUrl: false,
    telegramBotToken: "",
    clearTelegramBotToken: false,
    telegramChatId: "",
    events: { jobFailed: true, diskHigh: true, certExpiring: true },
    diskThresholdPercent: 85,
    certExpireDays: 14,
  },
};

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

/** 告警事件名 → 中文名（与服务端 `notify_state` 的键一致）。 */
const ALERT_EVENT_LABEL: Record<string, string> = {
  job_failed: "任务失败",
  disk_high: "磁盘占用过高",
  cert_expiring: "证书即将到期",
};

/** 把布尔开关写成中文，避免界面上出现 true / false。 */
function describeSwitch(enabled: boolean): string {
  return enabled ? "启用" : "停用";
}

/** 把字节数写成"可读大小（原始字节）"，只读回显里两个口径都给，排查时不用换算。 */
function describeBytes(bytes: number): string {
  return `${formatBytes(bytes)}（${bytes} 字节）`;
}

/** 渠道中文名。 */
function describeChannel(channel: AdminNotifyChannel): string {
  return channel === "telegram" ? "Telegram" : "webhook";
}

/** 最近一次告警的中文摘要；没有记录时说明还没发过。 */
function describeLastAlerts(notify: AdminNotify): string {
  if (notify.lastAlerts.length === 0) {
    return "还没有发送过";
  }
  return notify.lastAlerts
    .map((alert) => {
      const name = ALERT_EVENT_LABEL[alert.event] ?? alert.event;
      return `${name} ${formatDateTime(alert.at)}（${alert.ok ? "成功" : "失败"}）`;
    })
    .join("；");
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
      title: "告警",
      rows: [
        { label: "当前渠道", value: describeChannel(data.notify.channel) },
        {
          label: "证书文件",
          value: data.notify.certFile || "未配置（跳过证书检查）",
        },
        { label: "最近告警", value: describeLastAlerts(data.notify) },
      ],
    },
    {
      title: "数据库",
      rows: [{ label: "SQLite 路径", value: data.db.path }],
    },
  ];
}

/**
 * 密钥字段的提交规则：勾了"清除"就发空串，填了内容就覆盖，留空表示保持不变（`null`）。
 *
 * 服务端从不回显密钥，所以"留空"必须能区分"不改"与"清空"这两种意图。
 */
function secretValue(input: string, clear: boolean): string | null {
  if (clear) {
    return "";
  }
  const trimmed = input.trim();
  return trimmed === "" ? null : trimmed;
}

/**
 * 站点设置（D14 / D17）：可写 `imgBaseUrl` / `siteTimezone` / `notify`，其余只读回显。
 *
 * 写内容类设置会 `content_version+1`（公开读的 ETag 依赖它），提示里要点明；
 * 只改告警配置不会动 `content_version`。口令与告警密钥属于内部密钥，服务端任何接口都不返回（§9）。
 */
export function SettingsForm() {
  const [error, setError] = useState<string | null>(null);
  const [saved, setSaved] = useState<string | null>(null);
  const [testing, setTesting] = useState(false);

  const { data, error: loadError, isLoading, mutate } = useSWR<
    AdminSettings,
    ApiError
  >(swrKeys.adminSettings, () => adminSettingsApi.get());

  const {
    register,
    handleSubmit,
    reset,
    formState: { errors, isSubmitting, dirtyFields },
  } = useForm<SettingsValues>({
    resolver: zodResolver(settingsSchema),
    defaultValues: EMPTY_VALUES,
  });

  // 服务端是唯一真源：拉到数据（含保存后重新校验）就把表单同步成当前值。
  // 两个密钥字段刻意留空：服务端不回显，输入框只承担"覆盖"这一种意图。
  useEffect(() => {
    if (data) {
      reset({
        imgBaseUrl: data.imgBaseUrl,
        siteTimezone: data.siteTimezone,
        notify: {
          enabled: data.notify.enabled,
          channel: data.notify.channel,
          webhookUrl: "",
          clearWebhookUrl: false,
          telegramBotToken: "",
          clearTelegramBotToken: false,
          telegramChatId: data.notify.telegramChatId,
          events: data.notify.events,
          diskThresholdPercent: data.notify.diskThresholdPercent,
          certExpireDays: data.notify.certExpireDays,
        },
      });
    }
  }, [data, reset]);

  async function onSubmit(values: SettingsValues) {
    setError(null);
    setSaved(null);
    const { notify } = values;
    try {
      // 只提交真正改动的内容类字段：只调告警时不该让 content_version 白涨一次。
      const payload: AdminSettingsInput = {
        notify: {
          enabled: notify.enabled,
          channel: notify.channel,
          webhookUrl: secretValue(notify.webhookUrl, notify.clearWebhookUrl),
          telegramBotToken: secretValue(
            notify.telegramBotToken,
            notify.clearTelegramBotToken,
          ),
          telegramChatId: notify.telegramChatId,
          events: notify.events,
          diskThresholdPercent: notify.diskThresholdPercent,
          certExpireDays: notify.certExpireDays,
        },
      };
      if (dirtyFields.imgBaseUrl) {
        payload.imgBaseUrl = values.imgBaseUrl;
      }
      if (dirtyFields.siteTimezone) {
        payload.siteTimezone = values.siteTimezone;
      }

      await adminSettingsApi.update(payload);
      await mutate();
      setSaved(
        payload.imgBaseUrl !== undefined || payload.siteTimezone !== undefined
          ? "已保存（公开读的 ETag 已随之失效）"
          : "已保存",
      );
    } catch (caught) {
      setError(describeApiError(caught, "保存失败，请稍后重试"));
    }
  }

  async function onTestNotify() {
    setError(null);
    setSaved(null);
    setTesting(true);
    try {
      // 测的是**已保存**的配置，所以改了通道要先保存再测。
      await adminSettingsApi.testNotify();
      setSaved("测试告警已发出，去目标通道确认是否收到");
    } catch (caught) {
      setError(describeApiError(caught, "发送失败，请稍后重试"));
    } finally {
      setTesting(false);
    }
  }

  return (
    <section className="space-y-6">
      <header className="space-y-2">
        <h1 className="text-xl font-semibold">设置</h1>
        <p className="text-sm text-muted-foreground">
          可改图片前缀、站点时区与告警通道；其余是运维排查用的只读回显，口令与告警密钥永不下发。
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
          <form onSubmit={handleSubmit(onSubmit)} className="space-y-6">
            <div className="space-y-4 rounded-lg border border-border p-4">
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
                  <p
                    id="settings-timezone-error"
                    className="text-xs text-destructive"
                  >
                    {errors.siteTimezone.message}
                  </p>
                ) : (
                  <p className="text-xs text-muted-foreground">
                    IANA 名称；统计按它切日，名称不合法会被服务端拒绝。
                  </p>
                )}
              </div>
            </div>

            <div className="space-y-4 rounded-lg border border-border p-4">
              <div className="space-y-1">
                <h2 className="text-base font-semibold">告警通知（notify）</h2>
                <p className="text-xs text-muted-foreground">
                  任务失败、磁盘占用达到阈值、证书即将到期各发一次；同一条告警 24
                  小时内只发一次，条件恢复后重新计数。
                </p>
              </div>

              <div className="flex items-center gap-2">
                <input
                  id="settings-notify-enabled"
                  type="checkbox"
                  className="h-4 w-4"
                  {...register("notify.enabled")}
                />
                <Label htmlFor="settings-notify-enabled">启用告警</Label>
              </div>

              <div className="space-y-2">
                <Label htmlFor="settings-notify-channel">渠道</Label>
                <FieldSelect
                  id="settings-notify-channel"
                  {...register("notify.channel")}
                >
                  <option value="webhook">webhook（POST 一段 JSON）</option>
                  <option value="telegram">Telegram（Bot API）</option>
                </FieldSelect>
              </div>

              <div className="space-y-2">
                <Label htmlFor="settings-notify-webhook">webhook 地址</Label>
                <Input
                  id="settings-notify-webhook"
                  placeholder={
                    data.notify.webhookUrlSet
                      ? "已配置（留空则不修改）"
                      : "https://example.com/hook"
                  }
                  aria-invalid={errors.notify?.webhookUrl ? true : undefined}
                  {...register("notify.webhookUrl")}
                />
                {errors.notify?.webhookUrl ? (
                  <p className="text-xs text-destructive">
                    {errors.notify.webhookUrl.message}
                  </p>
                ) : null}
                {data.notify.webhookUrlSet ? (
                  <label className="flex items-center gap-2 text-xs text-muted-foreground">
                    <input
                      type="checkbox"
                      className="h-4 w-4"
                      {...register("notify.clearWebhookUrl")}
                    />
                    清除已保存的 webhook 地址
                  </label>
                ) : null}
              </div>

              <div className="grid gap-4 sm:grid-cols-2">
                <div className="space-y-2">
                  <Label htmlFor="settings-notify-token">Telegram bot token</Label>
                  <Input
                    id="settings-notify-token"
                    type="password"
                    autoComplete="off"
                    placeholder={
                      data.notify.telegramBotTokenSet
                        ? "已配置（留空则不修改）"
                        : "123456:ABC-DEF…"
                    }
                    {...register("notify.telegramBotToken")}
                  />
                  {data.notify.telegramBotTokenSet ? (
                    <label className="flex items-center gap-2 text-xs text-muted-foreground">
                      <input
                        type="checkbox"
                        className="h-4 w-4"
                        {...register("notify.clearTelegramBotToken")}
                      />
                      清除已保存的 token
                    </label>
                  ) : null}
                </div>

                <div className="space-y-2">
                  <Label htmlFor="settings-notify-chat">Telegram chatId</Label>
                  <Input
                    id="settings-notify-chat"
                    placeholder="-1001234567890"
                    {...register("notify.telegramChatId")}
                  />
                </div>
              </div>

              <fieldset className="space-y-2">
                <legend className="text-sm font-medium">触发哪些事件</legend>
                <div className="flex flex-wrap gap-4 text-sm">
                  <label className="flex items-center gap-2">
                    <input
                      type="checkbox"
                      className="h-4 w-4"
                      {...register("notify.events.jobFailed")}
                    />
                    任务失败
                  </label>
                  <label className="flex items-center gap-2">
                    <input
                      type="checkbox"
                      className="h-4 w-4"
                      {...register("notify.events.diskHigh")}
                    />
                    磁盘占用过高
                  </label>
                  <label className="flex items-center gap-2">
                    <input
                      type="checkbox"
                      className="h-4 w-4"
                      {...register("notify.events.certExpiring")}
                    />
                    证书即将到期
                  </label>
                </div>
              </fieldset>

              <div className="grid gap-4 sm:grid-cols-2">
                <div className="space-y-2">
                  <Label htmlFor="settings-notify-disk">磁盘阈值（%）</Label>
                  <Input
                    id="settings-notify-disk"
                    type="number"
                    min={1}
                    max={99}
                    aria-invalid={errors.notify?.diskThresholdPercent ? true : undefined}
                    {...register("notify.diskThresholdPercent", {
                      valueAsNumber: true,
                    })}
                  />
                  {errors.notify?.diskThresholdPercent ? (
                    <p className="text-xs text-destructive">
                      {errors.notify.diskThresholdPercent.message}
                    </p>
                  ) : (
                    <p className="text-xs text-muted-foreground">
                      默认 85，与 /readyz 的口径一致。
                    </p>
                  )}
                </div>

                <div className="space-y-2">
                  <Label htmlFor="settings-notify-cert">证书剩余天数阈值</Label>
                  <Input
                    id="settings-notify-cert"
                    type="number"
                    min={1}
                    max={3650}
                    aria-invalid={errors.notify?.certExpireDays ? true : undefined}
                    {...register("notify.certExpireDays", { valueAsNumber: true })}
                  />
                  {errors.notify?.certExpireDays ? (
                    <p className="text-xs text-destructive">
                      {errors.notify.certExpireDays.message}
                    </p>
                  ) : (
                    <p className="text-xs text-muted-foreground">
                      默认 14；未配置证书文件时跳过检查。
                    </p>
                  )}
                </div>
              </div>
            </div>

            <div className="flex flex-wrap items-center gap-2">
              <Button type="submit" disabled={isSubmitting}>
                保存
              </Button>
              <Button
                type="button"
                variant="outline"
                disabled={testing}
                onClick={() => void onTestNotify()}
              >
                {testing ? "发送中…" : "发送测试"}
              </Button>
              <span className="text-xs text-muted-foreground">
                测试用的是已保存的配置，改完通道请先保存再测。
              </span>
            </div>
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
