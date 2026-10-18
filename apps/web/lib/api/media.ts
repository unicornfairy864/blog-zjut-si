import { apiFetch } from "./fetcher";

/** 媒体库条目；url 由服务端按 img_base_url 拼好。 */
export interface MediaItem {
  id: number;
  sha256: string;
  originalName: string;
  mime: string;
  ext: string;
  bytes: number;
  width: number;
  height: number;
  pixels: number;
  alt: string;
  status: string;
  error: string;
  url: string;
  variants: Record<string, string>;
  createdAt: number;
  updatedAt: number;
}

/** 媒体库分页信封。 */
export interface MediaListResult {
  list: MediaItem[];
  total: number;
  page: number;
  pageSize: number;
}

/** 管理端媒体接口。 */
export const adminMediaApi = {
  /** 媒体库列表。 */
  list(query: { page?: number; pageSize?: number } = {}) {
    const params = new URLSearchParams();
    if (query.page) {
      params.set("page", String(query.page));
    }
    if (query.pageSize) {
      params.set("pageSize", String(query.pageSize));
    }
    const queryString = params.toString();
    return apiFetch<MediaListResult>(
      `/api/admin/media${queryString ? `?${queryString}` : ""}`,
    );
  },

  /**
   * 上传图片；alt 必须排在 file 之前，服务端按流式顺序读取。
   */
  upload(file: File, alt = "") {
    const form = new FormData();
    if (alt) {
      form.append("alt", alt);
    }
    form.append("file", file);
    return apiFetch<MediaItem>("/api/admin/media", {
      method: "POST",
      body: form,
      timeoutMs: 120_000,
    });
  },

  /** 删除未被引用的图片；被引用时抛 ApiError(2002)。 */
  remove(id: number) {
    return apiFetch<null>(`/api/admin/media/${id}`, { method: "DELETE" });
  },

  /** 从本地原图重新生成变体。 */
  retry(id: number) {
    return apiFetch<MediaItem>(`/api/admin/media/${id}/retry`, { json: {} });
  },
};
