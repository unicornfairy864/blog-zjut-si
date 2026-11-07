import type { FieldErrors } from "react-hook-form";

import type { PostDetail, PostWriteInput } from "@/lib/api/posts";
import type { PostEditorValues } from "@/lib/zod/post";

/**
 * 文章编辑器的表单值映射（`post-editor.tsx` 的纯函数部分）。
 *
 * 单独成模块是为了能被单测直接覆盖：编辑器组件本身牵扯 DOM、拖拽与上传，
 * 而"接口实体 ↔ 表单值"这一层是保存按钮能不能发出请求的关键（见 `idOrNull`）。
 */

/** 编辑器表单的初值：新建草稿，以及详情还没到手时的占位。 */
export const EMPTY_VALUES: PostEditorValues = {
  title: "",
  slug: "",
  contentMd: "",
  excerpt: "",
  seoTitle: "",
  seoDescription: "",
  canonicalUrl: "",
  tagIds: [],
  seriesId: null,
  seriesOrder: 0,
  coverMediaId: null,
  ogMediaId: null,
  isPinned: false,
  allowComments: true,
  lang: "",
};

/**
 * 把外键归一成 `null`：接口契约里 `0` 就是"清空/未设置"（agent.md §5）。
 *
 * 必须归一：表单 schema 只接受正整数 id，一旦实体里带着 `0`（历史数据真的存过，
 * 见 D19 的 `cover_media_id = 0`），整张表单的校验就一直不通过，保存按钮每次都
 * 被拦在发请求之前，用户看到的是"请先补全标题等必填项"——标题其实没问题。
 */
function idOrNull(value: number | null | undefined): number | null {
  return typeof value === "number" && value > 0 ? value : null;
}

/** 实体 → 表单值。 */
export function toFormValues(post: PostDetail): PostEditorValues {
  return {
    title: post.title,
    slug: post.slug,
    contentMd: post.contentMd,
    excerpt: post.excerpt,
    seoTitle: post.seoTitle,
    seoDescription: post.seoDescription,
    canonicalUrl: post.canonicalUrl,
    tagIds: (post.tagIds ?? []).filter((id) => id > 0),
    seriesId: idOrNull(post.seriesId),
    seriesOrder: post.seriesOrder,
    coverMediaId: idOrNull(post.coverMediaId),
    ogMediaId: idOrNull(post.ogMediaId),
    isPinned: post.isPinned,
    allowComments: post.allowComments,
    lang: post.lang,
  };
}

/** 表单值 → 接口入参；slug 为空时不传，交给服务端生成。 */
export function toWriteInput(
  values: PostEditorValues,
  createRedirect: boolean,
): PostWriteInput {
  const slug = values.slug.trim();
  return {
    title: values.title.trim(),
    contentMd: values.contentMd,
    excerpt: values.excerpt.trim(),
    ...(slug ? { slug } : {}),
    seoTitle: values.seoTitle.trim(),
    seoDescription: values.seoDescription.trim(),
    canonicalUrl: values.canonicalUrl.trim(),
    createRedirect,
    // 分类与展示字段：这些此前只有后端支持，编辑器没有入口（v1.17 补齐）。
    tagIds: values.tagIds,
    // 未设置统一发 0，服务端按"清空"处理（§5）；新建与更新走同一套语义。
    seriesId: values.seriesId ?? 0,
    seriesOrder: values.seriesOrder,
    coverMediaId: values.coverMediaId ?? 0,
    ogMediaId: values.ogMediaId ?? 0,
    isPinned: values.isPinned,
    allowComments: values.allowComments,
    lang: values.lang.trim(),
  };
}

/**
 * 取第一条字段错误的提示，用作保存失败横幅的文案。
 *
 * 横幅此前固定写"请先补全标题等必填项"：标题没问题时它就在说谎，真正拦下来的
 * 可能是别的字段（系列序号、Canonical URL……）。校验器的原文比笼统的兜底更有用。
 */
export function firstFieldError(errors: FieldErrors): string | null {
  for (const field of Object.values(errors)) {
    if (!field) {
      continue;
    }
    // 数组字段（tagIds）的错误挂在元素上，取第一个元素的 message。
    const candidates = Array.isArray(field) ? field : [field];
    for (const candidate of candidates) {
      const message = (candidate as { message?: unknown } | null)?.message;
      if (typeof message === "string" && message) {
        return message;
      }
    }
  }
  return null;
}
