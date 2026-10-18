import { apiFetch } from "./fetcher";

/** 公开端探活响应。 */
export interface PingResult {
  pong: boolean;
  time: number;
}

/**
 * 公开端接口（D9 起补 site / posts / tags …）。
 *
 * 只在服务端组件里调用；浏览器不直连 8640。
 */
export const publicApi = {
  ping() {
    return apiFetch<PingResult>("/api/system/ping");
  },
};
