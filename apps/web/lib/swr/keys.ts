/** SWR 的缓存键集中管理，避免组件里手写字符串。 */
export const swrKeys = {
  /** 当前管理端会话。 */
  adminMe: "/api/admin/auth/me",
  /** 文章列表；status 为空表示全部。 */
  adminPosts: (status: string) => `/api/admin/posts?status=${status}`,
  /** 文章详情。 */
  adminPost: (id: number) => `/api/admin/posts/${id}`,
} as const;
