import { apiFetch } from "./fetcher";

/** 评论状态枚举（agent.md §4）。 */
export const COMMENT_STATUSES = [
  "pending",
  "approved",
  "spam",
  "deleted",
] as const;

export type CommentStatus = (typeof COMMENT_STATUSES)[number];

/** 管理端评论条目；含邮箱（仅管理员可见），不含 ip_hash / ua。 */
export interface AdminCommentItem {
  id: number;
  postId: number;
  postSlug: string;
  postTitle: string;
  parentId: number | null;
  authorName: string;
  authorEmail: string;
  authorUrl: string;
  contentMd: string;
  contentHtml: string;
  status: string;
  createdAt: number;
}

/** 管理端评论分页。 */
export interface AdminCommentListResult {
  list: AdminCommentItem[];
  total: number;
  page: number;
  pageSize: number;
}

/** 管理端评论接口（D11）。 */
export const adminCommentApi = {
  /** 审核队列；status 传 all 看全部。 */
  list(
    query: { status?: string; page?: number; pageSize?: number } = {},
  ) {
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
    return apiFetch<AdminCommentListResult>(
      `/api/admin/comments${queryString ? `?${queryString}` : ""}`,
    );
  },

  /** 单条改状态：approved | spam | deleted。 */
  patch(id: number, status: CommentStatus) {
    return apiFetch<AdminCommentItem>("/api/admin/comments", {
      method: "PATCH",
      json: { id, status },
    });
  },

  /** 批量改状态。 */
  bulk(ids: number[], status: CommentStatus) {
    return apiFetch<{ updated: number }>("/api/admin/comments/bulk", {
      json: { ids, status },
    });
  },
};
