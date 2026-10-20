import { apiFetch } from "./fetcher";

/** 上传配置（只读回显）：目录与单文件字节上限。 */
export interface AdminSettingsUpload {
  dir: string;
  maxBytes: number;
}

/** 任务调度配置（只读回显）。 */
export interface AdminSettingsTask {
  enabled: boolean;
  intervalSeconds: number;
}

/** 附件存储配置（只读回显）：目录与单文件字节上限。 */
export interface AdminSettingsFile {
  dir: string;
  maxBytes: number;
}

/** 数据库配置（只读回显）。 */
export interface AdminSettingsDb {
  path: string;
}

/**
 * GET /api/admin/settings 的 data。
 *
 * 只有 `imgBaseUrl` 与 `siteTimezone` 可写，其余字段是运维排查用的只读回显。
 * 图片与附件都由本站自己托管，各有一份目录与上限。
 */
export interface AdminSettings {
  imgBaseUrl: string;
  siteTimezone: string;
  contentVersion: number;
  upload: AdminSettingsUpload;
  file: AdminSettingsFile;
  task: AdminSettingsTask;
  db: AdminSettingsDb;
}

/** PUT /api/admin/settings 的入参；写成功服务端会 `content_version+1`。 */
export interface AdminSettingsInput {
  imgBaseUrl: string;
  siteTimezone: string;
}

/** 站点设置接口（D14）。 */
export const adminSettingsApi = {
  /** 读当前设置；不含任何口令。 */
  get() {
    return apiFetch<AdminSettings>("/api/admin/settings");
  },

  /**
   * 写可写项。
   *
   * `imgBaseUrl` 留空表示用相对路径 `/uploads/…`；`siteTimezone` 必须是 IANA 名，
   * 非法时服务端返回 `400 / 1003`。成功后公开读的 ETag 依赖的 `content_version` 会自增。
   */
  update(input: AdminSettingsInput) {
    return apiFetch<AdminSettings>("/api/admin/settings", {
      method: "PUT",
      json: input,
    });
  },
};
