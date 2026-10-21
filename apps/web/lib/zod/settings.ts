import { z } from "zod";

/**
 * IANA 时区名的形状（`Asia/Shanghai`、`UTC`）。
 *
 * 前端没有权威的时区列表，只做形状校验；名称是否真实存在由服务端的
 * `time.LoadLocation` 判定，非法时返回 `code 1003`（HTTP 200）。
 */
const TIMEZONE_PATTERN = /^[A-Za-z][A-Za-z0-9_+-]*(?:\/[A-Za-z0-9_+-]+)*$/;

/** 图片前缀填了就必须是 http(s) 绝对地址（禁止 `?v=` 之类的查询串，§10）。 */
const HTTP_URL_PATTERN = /^https?:\/\/\S+$/;

/**
 * 告警配置 schema（agent.md §13 / §14）。
 *
 * `webhookUrl` / `telegramBotToken` 是密钥：服务端从不回显，输入框留空表示"保持不变"，
 * 要清空得显式勾 `clearWebhookUrl` / `clearTelegramBotToken`（提交时转成空串）。
 */
export const notifySchema = z.object({
  enabled: z.boolean(),
  channel: z.enum(["webhook", "telegram"], {
    message: "只能是 webhook 或 telegram",
  }),
  webhookUrl: z
    .string()
    .trim()
    .max(500, "地址不能超过 500 个字符")
    .refine(
      (value) => value === "" || HTTP_URL_PATTERN.test(value),
      "必须是 http(s):// 开头的地址",
    ),
  clearWebhookUrl: z.boolean(),
  telegramBotToken: z.string().trim().max(200, "token 不能超过 200 个字符"),
  clearTelegramBotToken: z.boolean(),
  telegramChatId: z.string().trim().max(64, "chatId 不能超过 64 个字符"),
  events: z.object({
    jobFailed: z.boolean(),
    diskHigh: z.boolean(),
    certExpiring: z.boolean(),
  }),
  diskThresholdPercent: z
    .number({ message: "请填 1–99 的整数" })
    .int("请填整数")
    .min(1, "至少 1")
    .max(99, "最多 99"),
  certExpireDays: z
    .number({ message: "请填 1–3650 的整数" })
    .int("请填整数")
    .min(1, "至少 1")
    .max(3650, "最多 3650"),
});

/**
 * 站点设置 schema；只覆盖可写项，约束与服务端一致（agent.md §8）。
 *
 * `imgBaseUrl` 留空表示用相对路径 `/uploads/…`；填了必须是 `http(s)://` 且不带结尾斜杠
 * （带斜杠会拼出 `//` 的双斜杠 URL）。
 */
export const settingsSchema = z.object({
  imgBaseUrl: z
    .string()
    .trim()
    .max(500, "图片前缀不能超过 500 个字符")
    .refine(
      (value) =>
        value === "" ||
        (HTTP_URL_PATTERN.test(value) && !value.endsWith("/")),
      "必须是 http(s):// 开头且不以 / 结尾，留空表示用相对路径",
    ),
  siteTimezone: z
    .string()
    .trim()
    .min(1, "请输入站点时区")
    .max(64, "时区名不能超过 64 个字符")
    .refine((value) => TIMEZONE_PATTERN.test(value), "时区名形如 Asia/Shanghai"),
  notify: notifySchema,
});

/** 站点设置表单值。 */
export type SettingsValues = z.infer<typeof settingsSchema>;

/** 告警配置表单值。 */
export type NotifyValues = z.infer<typeof notifySchema>;
