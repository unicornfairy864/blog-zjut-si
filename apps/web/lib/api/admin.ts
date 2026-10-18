import { apiFetch } from "./fetcher";
import { clearCSRFToken, setCSRFToken } from "./csrf";

/** 管理员的公开字段（不含 password_hash）。 */
export interface AdminProfile {
  id: number;
  username: string;
  email: string;
  lastLoginAt: number;
  createdAt: number;
  updatedAt: number;
}

/** POST /api/admin/auth/login 的 data。 */
export interface LoginResult {
  admin: AdminProfile;
  expiresAt: number;
}

/** GET /api/admin/auth/me 的 data。 */
export interface MeResult {
  admin: AdminProfile;
  csrfToken: string;
  expiresAt: number;
}

/** 管理端鉴权接口；浏览器侧一律同源调用。 */
export const adminApi = {
  /** 登录；成功后服务端已写入 sid cookie。 */
  login(input: { username: string; password: string }) {
    return apiFetch<LoginResult>("/api/admin/auth/login", { json: input });
  },

  /** 拉取当前会话，并把 csrfToken 存进内存供后续写请求使用。 */
  async me(): Promise<MeResult> {
    const result = await apiFetch<MeResult>("/api/admin/auth/me");
    setCSRFToken(result.csrfToken);
    return result;
  },

  /** 登出；无论服务端结果如何都清掉本地令牌。 */
  async logout(): Promise<void> {
    try {
      await apiFetch<null>("/api/admin/auth/logout", { json: {} });
    } finally {
      clearCSRFToken();
    }
  },
};
