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
} as const;
