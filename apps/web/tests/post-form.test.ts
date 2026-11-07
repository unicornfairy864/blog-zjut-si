import type { FieldErrors } from "react-hook-form";
import { describe, expect, it } from "vitest";

import type { PostDetail } from "@/lib/api/posts";
import { postEditorSchema } from "@/lib/zod/post";
import {
  EMPTY_VALUES,
  firstFieldError,
  toFormValues,
  toWriteInput,
} from "@/ui/admin/post-form";

/**
 * 编辑器表单映射（agent.md §5 的"0 = 清空" + §8 的保存反馈）。
 *
 * 这层是保存按钮能不能发出请求的关键：`toFormValues` 少归一一个 0，整张表单就会
 * 永久校验失败，而用户看到的只是一句"请先补全标题等必填项"（D19 线上实测）。
 */

/** 造一篇详情；只覆盖用例关心的字段，其余给稳定默认值。 */
function makePost(overrides: Partial<PostDetail> = {}): PostDetail {
  return {
    id: 1,
    slug: "hello-world",
    title: "标题",
    excerpt: "摘要",
    contentMd: "# 标题\n",
    contentHtml: "<h1>标题</h1>",
    coverMediaId: null,
    status: "draft",
    publishedAt: null,
    createdAt: 1,
    updatedAt: 1,
    deletedAt: null,
    wordCount: 3,
    readingTime: 1,
    isPinned: false,
    allowComments: true,
    seriesId: null,
    seriesOrder: 0,
    seoTitle: "",
    seoDescription: "",
    canonicalUrl: "",
    ogMediaId: null,
    viewCount: 0,
    likeCount: 0,
    commentCount: 0,
    revision: 1,
    lang: "zh-CN",
    tagIds: [],
    cover: null,
    ogImage: null,
    ...overrides,
  };
}

/** 一份能通过校验的表单值（`EMPTY_VALUES` 的标题是空的，本来就不合法）。 */
function validValues() {
  return { ...EMPTY_VALUES, title: "标题" };
}

describe("toFormValues", () => {
  it("把外键上的 0 归一成 null，让校验能通过", () => {
    // 缺陷背景：Create 早先把接口层的 0 当外键写进库，于是详情回 coverMediaId=0。
    // schema 若只收正整数，`trigger()` 就永远返回 false，保存请求根本发不出去。
    const values = toFormValues(
      makePost({ coverMediaId: 0, ogMediaId: 0, seriesId: 0, tagIds: [0, 3] }),
    );

    expect(values.coverMediaId).toBeNull();
    expect(values.ogMediaId).toBeNull();
    expect(values.seriesId).toBeNull();
    expect(values.tagIds).toEqual([3]);

    const parsed = postEditorSchema.safeParse(values);
    expect(parsed.success, JSON.stringify(parsed.error?.issues)).toBe(true);
  });

  it("保留有效 id 与其余字段", () => {
    const values = toFormValues(
      makePost({
        title: "带封面的文章",
        coverMediaId: 7,
        ogMediaId: 8,
        seriesId: 2,
        seriesOrder: 3,
        tagIds: [4, 5],
        isPinned: true,
        allowComments: false,
        slug: "with-cover",
      }),
    );

    expect(values).toMatchObject({
      title: "带封面的文章",
      slug: "with-cover",
      coverMediaId: 7,
      ogMediaId: 8,
      seriesId: 2,
      seriesOrder: 3,
      tagIds: [4, 5],
      isPinned: true,
      allowComments: false,
    });
    expect(postEditorSchema.safeParse(values).success).toBe(true);
  });

  it("实体缺 tagIds 时给空数组，不给 undefined", () => {
    const values = toFormValues(makePost({ tagIds: undefined as unknown as number[] }));
    expect(values.tagIds).toEqual([]);
  });
});

describe("postEditorSchema 的外键口径", () => {
  it("放行 0 与 null：契约里 0 就是清空（§5）", () => {
    for (const field of ["seriesId", "coverMediaId", "ogMediaId"] as const) {
      expect(postEditorSchema.safeParse({ ...validValues(), [field]: null }).success).toBe(true);
      expect(postEditorSchema.safeParse({ ...validValues(), [field]: 0 }).success).toBe(true);
      expect(postEditorSchema.safeParse({ ...validValues(), [field]: 12 }).success).toBe(true);
      expect(postEditorSchema.safeParse({ ...validValues(), [field]: -1 }).success).toBe(false);
      expect(postEditorSchema.safeParse({ ...validValues(), [field]: 1.5 }).success).toBe(false);
    }
  });
});

describe("toWriteInput", () => {
  it("未设置的 id 发 0，有效 id 原样发出", () => {
    const input = toWriteInput(
      { ...validValues(), slug: " hello ", seriesId: null, coverMediaId: 5, ogMediaId: null },
      false,
    );

    expect(input.slug).toBe("hello");
    expect(input.seriesId).toBe(0);
    expect(input.ogMediaId).toBe(0);
    expect(input.coverMediaId).toBe(5);
    expect(input.createRedirect).toBe(false);
  });

  it("slug 为空时不传，交给服务端生成", () => {
    const input = toWriteInput({ ...validValues(), slug: "   " }, true);
    expect(input.slug).toBeUndefined();
    expect(input.createRedirect).toBe(true);
  });

  it("文本字段去掉首尾空白", () => {
    const input = toWriteInput(
      { ...validValues(), title: "  标题  ", excerpt: " 摘要 ", lang: " zh-CN " },
      false,
    );
    expect(input.title).toBe("标题");
    expect(input.excerpt).toBe("摘要");
    expect(input.lang).toBe("zh-CN");
  });
});

describe("firstFieldError", () => {
  it("取第一条字段错误的原文", () => {
    const errors = {
      seriesOrder: { type: "min", message: "请填 0 或正整数" },
    } as unknown as FieldErrors;

    expect(firstFieldError(errors)).toBe("请填 0 或正整数");
  });

  it("数组字段（tagIds）取元素上的 message", () => {
    const errors = {
      tagIds: [{ type: "too_big", message: "一篇文章最多 20 个标签" }],
    } as unknown as FieldErrors;

    expect(firstFieldError(errors)).toBe("一篇文章最多 20 个标签");
  });

  it("没有任何错误时返回 null，让调用方用兜底文案", () => {
    expect(firstFieldError({} as FieldErrors)).toBeNull();
    expect(firstFieldError({ title: undefined } as unknown as FieldErrors)).toBeNull();
  });
});
