import { apiFetch } from "./fetcher";

/** 文章状态计数；`deleted` 是回收站，其余与 posts.status 一致（agent.md §4）。 */
export interface AdminPostCounts {
  published: number;
  draft: number;
  scheduled: number;
  archived: number;
  deleted: number;
}

/** 评论状态计数；只有 approved 会出现在公开页面。 */
export interface AdminCommentCounts {
  pending: number;
  approved: number;
  spam: number;
}

/** 媒体处理状态计数。 */
export interface AdminMediaCounts {
  ready: number;
  processing: number;
  failed: number;
}

/** 阅读计数；`uvToday` 是今日去重访客数（按上海日期重算，不做增量近似）。 */
export interface AdminViewCounts {
  total: number;
  today: number;
  uvToday: number;
}

/** 点赞计数。 */
export interface AdminLikeCounts {
  total: number;
  today: number;
}

/** 看板 Top 榜行。 */
export interface AdminTopPost {
  id: number;
  slug: string;
  title: string;
  viewCount: number;
  likeCount: number;
}

/** GET /api/admin/stats/overview 的 data。 */
export interface AdminStatsOverview {
  posts: AdminPostCounts;
  comments: AdminCommentCounts;
  media: AdminMediaCounts;
  views: AdminViewCounts;
  likes: AdminLikeCounts;
  topPosts: AdminTopPost[];
}

/** 日粒度的一行；date 是上海日期 YYYY-MM-DD。 */
export interface AdminStatsDayRow {
  date: string;
  pv: number;
  uv: number;
}

/**
 * GET /api/admin/stats/daily 的 data。
 *
 * 没有数据的日期不补零行（api.md「统计」），补空点由前端按区间做。
 */
export interface AdminStatsDaily {
  from: string;
  to: string;
  totalPv: number;
  totalUv: number;
  list: AdminStatsDayRow[];
}

/** 管理端统计接口；浏览器侧一律同源调用（agent.md §8）。 */
export const adminStatsApi = {
  /** 看板：状态计数 + 今日/累计 PV·UV + 点赞 + Top 5 文章。 */
  overview() {
    return apiFetch<AdminStatsOverview>("/api/admin/stats/overview");
  },

  /** 全站日粒度；from / to 缺省时服务端给最近 30 天，区间上限 366 天。 */
  daily(params: { from?: string; to?: string } = {}) {
    const search = new URLSearchParams();
    if (params.from) {
      search.set("from", params.from);
    }
    if (params.to) {
      search.set("to", params.to);
    }
    const queryString = search.toString();
    return apiFetch<AdminStatsDaily>(
      `/api/admin/stats/daily${queryString ? `?${queryString}` : ""}`,
    );
  },
};
