import { apiFetch } from "./fetcher";

/** 磁盘文件类型：原图 / 四档变体 / 附件（agent.md §10）。 */
export type StorageKind = "original" | "variant" | "attachment";

/** 孤儿文件：磁盘上有、库里没有任何记录引用。 */
export interface StorageOrphan {
  /** 相对 upload.dir / file.dir 的路径，如 `ab/cd…/960.webp`。 */
  path: string;
  bytes: number;
  kind: StorageKind;
}

/** 缺失文件：库里有记录、磁盘上没有。 */
export interface StorageMissing {
  kind: StorageKind;
  storageKey: string;
  /** 只有缺失的是图片原图时才有值（服务端 omitempty，0 会被省略）。 */
  mediaId?: number;
  /** 只有缺失的是附件时才有值。 */
  attachmentId?: number;
}

/** GET /api/admin/storage 的 data：占用统计 + 对账清单。 */
export interface StorageOverview {
  uploadDir: string;
  fileDir: string;
  mediaCount: number;
  originalBytes: number;
  variantBytes: number;
  originalsRemoved: number;
  attachmentCount: number;
  attachmentBytes: number;
  /** 本地上传的附件数（storageKey 非空）。 */
  linkedAttachmentCount: number;
  totalBytes: number;
  orphanCount: number;
  orphanBytes: number;
  missingCount: number;
  /** 最多 200 条；`orphanCount` 才是全量计数。 */
  orphans: StorageOrphan[];
  /** 最多 100 条；`missingCount` 才是全量计数。 */
  missing: StorageMissing[];
}

/** POST /api/admin/storage/cleanup 的 data。 */
export interface StorageCleanupResult {
  removedFiles: number;
  removedBytes: number;
  /** 删不掉的文件数（权限等），通常是 0。 */
  failedFiles: number;
}

/** 把磁盘文件类型写成中文，避免界面上出现 original / variant 这类原始值。 */
export function describeStorageKind(kind: string): string {
  switch (kind) {
    case "original":
      return "原图";
    case "variant":
      return "变体";
    case "attachment":
      return "附件";
    default:
      // 后端将来新增类型时不至于让界面变成空白。
      return kind;
  }
}

/** 管理端存储接口（D14）；浏览器侧一律同源调用。 */
export const adminStorageApi = {
  /** 对账磁盘与数据库：占用统计、孤儿文件、缺失文件。 */
  overview() {
    return apiFetch<StorageOverview>("/api/admin/storage");
  },

  /** 删除没有任何记录引用的孤儿文件；返回删掉的文件数与字节数。 */
  cleanup() {
    return apiFetch<StorageCleanupResult>("/api/admin/storage/cleanup", {
      json: {},
    });
  },
};
