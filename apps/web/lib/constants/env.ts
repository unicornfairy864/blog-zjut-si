/**
 * 运行环境与 API 地址（agent.md §8）。
 *
 * - 公开页在服务端取数，走容器内回环 http://127.0.0.1:8640；
 * - 管理页在浏览器取数，走同源 /api/admin/*（由 Go 的 8641 边缘处理）。
 */
const PUBLIC_API_FALLBACK = "http://127.0.0.1:8640";

/** 返回当前运行位置可用的 API 基址；浏览器侧返回空串即同源。 */
export function apiBaseUrl(): string {
  if (typeof window !== "undefined") {
    return "";
  }
  return process.env.BLOG_PUBLIC_API_URL ?? PUBLIC_API_FALLBACK;
}

/** 是否为浏览器环境。 */
export function isBrowser(): boolean {
  return typeof window !== "undefined";
}
