import { z } from "zod";

/**
 * IANA 时区名的形状（`Asia/Shanghai`、`UTC`）。
 *
 * 前端没有权威的时区列表，只做形状校验；名称是否真实存在由服务端的
 * `time.LoadLocation` 判定，非法时返回 `400 / 1003`。
 */
const TIMEZONE_PATTERN = /^[A-Za-z][A-Za-z0-9_+-]*(?:\/[A-Za-z0-9_+-]+)*$/;

/** 图片前缀填了就必须是 http(s) 绝对地址（禁止 `?v=` 之类的查询串，§10）。 */
const HTTP_URL_PATTERN = /^https?:\/\/\S+$/;

/**
 * 站点设置 schema；只覆盖可写项，约束与服务端 binding 一致（agent.md §8）。
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
});

/** 站点设置表单值。 */
export type SettingsValues = z.infer<typeof settingsSchema>;
