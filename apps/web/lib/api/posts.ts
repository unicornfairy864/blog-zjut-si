import { apiFetch } from "./fetcher";

/** 文章状态（agent.md §4）。 */
export type PostStatus = "draft" | "scheduled" | "published" | "archived";

/** 列表项：服务端裁剪过的轻量结构，不含正文。 */
export interface PostListItem {
  id: number;
  slug: string;
  title: string;
  excerpt: string;
  status: PostStatus;
  isPinned: boolean;
  allowComments: boolean;
  wordCount: number;
  readingTime: number;
  revision: number;
  lang: string;
  publishedAt: number | null;
  createdAt: number;
  updatedAt: number;
}

/** 详情：编辑器需要完整正文与 SEO 字段。 */
export interface PostDetail {
  id: number;
  slug: string;
  title: string;
  excerpt: string;
  contentMd: string;
  contentHtml: string;
  coverMediaId: number | null;
  status: PostStatus;
  publishedAt: number | null;
  createdAt: number;
  updatedAt: number;
  deletedAt: number | null;
  wordCount: number;
  readingTime: number;
  isPinned: boolean;
  allowComments: boolean;
  seriesId: number | null;
  seriesOrder: number;
  seoTitle: string;
  seoDescription: string;
  canonicalUrl: string;
  ogMediaId: number | null;
  viewCount: number;
  likeCount: number;
  commentCount: number;
  revision: number;
  lang: string;
}

/** 分页信封（GET /api/admin/posts）。 */
export interface PostListResult {
  list: PostListItem[];
  total: number;
  page: number;
  pageSize: number;
}

/** 写入字段；更新时 nil 语义由服务端处理（缺省即不改）。 */
export interface PostWriteInput {
  title: string;
  contentMd: string;
  excerpt?: string;
  slug?: string;
  seoTitle?: string;
  seoDescription?: string;
  canonicalUrl?: string;
  isPinned?: boolean;
  allowComments?: boolean;
  createRedirect?: boolean;
}

/** POST /api/admin/posts/preview 的结果；与发布落库的渲染同源。 */
export interface PostPreview {
  html: string;
  text: string;
  wordCount: number;
  readingTime: number;
  excerpt: string;
}

/** 管理端文章接口；浏览器侧一律同源调用。 */
export const adminPostApi = {
  /** 列表：status 为空表示全部。 */
  list(query: {
    status?: string;
    page?: number;
    pageSize?: number;
    keyword?: string;
  } = {}) {
    const params = new URLSearchParams();
    if (query.status) {
      params.set("status", query.status);
    }
    if (query.page) {
      params.set("page", String(query.page));
    }
    if (query.pageSize) {
      params.set("pageSize", String(query.pageSize));
    }
    if (query.keyword) {
      params.set("keyword", query.keyword);
    }
    const queryString = params.toString();
    return apiFetch<PostListResult>(
      `/api/admin/posts${queryString ? `?${queryString}` : ""}`,
    );
  },

  /** 详情。 */
  get(id: number) {
    return apiFetch<PostDetail>(`/api/admin/posts/${id}`);
  },

  /** 新建草稿。 */
  create(input: PostWriteInput) {
    return apiFetch<PostDetail>("/api/admin/posts", { json: input });
  },

  /** 局部更新；未提供的字段保持不变。 */
  update(id: number, input: Partial<PostWriteInput> & { status?: PostStatus }) {
    return apiFetch<PostDetail>(`/api/admin/posts/${id}`, {
      method: "PATCH",
      json: input,
    });
  },

  /** 软删（回收站）。 */
  remove(id: number) {
    return apiFetch<null>(`/api/admin/posts/${id}`, { method: "DELETE" });
  },

  /** 发布；note 落进 post_revisions.note。 */
  publish(id: number, note = "") {
    return apiFetch<PostDetail>(`/api/admin/posts/${id}/publish`, {
      json: { note },
    });
  },

  /** 取消发布 / 取消归档，回到草稿。 */
  unpublish(id: number) {
    return apiFetch<PostDetail>(`/api/admin/posts/${id}/unpublish`, {
      json: {},
    });
  },

  /** 版本列表（最多 50 条）。 */
  revisions(id: number) {
    return apiFetch<
      Array<{
        id: number;
        postId: number;
        revisionNo: number;
        title: string;
        note: string;
        createdAt: number;
      }>
    >(`/api/admin/posts/${id}/revisions`);
  },

  /** 服务端预览：不落库，返回与发布一致的 HTML。 */
  preview(input: { contentMd: string; excerpt?: string }) {
    return apiFetch<PostPreview>("/api/admin/posts/preview", { json: input });
  },
};
