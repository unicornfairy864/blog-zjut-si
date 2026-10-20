import { visitorHeaders } from "@/lib/visitor";

import { ApiError, apiFetch } from "./fetcher";

/** 判断是否是指定 HTTP 状态的业务错误（404 走 notFound、410 已下线）。 */
export function isApiError(error: unknown, status: number): boolean {
  return error instanceof ApiError && error.status === status;
}

/** 公开端探活响应。 */
export interface PingResult {
  pong: boolean;
  time: number;
}

/** 公开图片信息；url / variants / origUrl 都由服务端按 img_base_url 与本地上传目录拼好。 */
export interface PublicMedia {
  sha256: string;
  ext: string;
  width: number;
  height: number;
  alt: string;
  blurhash: string;
  url: string;
  origUrl: string;
  variants: Record<string, string>;
}

/** 文章上挂的标签（列表与详情共用）。 */
export interface PublicTagRef {
  name: string;
  slug: string;
}

/** 公开列表项（agent.md §5 / §12）。 */
export interface PublicPostListItem {
  id: number;
  slug: string;
  title: string;
  excerpt: string;
  coverMediaId: number | null;
  publishedAt: number | null;
  updatedAt: number;
  wordCount: number;
  readingTime: number;
  isPinned: boolean;
  cover: PublicMedia | null;
  tags: PublicTagRef[];
}

/** 上一篇 / 下一篇。 */
export interface PublicNeighbor {
  slug: string;
  title: string;
}

/** 公开详情。 */
export interface PublicPostDetail extends PublicPostListItem {
  contentHtml: string;
  seoTitle: string;
  seoDescription: string;
  canonicalUrl: string;
  lang: string;
  /** 作者是否允许评论（false 时前端不显示表单）。 */
  allowComments: boolean;
  prev: PublicNeighbor | null;
  next: PublicNeighbor | null;
}

/** 公开评论项；`replies` 只出现在顶层评论上（嵌套固定两层）。 */
export interface PublicComment {
  id: number;
  parentId: number | null;
  authorName: string;
  authorUrl: string;
  contentHtml: string;
  createdAt: number;
  replies?: PublicComment[];
}

/** 评论列表出参。 */
export interface CommentListResult {
  total: number;
  list: PublicComment[];
}

/** 提交评论的入参；邮箱与网址可空。 */
export interface CommentCreateInput {
  authorName: string;
  authorEmail?: string;
  authorUrl?: string;
  contentMd: string;
  parentId?: number | null;
}

/** 提交结果：新评论一律 pending，等管理员审核后才公开。 */
export interface CommentCreateResult {
  id: number;
  status: string;
  createdAt: number;
}

/** 访客态的阅读数与点赞数（D12）；计数不进 SSR HTML，只由客户端异步取。 */
export interface PostStats {
  viewCount: number;
  likeCount: number;
  /** 当前访客是否点过赞；依赖访客，所以接口是 no-store、不参与 ETag。 */
  liked: boolean;
}

/** 点赞 / 取消点赞后的权威状态。 */
export interface LikeState {
  liked: boolean;
  likeCount: number;
}

/** PV 打点结果；`viewCount` 只在 `slug` 命中已发布文章时才有值。 */
export interface ViewResult {
  /** 同 visitor 同文章 10 分钟内只 +1，重复打点为 false。 */
  recorded: boolean;
  viewCount: number | null;
}

/** 统一分页出参。 */
export interface Paged<T> {
  list: T[];
  total: number;
  page: number;
  pageSize: number;
}

/** 标签（带已发布文章数）。 */
export interface TagItem {
  id: number;
  name: string;
  slug: string;
  description: string;
  count: number;
}

/** 系列（带已发布文章数）。 */
export interface SeriesItem {
  id: number;
  name: string;
  slug: string;
  description: string;
  count: number;
}

/** 友链。 */
export interface LinkItem {
  id: number;
  name: string;
  url: string;
  avatar: string;
  description: string;
}

/** 标签详情：标签本身 + 该标签下的分页文章。 */
export interface TagPosts extends Paged<PublicPostListItem> {
  tag: Omit<TagItem, "count">;
}

/** 系列详情：系列本身 + 有序文章（系列不分页）。 */
export interface SeriesPosts {
  series: Omit<SeriesItem, "count">;
  list: PublicPostListItem[];
  total: number;
}

/** 归档里的文章条目（只有渲染列表需要的字段）。 */
export interface ArchivePost {
  id: number;
  slug: string;
  title: string;
  publishedAt: number;
}

/** 归档的月分组。 */
export interface ArchiveMonth {
  month: number;
  count: number;
  posts: ArchivePost[];
}

/** 归档的年分组。 */
export interface ArchiveYear {
  year: number;
  count: number;
  months: ArchiveMonth[];
}

/** 归档页数据。 */
export interface ArchiveResult {
  total: number;
  years: ArchiveYear[];
}

/** 站点级信息（settings 派生 + 计数）。 */
export interface SiteInfo {
  timezone: string;
  imgBaseUrl: string;
  contentVersion: number;
  counts: {
    posts: number;
    tags: number;
    series: number;
    links: number;
  };
}

/** 静态页（/about 等）。 */
export interface PageDetail {
  slug: string;
  title: string;
  contentHtml: string;
  seoTitle: string;
  seoDescription: string;
  updatedAt: number;
}

/** 拼查询串；空值直接省略。 */
function query(params: Record<string, string | number | undefined>): string {
  const search = new URLSearchParams();
  for (const [key, value] of Object.entries(params)) {
    if (value !== undefined && value !== "") {
      search.set(key, String(value));
    }
  }
  const result = search.toString();
  return result ? `?${result}` : "";
}

/**
 * 公开端接口（D9/D10）。
 *
 * 只在服务端组件里调用；浏览器不直连 8640。错误统一抛 `ApiError`
 * （`status === 404` 走 notFound，`status === 410` 由 middleware 拦截）。
 */
export const publicApi = {
  ping() {
    return apiFetch<PingResult>("/api/system/ping");
  },

  /** 站点信息。 */
  site() {
    return apiFetch<SiteInfo>("/api/site");
  },

  /** 文章列表（置顶优先，其次发布时间倒序）。 */
  posts(params: { page?: number; pageSize?: number } = {}) {
    return apiFetch<Paged<PublicPostListItem>>(
      `/api/posts${query({ page: params.page, pageSize: params.pageSize })}`,
    );
  },

  /** 文章详情；归档/已删除返回 410，草稿返回 404。 */
  post(slug: string) {
    return apiFetch<PublicPostDetail>(`/api/posts/${encodeURIComponent(slug)}`);
  },

  /** 相关文章（同标签或同系列）。 */
  related(slug: string) {
    return apiFetch<{ list: PublicPostListItem[] }>(
      `/api/posts/${encodeURIComponent(slug)}/related`,
    );
  },

  /** 文章的已通过评论：顶层评论 + 一层回复（agent.md §5）。 */
  comments(slug: string) {
    return apiFetch<CommentListResult>(
      `/api/posts/${encodeURIComponent(slug)}/comments`,
    );
  },

  /** 提交评论；成功即 pending，等管理员审核后才出现在公开列表里。 */
  createComment(slug: string, input: CommentCreateInput) {
    return apiFetch<CommentCreateResult>(
      `/api/posts/${encodeURIComponent(slug)}/comments`,
      { json: input },
    );
  },

  /**
   * 访客态统计（D12）：阅读数与点赞数。
   *
   * 带 `X-Visitor-Id` 才能拿到正确的 `liked`；接口是 no-store，
   * 所以每次都真的回源，SWR 的缓存由调用方按需决定。
   */
  postStats(slug: string) {
    return apiFetch<PostStats>(`/api/posts/${encodeURIComponent(slug)}/stats`, {
      headers: visitorHeaders(),
    });
  },

  /** 点赞；幂等，已点过也返回 200。 */
  like(slug: string) {
    return apiFetch<LikeState>(`/api/posts/${encodeURIComponent(slug)}/like`, {
      method: "POST",
      headers: visitorHeaders(),
    });
  },

  /** 取消点赞；幂等，没点过也返回 200。 */
  unlike(slug: string) {
    return apiFetch<LikeState>(`/api/posts/${encodeURIComponent(slug)}/like`, {
      method: "DELETE",
      headers: visitorHeaders(),
    });
  },

  /** PV 打点；`slug` 命中已发布文章时同时计文章行与全站行。 */
  recordView(input: { path: string; slug?: string; referrer?: string }) {
    return apiFetch<ViewResult>("/api/events/view", {
      json: input,
      headers: visitorHeaders(),
    });
  },

  /** 全文搜索。 */
  search(keyword: string, params: { page?: number; pageSize?: number } = {}) {
    return apiFetch<Paged<PublicPostListItem>>(
      `/api/search${query({
        q: keyword,
        page: params.page,
        pageSize: params.pageSize,
      })}`,
    );
  },

  /** 标签云。 */
  tags() {
    return apiFetch<{ list: TagItem[] }>("/api/tags");
  },

  /** 标签下的文章。 */
  tag(slug: string, params: { page?: number; pageSize?: number } = {}) {
    return apiFetch<TagPosts>(
      `/api/tags/${encodeURIComponent(slug)}${query({
        page: params.page,
        pageSize: params.pageSize,
      })}`,
    );
  },

  /** 归档（按年/月分组）。 */
  archive() {
    return apiFetch<ArchiveResult>("/api/archive");
  },

  /** 系列列表。 */
  series() {
    return apiFetch<{ list: SeriesItem[] }>("/api/series");
  },

  /** 系列详情。 */
  seriesDetail(slug: string) {
    return apiFetch<SeriesPosts>(`/api/series/${encodeURIComponent(slug)}`);
  },

  /** 友链。 */
  links() {
    return apiFetch<{ list: LinkItem[] }>("/api/links");
  },

  /** 静态页；不存在 404，软删 410。 */
  page(slug: string) {
    return apiFetch<PageDetail>(`/api/pages/${encodeURIComponent(slug)}`);
  },
};
