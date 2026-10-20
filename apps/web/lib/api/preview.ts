import { apiFetch } from "./fetcher";

/**
 * 预览内容（agent.md §5 `GET /preview/{token}`）。
 *
 * 只够渲染这一页：草稿 / 定时 / 归档文章都能看，但**没有**标签、评论、
 * 上下篇与阅读数——预览页按 D13 口径不展示互动数据。
 */
export interface PreviewPost {
  slug: string;
  title: string;
  status: string;
  excerpt: string;
  contentHtml: string;
  wordCount: number;
  readingTime: number;
  /** 未发布（草稿 / 定时）时为 null。 */
  publishedAt: number | null;
  updatedAt: number;
  expiresAt: number;
}

/**
 * 公开预览接口。
 *
 * **只允许在服务端组件里调用**：接口是 `no-store` 且一次性——首次访问写 `used_at`
 * 并返回内容，之后或超过 24h 就是 `code 2006`（v1.14 起失败也是 HTTP 200）。
 * 在浏览器里调会白白烧掉令牌，而且 410 页面只能由服务端决定 HTTP 语义。
 */
export const publicPreviewApi = {
  /** 取一次性预览内容；token 不存在 → code 1002，已用 / 过期 → code 2006。 */
  get(token: string) {
    return apiFetch<PreviewPost>(`/api/preview/${encodeURIComponent(token)}`);
  },
};
