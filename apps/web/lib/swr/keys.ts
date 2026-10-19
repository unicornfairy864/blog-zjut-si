/** SWR 的缓存键集中管理，避免组件里手写字符串。 */
export const swrKeys = {
  /** 当前管理端会话。 */
  adminMe: "/api/admin/auth/me",
  /** 文章列表；status 为空表示全部。 */
  adminPosts: (status: string, page: number, pageSize: number) =>
    `/api/admin/posts?status=${status}&page=${page}&pageSize=${pageSize}`,
  /** 文章详情。 */
  adminPost: (id: number) => `/api/admin/posts/${id}`,
  /** 媒体库列表。 */
  adminMedia: (page: number, pageSize: number) =>
    `/api/admin/media?page=${page}&pageSize=${pageSize}`,
  /** 评论审核队列；status 传 all 表示全部。 */
  adminComments: (status: string, page: number, pageSize: number) =>
    `/api/admin/comments?status=${status}&page=${page}&pageSize=${pageSize}`,
  /** 公开文章的访客态统计；no-store，随访客变化。 */
  postStats: (slug: string) => `/api/posts/${slug}/stats`,
  /** 管理端看板总览。 */
  adminStatsOverview: "/api/admin/stats/overview",
  /** 管理端日粒度；from / to 都是上海日期 YYYY-MM-DD。 */
  adminStatsDaily: (from: string, to: string) =>
    `/api/admin/stats/daily?from=${from}&to=${to}`,
} as const;
