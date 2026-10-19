import { z } from "zod";

/**
 * 数字输入框统一按字符串收集（HTML input 的 value 本来就是字符串），
 * 提交前再转 number；空串表示"不传这个字段"，由服务端保留原值或落默认值。
 */
const optionalInteger = (label: string) =>
  z
    .string()
    .trim()
    .refine(
      (value) => value === "" || /^\d+$/.test(value),
      `${label}必须是非负整数`,
    );

/**
 * 附件表单 schema；约束与服务端 binding 保持一致（agent.md §8）。
 *
 * 服务端要求 postId / title / shareUrl 必填、shareUrl 是 http(s)，这里做同一套前置校验，
 * 免得把明显不合法的输入打到接口上。
 */
export const attachmentFormSchema = z.object({
  postId: z
    .string()
    .trim()
    .regex(/^\d+$/, "文章 ID 必须是数字")
    .refine((value) => Number(value) >= 1, "文章 ID 必须大于 0"),
  title: z
    .string()
    .trim()
    .min(1, "请输入附件标题")
    .max(200, "标题不能超过 200 个字符"),
  shareUrl: z
    .string()
    .trim()
    .min(1, "请输入分享链接")
    .refine(
      (value) => /^https?:\/\/\S+$/.test(value),
      "分享链接必须是 http(s) 地址",
    ),
  fileName: z.string().trim().max(255, "文件名不能超过 255 个字符"),
  bytes: optionalInteger("文件大小"),
  sha256: z
    .string()
    .trim()
    .max(64, "sha256 不能超过 64 个字符")
    .refine(
      (value) => value === "" || /^[0-9a-fA-F]+$/.test(value),
      "sha256 只能是十六进制字符",
    ),
  version: z.string().trim().max(64, "版本号不能超过 64 个字符"),
  sort: optionalInteger("排序值"),
});

/** 附件表单值；字段全是字符串，数字在提交时再转换。 */
export type AttachmentFormValues = z.infer<typeof attachmentFormSchema>;
