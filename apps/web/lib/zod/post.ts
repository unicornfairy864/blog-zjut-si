import { z } from "zod";

/**
 * 文章编辑器 schema；约束与服务端 binding 保持一致（agent.md §8）。
 *
 * slug 留空表示由服务端生成；canonical 允许留空，填写时必须是 http(s) 链接。
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
    .max(500, "canonical 不能超过 500 个字符")
    .refine(
      (value) => value === "" || /^https?:\/\/\S+$/.test(value),
      "canonical 必须是 http(s) 链接",
    ),
});

/** 编辑器表单值。 */
export type PostEditorValues = z.infer<typeof postEditorSchema>;
