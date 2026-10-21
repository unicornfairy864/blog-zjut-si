import { apiFetch } from "./fetcher";

/** 附件（agent.md §4 post_attachments）；公开访问走 /api/attachments/{id}/go 302 跳转并计点击。 */
export interface AdminAttachment {
  id: number;
  postId: number;
  title: string;
  shareUrl: string;
  fileName: string;
  bytes: number;
  sha256: string;
  version: string;
  sort: number;
  clicks: number;
  /** 非空表示本地上传（文件在 file.dir 下），空串表示外链附件。 */
  storageKey: string;
  /** 文件头嗅探出的 MIME，只作展示与排障；下载时服务端一律发 octet-stream。 */
  mime: string;
  createdAt: number;
}

/** 附件分页信封（agent.md §5 分页出参）。 */
export interface AdminAttachmentListResult {
  list: AdminAttachment[];
  total: number;
  page: number;
  pageSize: number;
}

/**
 * 新建外链附件入参；postId / title / shareUrl 必填，shareUrl 必须是 http(s)。
 *
 * 本地文件不走这里：POST /api/admin/attachments 只登记外链，shareUrl 为空会被 400 拒绝，
 * 上传本地附件请用 `adminAttachmentApi.upload`。
 */
export interface AttachmentCreateInput {
  postId: number;
  title: string;
  shareUrl: string;
  fileName?: string;
  bytes?: number;
  sha256?: string;
  version?: string;
  sort?: number;
}

/** 上传本地附件入参（multipart/form-data）。 */
export interface AttachmentUploadInput {
  file: File;
  postId: number;
  /** 留空时服务端取去掉扩展名的文件名。 */
  title?: string;
  version?: string;
  sort?: number;
}

/** 更新附件入参；只提交要改的字段，未提交的字段服务端保持原值。 */
export type AttachmentUpdateInput = Partial<AttachmentCreateInput>;

/** 任务状态（agent.md §4 jobs.status）。 */
export const JOB_STATUSES = ["pending", "running", "done", "failed"] as const;

export type JobStatus = (typeof JOB_STATUSES)[number];

/** 手动投递只允许这四类（D13/D17：留给运维与自测，其余类型由调度器投递）。 */
export const MANUAL_JOB_TYPES = [
  "cleanup",
  "backup",
  "link_check",
  "integrity_check",
] as const;

export type ManualJobType = (typeof MANUAL_JOB_TYPES)[number];

/**
 * 任务；`payload` 与 `lastError` 都是原样回显的字符串（api.md「任务与审计」）。
 *
 * `locked_at` 超 10 分钟会被调度器放回 pending（agent.md §6），所以 `lockedAt` 只在执行中才有值。
 */
export interface AdminJob {
  id: number;
  type: string;
  payload: string;
  runAt: number;
  status: JobStatus;
  attempts: number;
  maxAttempts: number;
  lastError: string;
  lockedAt: number | null;
  createdAt: number;
}

/** 任务分页信封。 */
export interface AdminJobListResult {
  list: AdminJob[];
  total: number;
  page: number;
  pageSize: number;
}

/** 审计日志；`meta` 库里是 JSON 字符串，接口以对象回显，可能为空（D14）。 */
export interface AdminAuditLog {
  id: number;
  adminId: number;
  action: string;
  targetType: string;
  targetId: string;
  meta: Record<string, unknown> | null;
  ipHash: string;
  createdAt: number;
}

/** 审计日志分页信封。 */
export interface AdminAuditLogListResult {
  list: AdminAuditLog[];
  total: number;
  page: number;
  pageSize: number;
}

/** 审计筛选入参；三项都留空表示不过滤（api.md「任务与审计」）。 */
export interface AuditLogQuery {
  action?: string;
  targetType?: string;
  targetId?: string;
  page?: number;
  pageSize?: number;
}

/**
 * 附件的公开地址（相对路径，管理端与公开端同源）。
 *
 * 本地附件由本站直接下发文件，外链附件 302 跳到 shareUrl；两种都计一次点击。
 * 组件里禁止拼 API 路径（agent.md §8），所以地址形态收在这里，界面只负责展示与复制。
 */
export function attachmentGoPath(id: number): string {
  return `/api/attachments/${id}/go`;
}

/** 管理端附件接口（D14）。 */
export const adminAttachmentApi = {
  /** 附件分页；`postId` 不传表示全部，服务端按 sort、id 升序。 */
  list(query: { postId?: number; page?: number; pageSize?: number } = {}) {
    const params = new URLSearchParams();
    if (query.postId) {
      params.set("postId", String(query.postId));
    }
    if (query.page) {
      params.set("page", String(query.page));
    }
    if (query.pageSize) {
      params.set("pageSize", String(query.pageSize));
    }
    const queryString = params.toString();
    return apiFetch<AdminAttachmentListResult>(
      `/api/admin/attachments${queryString ? `?${queryString}` : ""}`,
    );
  },

  /** 新建外链附件；shareUrl 为空会被服务端 400 拒绝（本地附件走 upload）。 */
  create(input: AttachmentCreateInput) {
    return apiFetch<AdminAttachment>("/api/admin/attachments", { json: input });
  },

  /**
   * 上传本地附件（multipart/form-data）：文件存进 file.dir，记录带 storage_key 与 mime。
   *
   * 失败语义（信封的 msg 直接给用户看即可；v1.14 起失败也是 HTTP 200，只看 code）：
   * - `code 1003` 参数问题（缺 file / postId 非正整数）；
   * - `code 2007` 扩展名或文件名被安全黑名单拒绝（如 .php / .html / .exe / .svg、隐藏文件）；
   * - `code 2008` 超过 `file.maxBytes` 上限。
   */
  upload(input: AttachmentUploadInput) {
    const form = new FormData();
    form.append("postId", String(input.postId));
    if (input.title) {
      form.append("title", input.title);
    }
    if (input.version) {
      form.append("version", input.version);
    }
    if (input.sort !== undefined) {
      form.append("sort", String(input.sort));
    }
    form.append("file", input.file);
    return apiFetch<AdminAttachment>("/api/admin/attachments/upload", {
      method: "POST",
      body: form,
      timeoutMs: 120_000,
    });
  },

  /** 更新附件；校验失败抛 ApiError(1003)，shareUrl 非 http(s) 同理。 */
  update(id: number, input: AttachmentUpdateInput) {
    return apiFetch<AdminAttachment>(`/api/admin/attachments/${id}`, {
      method: "PUT",
      json: input,
    });
  },

  /** 删除附件；记录没了，但已经发出去的 /go 地址会变成 code 1002。 */
  remove(id: number) {
    return apiFetch<null>(`/api/admin/attachments/${id}`, { method: "DELETE" });
  },
};

/** 管理端任务接口（D13）。 */
export const adminJobApi = {
  /** 任务分页；`status` 为空表示全部，服务端按 run_at 倒序。 */
  list(query: { status?: JobStatus | ""; page?: number; pageSize?: number } = {}) {
    const params = new URLSearchParams();
    if (query.status) {
      params.set("status", query.status);
    }
    if (query.page) {
      params.set("page", String(query.page));
    }
    if (query.pageSize) {
      params.set("pageSize", String(query.pageSize));
    }
    const queryString = params.toString();
    return apiFetch<AdminJobListResult>(
      `/api/admin/jobs${queryString ? `?${queryString}` : ""}`,
    );
  },

  /** 手动投递；立即入队，不等周期调度。 */
  enqueue(type: ManualJobType) {
    return apiFetch<AdminJob>("/api/admin/jobs", { json: { type } });
  },

  /** 把 failed 任务放回 pending 并立刻到期；非 failed → code 1004。 */
  retry(id: number) {
    return apiFetch<AdminJob>(`/api/admin/jobs/${id}/retry`, { json: {} });
  },
};

/** 管理端审计接口（D14）；adminId / ipHash 只在这里出现。 */
export const adminAuditApi = {
  /** 审计日志分页，按创建时间倒序。 */
  list(query: AuditLogQuery = {}) {
    const params = new URLSearchParams();
    if (query.action) {
      params.set("action", query.action);
    }
    if (query.targetType) {
      params.set("targetType", query.targetType);
    }
    if (query.targetId) {
      params.set("targetId", query.targetId);
    }
    if (query.page) {
      params.set("page", String(query.page));
    }
    if (query.pageSize) {
      params.set("pageSize", String(query.pageSize));
    }
    const queryString = params.toString();
    return apiFetch<AdminAuditLogListResult>(
      `/api/admin/audit-logs${queryString ? `?${queryString}` : ""}`,
    );
  },
};
