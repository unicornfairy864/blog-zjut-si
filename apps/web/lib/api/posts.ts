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

/**
 * 版本快照（agent.md §4 `post_revisions`）。
 *
 * 列表接口**不返回正文**：回滚时服务端按 id 自己取，前端不需要把长文搬来搬去。
 */
export interface PostRevision {
  id: number;
  postId: number;
  revisionNo: number;
  title: string;
  note: string;
  createdAt: number;
}

/** 一次性预览链接；`token` 明文只在生成那一次返回，库里只存哈希。 */
export interface PreviewLink {
  token: string;
  url: string;
  expiresAt: number;
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

  /** 版本列表（最多 50 条，按 revisionNo 倒序，不含正文）。 */
  revisions(id: number) {
    return apiFetch<{ list: PostRevision[] }>(
      `/api/admin/posts/${id}/revisions`,
    );
  },

  /**
   * 回滚到指定版本（D13）。
   *
   * 服务端用旧版本内容覆盖正文，并**再写一条新 revision**（历史不被改写），
   * 所以 note 记的是来源；不传时省略字段，由服务端补默认文案。
   */
  restoreRevision(id: number, revisionId: number, note?: string) {
    return apiFetch<PostDetail>(
      `/api/admin/posts/${id}/revisions/${revisionId}/restore`,
      { json: note ? { note } : {} },
    );
  },

  /**
   * 定时发布（D13）：`publishedAt` 必须是未来时间，否则服务端 `400 / 1003`。
   *
   * 重复调用会先撤掉该文待执行的 `publish_scheduled` 任务，所以这里可以直接改时间。
   */
  schedule(id: number, input: { publishedAt: number; note?: string }) {
    return apiFetch<PostDetail>(`/api/admin/posts/${id}/schedule`, {
      json: input,
    });
  },

  /** 生成一次性预览链接；明文 token 只回这一次，之后无从再取。 */
  createPreviewLink(id: number) {
    return apiFetch<PreviewLink>(`/api/admin/posts/${id}/preview-link`, {
      json: {},
    });
  },

  /** 立即作废预览链接；token 是 hex，仍走 encodeURIComponent 以免将来换字符集出事。 */
  deletePreviewLink(token: string) {
    return apiFetch<{ deleted: boolean }>(
      `/api/admin/preview-links/${encodeURIComponent(token)}`,
      { method: "DELETE" },
    );
  },

  /** 服务端预览：不落库，返回与发布一致的 HTML。 */
  preview(input: { contentMd: string; excerpt?: string }) {
    return apiFetch<PostPreview>("/api/admin/posts/preview", { json: input });
  },
};
