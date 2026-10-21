import { apiFetch } from "./fetcher";

/** 上传配置（只读回显）：目录与单文件字节上限。 */
export interface AdminSettingsUpload {
  dir: string;
  maxBytes: number;
}

/** 任务调度配置（只读回显）。 */
export interface AdminSettingsTask {
  enabled: boolean;
  intervalSeconds: number;
}

/** 附件存储配置（只读回显）：目录与单文件字节上限。 */
export interface AdminSettingsFile {
  dir: string;
  maxBytes: number;
}

/** 数据库配置（只读回显）。 */
export interface AdminSettingsDb {
  path: string;
}

/** 告警渠道；同一时刻只有一个生效（agent.md §13）。 */
export type AdminNotifyChannel = "webhook" | "telegram";

/** 三类告警事件的开关。 */
export interface AdminNotifyEvents {
  jobFailed: boolean;
  diskHigh: boolean;
  certExpiring: boolean;
}

/** 某事件最近一次投递结果（只读回显，用来判断"到底发没发出去"）。 */
export interface AdminNotifyLastAlert {
  event: string;
  at: number;
  ok: boolean;
}

/**
 * 告警配置（`GET /api/admin/settings` 的 `notify`）。
 *
 * 密钥（webhook 地址、Telegram bot token）**永不回显**，只给 `*Set` 布尔值；
 * 要清掉已保存的密钥，用 PUT 传空串（见 `AdminNotifyInput`）。
 */
export interface AdminNotify {
  enabled: boolean;
  channel: AdminNotifyChannel;
  webhookUrlSet: boolean;
  telegramBotTokenSet: boolean;
  telegramChatId: string;
  events: AdminNotifyEvents;
  diskThresholdPercent: number;
  certExpireDays: number;
  /** 容器内可见的证书路径（来自进程配置 `BLOG_TLS_CERT_FILE`）；空串表示不检查证书。 */
  certFile: string;
  lastAlerts: AdminNotifyLastAlert[];
}

/**
 * 告警配置的入参。
 *
 * 两个密钥字段传 `null` 表示"保持不变"、空串表示"清空"；其余字段总是覆盖。
 */
export interface AdminNotifyInput {
  enabled: boolean;
  channel: AdminNotifyChannel;
  webhookUrl: string | null;
  telegramBotToken: string | null;
  telegramChatId: string;
  events: AdminNotifyEvents;
  diskThresholdPercent: number;
  certExpireDays: number;
}

/**
 * GET /api/admin/settings 的 data。
 *
 * 可写项是 `imgBaseUrl`、`siteTimezone` 与 `notify`；其余字段是运维排查用的只读回显。
 * 图片与附件都由本站自己托管，各有一份目录与上限。
 */
export interface AdminSettings {
  imgBaseUrl: string;
  siteTimezone: string;
  contentVersion: number;
  upload: AdminSettingsUpload;
  file: AdminSettingsFile;
  task: AdminSettingsTask;
  db: AdminSettingsDb;
  notify: AdminNotify;
}

/**
 * PUT /api/admin/settings 的入参。
 *
 * 写 `imgBaseUrl` / `siteTimezone` 会让服务端 `content_version+1`（公开读的 ETag 随之失效）；
 * 只改 `notify` 不会动 `content_version`——告警配置与内容无关。
 */
export interface AdminSettingsInput {
  imgBaseUrl: string;
  siteTimezone: string;
  notify: AdminNotifyInput;
}

/** 站点设置接口（D14 / D17）。 */
export const adminSettingsApi = {
  /** 读当前设置；不含任何口令。 */
  get() {
    return apiFetch<AdminSettings>("/api/admin/settings");
  },

  /**
   * 写可写项。
   *
   * `imgBaseUrl` 留空表示用相对路径 `/uploads/…`；`siteTimezone` 必须是 IANA 名，
   * 非法时服务端返回 `code 1003`（HTTP 200）。
   */
  update(input: AdminSettingsInput) {
    return apiFetch<AdminSettings>("/api/admin/settings", {
      method: "PUT",
      json: input,
    });
  },

  /**
   * 按当前**已保存**的配置立刻投递一条测试告警。
   *
   * 不看 `enabled`，也不受 24h 静默期影响：这个接口就是用来先证明"能收到"的。
   * 通道没配齐时服务端返回 `code 1003`，投递失败返回 `code 1006`（都是 HTTP 200）。
   */
  testNotify() {
    return apiFetch<null>("/api/admin/notify/test", { method: "POST" });
  },
};
