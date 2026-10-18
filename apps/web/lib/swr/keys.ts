/** SWR 的缓存键集中管理，避免组件里手写字符串。 */
export const swrKeys = {
  /** 当前管理端会话。 */
  adminMe: "/api/admin/auth/me",
} as const;
