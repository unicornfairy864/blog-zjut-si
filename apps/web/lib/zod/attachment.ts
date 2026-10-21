import { z } from "zod";

/**
 * 数字输入框统一按字符串收集（HTML input 的 value 本来就是字符串），
 * 提交前再转 number；空串表示"不传这个字段"，由服务端保留原值或落默认值。
 *
 * `max` 与服务端上限对齐（例如 `sort` 是 0–99999）：前端拦下来比让用户吃一次
 * `code 1003` 好——那个错误只会在表单顶部蹦一句"参数校验失败"。
 */
const optionalInteger = (label: string, max?: number) =>
  z
    .string()
    .trim()
    .refine(
      (value) => value === "" || /^\d+$/.test(value),
      `${label}必须是非负整数`,
    )
    .refine(
      (value) => value === "" || max === undefined || Number(value) <= max,
      `${label}不能超过 ${max ?? 0}`,
    );

/**
 * 分享链接：外链附件必填且必须是 http(s)；本地附件（storageKey 非空）允许留空。
 *
 * 服务端也按同一口径校验：填了就必须是 http(s)，留空只在本地附件上合法。
 */
function shareUrlField(required: boolean) {
  const field = z.string().trim().max(500, "分享链接不能超过 500 个字符");
  if (!required) {
    return field;
  }
  return field
    .min(1, "请输入分享链接")
    .refine(
      (value) => /^https?:\/\/\S+$/.test(value),
      "分享链接必须是 http(s) 地址",
    );
}

/**
 * 附件表单 schema；约束与服务端 binding 保持一致（agent.md §8）。
 *
 * 服务端要求 postId / title 必填，外链还要 shareUrl；这里做同一套前置校验，
 * 免得把明显不合法的输入打到接口上。
 */
function buildAttachmentFormSchema(requireShareUrl: boolean) {
  return z.object({
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
    shareUrl: shareUrlField(requireShareUrl),
    fileName: z.string().trim().max(200, "文件名不能超过 200 个字符"),
    bytes: optionalInteger("文件大小"),
    sha256: z
      .string()
      .trim()
      .max(64, "sha256 不能超过 64 个字符")
      .refine(
        (value) => value === "" || /^[0-9a-fA-F]+$/.test(value),
        "sha256 只能是十六进制字符",
      ),
    version: z.string().trim().max(50, "版本号不能超过 50 个字符"),
    sort: optionalInteger("排序值", 99999),
  });
}

/** 外链附件表单（新建 / 编辑外链）：shareUrl 必填。 */
export const attachmentFormSchema = buildAttachmentFormSchema(true);

/** 本地附件表单（编辑本地上传的附件）：shareUrl 允许留空。 */
export const attachmentLocalFormSchema = buildAttachmentFormSchema(false);

/** 附件表单值；字段全是字符串，数字在提交时再转换。 */
export type AttachmentFormValues = z.infer<typeof attachmentFormSchema>;
