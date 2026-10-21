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
  blurhash: string;
  status: string;
  error: string;
  /** 缩略图（默认 1600 档）；status 非 ready 时可能还没有真实文件。 */
  url: string;
  /** 本地原图直链（同源 /uploads/…）；原图已删除时为空串。 */
  origUrl: string;
  /** 原图是否已被删除（只保留四档变体）。 */
  origArchived: boolean;
  /** 原图删除时间（Unix 秒）；没删过是 null。 */
  origArchivedAt: number | null;
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

  /** 从本地原图重新生成变体；原图已删除时抛 ApiError(code 1002)。 */
  retry(id: number) {
    return apiFetch<MediaItem>(`/api/admin/media/${id}/retry`, { json: {} });
  },

  /**
   * 只删除本地原图，四档变体保留。
   *
   * 返回删除后的媒体条目（`origArchived` 为 true、`origUrl` 为空串）；
   * 删掉后不能再用 retry 重新转码。
   */
  removeOriginal(id: number) {
    return apiFetch<MediaItem>(`/api/admin/media/${id}/original`, {
      method: "DELETE",
    });
  },
};

/**
 * 正文里插入一张图用的 Markdown。
 *
 * alt 优先用人工描述，其次原文件名（去掉扩展名）；方括号会截断 Markdown 的
 * 链接语法，换行会破坏段落，所以这两类字符在这里就去掉。
 */
export function mediaMarkdown(item: MediaItem): string {
  const alt = (item.alt || item.originalName.replace(/\.[^.]+$/, ""))
    .replaceAll("[", "")
    .replaceAll("]", "")
    .replace(/[\r\n]+/g, " ")
    .trim();
  return `![${alt || "图片"}](${item.url})`;
}

/** `waitForMediaReady` 的可注入点：测试用假时钟与假数据源，生产走默认实现。 */
export interface WaitForMediaReadyOptions {
  /** 最长等待时间，默认 30 秒。 */
  timeoutMs?: number;
  /** 轮询间隔，默认 1.5 秒。 */
  intervalMs?: number;
  sleep?: (ms: number) => Promise<void>;
  now?: () => number;
  find?: (id: number) => Promise<MediaItem | null>;
}

/**
 * 等到图片转码完成再返回条目。
 *
 * `processing` 状态下 `item.url` 只是按 sha256 推出来的预测地址：转码失败就是
 * 一条死链，所以拖拽/粘贴上传后必须等到 `ready` 才写进正文（§10）。
 * 超过 `timeoutMs` 或转码失败都抛错，让调用方提示用户去媒体库确认，而不是静默插死链。
 */
export async function waitForMediaReady(
  id: number,
  options: WaitForMediaReadyOptions = {},
): Promise<MediaItem> {
  const {
    timeoutMs = 30_000,
    intervalMs = 1_500,
    sleep = (ms: number) => new Promise<void>((resolve) => setTimeout(resolve, ms)),
    now = () => Date.now(),
    find = findMediaItem,
  } = options;

  // 双保险：假时钟不前进时靠次数上限退出，避免测试或异常环境下空转。
  const deadline = now() + timeoutMs;
  const maxAttempts = Math.max(1, Math.ceil(timeoutMs / intervalMs) + 1);

  for (let attempt = 0; attempt < maxAttempts; attempt += 1) {
    const item = await find(id);
    if (item?.status === "ready") {
      return item;
    }
    if (item?.status === "failed") {
      throw new Error(
        item.error ? `图片转码失败：${item.error}` : "图片转码失败，请重新上传",
      );
    }
    if (now() >= deadline || attempt + 1 >= maxAttempts) {
      break;
    }
    await sleep(intervalMs);
  }

  throw new Error("图片仍在转码，请稍后在媒体库里确认");
}

/** 媒体库按创建时间倒序，刚上传的那张一定在第一页。 */
async function findMediaItem(id: number): Promise<MediaItem | null> {
  const page = await adminMediaApi.list({ page: 1, pageSize: 20 });
  return page.list.find((item) => item.id === id) ?? null;
}
