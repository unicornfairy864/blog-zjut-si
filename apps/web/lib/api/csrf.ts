/**
 * CSRF 双提交令牌的浏览器侧存储。
 *
 * 令牌由 GET /api/admin/auth/me 下发；这里只放在内存 + sessionStorage，
 * 不写 localStorage、不进 URL（agent.md §9）。
 */
const STORAGE_KEY = "zjut.si:csrf";

/** 写请求必须携带的头名。 */
export const CSRF_HEADER = "X-CSRF-Token";

let memoryToken: string | null = null;

/** 保存令牌。 */
export function setCSRFToken(token: string): void {
  memoryToken = token;
  if (typeof window !== "undefined") {
    try {
      window.sessionStorage.setItem(STORAGE_KEY, token);
    } catch {
      // 隐私模式下 sessionStorage 可能不可用，退化为纯内存。
    }
  }
}

/** 读取令牌；没有会话时返回 null。 */
export function getCSRFToken(): string | null {
  if (memoryToken) {
    return memoryToken;
  }
  if (typeof window !== "undefined") {
    try {
      memoryToken = window.sessionStorage.getItem(STORAGE_KEY);
    } catch {
      memoryToken = null;
    }
  }
  return memoryToken;
}

/** 登出或 401 后清空令牌。 */
export function clearCSRFToken(): void {
  memoryToken = null;
  if (typeof window !== "undefined") {
    try {
      window.sessionStorage.removeItem(STORAGE_KEY);
    } catch {
      // 忽略
    }
  }
}
