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
  /** 文章的版本列表（不含正文，回滚时才按 id 取）。 */
  adminPostRevisions: (postId: number) =>
    `/api/admin/posts/${postId}/revisions`,
  /** 标签 / 系列 / 友链 / 跳转：管理端都是全量列表，不分页。 */
  adminTags: "/api/admin/tags",
  adminSeries: "/api/admin/series",
  adminLinks: "/api/admin/links",
  adminRedirects: "/api/admin/redirects",
  /** 站点设置（可写项 + 只读回显）。 */
  adminSettings: "/api/admin/settings",
  /** 附件分页；postId 为空表示全部。 */
  adminAttachments: (postId: string, page: number, pageSize: number) =>
    `/api/admin/attachments?postId=${postId}&page=${page}&pageSize=${pageSize}`,
  /** 任务队列分页；status 为空表示全部。 */
  adminJobs: (status: string, page: number, pageSize: number) =>
    `/api/admin/jobs?status=${status}&page=${page}&pageSize=${pageSize}`,
  /** 审计日志分页；筛选条件拼进 key，保证换筛选即换缓存。 */
  adminAuditLogs: (filter: string, page: number, pageSize: number) =>
    `/api/admin/audit-logs?${filter}&page=${page}&pageSize=${pageSize}`,
} as const;
