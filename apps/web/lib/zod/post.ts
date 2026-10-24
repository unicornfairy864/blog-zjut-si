import { z } from "zod";

/**
 * 文章编辑器 schema；约束与服务端 binding 保持一致（agent.md §8）。
 *
 * slug 留空表示由服务端生成；canonical 允许留空，填写时必须是 http(s) 链接。
 * 分类字段（tagIds/seriesId/seriesOrder）与展示开关（isPinned/allowComments）在 D20 补齐：
 * 此前这些字段只有后端支持，编辑器没有入口，表现是"标签建了也挂不上、封面永远为空"。
 */
export const postEditorSchema = z.object({
  title: z
    .string()
    .trim()
    .min(1, "请输入标题")
    .max(200, "标题不能超过 200 个字符"),
  slug: z
    .string()
    .trim()
    .max(120, "slug 不能超过 120 个字符")
    .refine(
      (value) => value === "" || /^[a-z0-9]+(?:-[a-z0-9]+)*$/.test(value),
      "slug 只能包含小写字母、数字与连字符",
    ),
  contentMd: z.string().max(2_000_000, "正文过长"),
  excerpt: z.string().max(300, "摘要不能超过 300 个字符"),
  seoTitle: z.string().max(200, "SEO 标题不能超过 200 个字符"),
  seoDescription: z.string().max(500, "SEO 描述不能超过 500 个字符"),
  canonicalUrl: z
    .string()
    .trim()
    .max(500, "Canonical URL 不能超过 500 个字符")
    .refine(
      (value) => value === "" || /^https?:\/\/\S+$/.test(value),
      "Canonical URL 必须是 http(s) 链接",
    ),
  tagIds: z.array(z.number().int().positive()).max(20, "一篇文章最多 20 个标签"),
  seriesId: z.number().int().positive().nullable(),
  seriesOrder: z
    .number({ message: "请填 0 或正整数" })
    .int("请填整数")
    .min(0, "不能是负数")
    .max(99999, "最多 99999"),
  coverMediaId: z.number().int().positive().nullable(),
  ogMediaId: z.number().int().positive().nullable(),
  isPinned: z.boolean(),
  allowComments: z.boolean(),
  lang: z.string().trim().max(20, "语言标签不能超过 20 个字符"),
});

/** 编辑器表单值。 */
export type PostEditorValues = z.infer<typeof postEditorSchema>;
