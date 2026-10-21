import { z } from "zod";

import { LINK_STATUSES } from "@/lib/api/taxonomy";

/** slug 规则与 §4 一致：仅 `[a-z0-9-]`，且不允许首尾或连续连字符。 */
const SLUG_PATTERN = /^[a-z0-9]+(?:-[a-z0-9]+)*$/;

/** 友链的 url / avatar 必须是 http(s) 绝对地址。 */
const HTTP_URL_PATTERN = /^https?:\/\/\S+$/;

/** 排序值在表单里是字符串（原生 input），只允许非负整数。 */
const SORT_PATTERN = /^\d{1,5}$/;

/**
 * 跳转状态在表单里的字符串形态；取值必须与 `REDIRECT_STATUSES`（301 / 302）一致。
 *
 * 原生 `<select>` 的值只能是字符串，提交前用组件里的 `toRedirectStatus` 转回数字码，
 * 避免把数字塞进字符串字段而撒谎。
 */
export const redirectStatusValues = ["301", "302"] as const;

export type RedirectStatusValue = (typeof redirectStatusValues)[number];

/**
 * 标签 / 系列共用的表单 schema；约束与服务端 binding 一致（agent.md §8）。
 *
 * `slug` 留空表示交给服务端按 ASCII 名生成，中文名必须手填；长度 1–120 见 §4。
 */
export const taxonomySchema = z.object({
  name: z
    .string()
    .trim()
    .min(1, "请输入名称")
    .max(200, "名称不能超过 200 个字符"),
  slug: z
    .string()
    .trim()
    .max(120, "slug 不能超过 120 个字符")
    .refine(
      (value) => value === "" || SLUG_PATTERN.test(value),
      "slug 只能包含小写字母、数字与连字符",
    ),
  description: z.string().max(500, "描述不能超过 500 个字符"),
});

/** 标签 / 系列表单值。 */
export type TaxonomyValues = z.infer<typeof taxonomySchema>;

/** 友链表单 schema；`sort` 是字符串，提交前转成数字。 */
export const linkSchema = z.object({
  name: z
    .string()
    .trim()
    .min(1, "请输入名称")
    .max(100, "名称不能超过 100 个字符"),
  url: z
    .string()
    .trim()
    .min(1, "请输入链接")
    .max(500, "链接不能超过 500 个字符")
    .refine((value) => HTTP_URL_PATTERN.test(value), "链接必须是 http(s) 地址"),
  avatar: z
    .string()
    .trim()
    .max(500, "头像地址不能超过 500 个字符")
    .refine(
      (value) => value === "" || HTTP_URL_PATTERN.test(value),
      "头像必须是 http(s) 地址，或留空",
    ),
  description: z.string().max(200, "描述不能超过 200 个字符"),
  sort: z
    .string()
    .trim()
    .refine(
      (value) => value === "" || SORT_PATTERN.test(value),
      "排序只能填 0–99999 的整数",
    ),
  status: z.enum(LINK_STATUSES),
});

/** 友链表单值。 */
export type LinkValues = z.infer<typeof linkSchema>;

/** 跳转表单 schema；`fromPath` 唯一且必须以 `/` 开头。 */
export const redirectSchema = z.object({
  fromPath: z
    .string()
    .trim()
    .min(1, "请输入原路径")
    .max(200, "原路径不能超过 200 个字符")
    .refine((value) => value.startsWith("/"), "原路径必须以 / 开头"),
  toPath: z
    .string()
    .trim()
    .min(1, "请输入目标路径")
    .max(500, "目标路径不能超过 500 个字符")
    .refine(
      (value) => value.startsWith("/") || HTTP_URL_PATTERN.test(value),
      "目标路径必须以 / 开头，或是 http(s) 绝对地址",
    ),
  status: z.enum(redirectStatusValues),
});

/** 跳转表单值。 */
export type RedirectValues = z.infer<typeof redirectSchema>;
