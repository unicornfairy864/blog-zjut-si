import { apiFetch } from "./fetcher";

/** 友链状态枚举（agent.md §4）：公开端只出 active。 */
export const LINK_STATUSES = ["active", "hidden"] as const;

export type LinkStatus = (typeof LINK_STATUSES)[number];

/** 跳转状态码枚举：只有 301 / 302 两种（§4 的 CHECK 约束）。 */
export const REDIRECT_STATUSES = [301, 302] as const;

export type RedirectStatus = (typeof REDIRECT_STATUSES)[number];

/**
 * 标签。
 *
 * `count` 是该标签下**未删除**文章数（含草稿），与公开端「只含已发布」的口径不同，
 * 所以这里不能复用公开端的标签类型。
 */
export interface AdminTag {
  id: number;
  name: string;
  slug: string;
  description: string;
  count: number;
  createdAt: number;
}

/** 系列；字段与标签同形，但一篇文章最多属于一个系列（`posts.series_id`）。 */
export interface AdminSeries {
  id: number;
  name: string;
  slug: string;
  description: string;
  count: number;
  createdAt: number;
}

/** 友链；管理端全量返回（含 hidden），公开端只出 active。 */
export interface AdminLink {
  id: number;
  name: string;
  url: string;
  avatar: string;
  description: string;
  sort: number;
  status: LinkStatus;
  createdAt: number;
}

/** 跳转；`hits` 是公开边缘按 `from_path` 命中并跳转的次数。 */
export interface AdminRedirect {
  id: number;
  fromPath: string;
  toPath: string;
  status: RedirectStatus;
  hits: number;
  createdAt: number;
}

/** 新建分类的入参；`slug` 留空表示由服务端按 ASCII 名生成（中文名必须手填）。 */
export interface TaxonomyInput {
  name: string;
  slug: string;
  description: string;
}

/** 局部更新分类；字段均可选。 */
export type TaxonomyUpdateInput = Partial<TaxonomyInput>;

/** 新建友链的入参。 */
export interface LinkInput {
  name: string;
  url: string;
  avatar: string;
  description: string;
  sort: number;
  status: LinkStatus;
}

/** 局部更新友链；字段均可选。 */
export type LinkUpdateInput = Partial<LinkInput>;

/** 新建跳转的入参；`fromPath` 唯一且必须以 `/` 开头。 */
export interface RedirectInput {
  fromPath: string;
  toPath: string;
  status: RedirectStatus;
}

/** 局部更新跳转；字段均可选。 */
export type RedirectUpdateInput = Partial<RedirectInput>;

/** 删除出参。 */
export interface DeletedResult {
  deleted: boolean;
}

/** 标签列表出参；这几类都是全量列表，不分页（api.md「分类与友链（管理端）」）。 */
export interface AdminTagListResult {
  list: AdminTag[];
}

/** 系列列表出参。 */
export interface AdminSeriesListResult {
  list: AdminSeries[];
}

/** 友链列表出参。 */
export interface AdminLinkListResult {
  list: AdminLink[];
}

/** 跳转列表出参。 */
export interface AdminRedirectListResult {
  list: AdminRedirect[];
}

/** 分类（标签 / 系列）与友链、跳转的管理端接口（D14）。 */
export const adminTaxonomyApi = {
  /** 全量标签，带未删除文章数。 */
  tags() {
    return apiFetch<AdminTagListResult>("/api/admin/tags");
  },

  /** 新建标签；slug 冲突抛 2001，name 重复抛 1004。 */
  createTag(input: TaxonomyInput) {
    return apiFetch<AdminTag>("/api/admin/tags", { json: input });
  },

  /** 局部更新标签。 */
  updateTag(id: number, input: TaxonomyUpdateInput) {
    return apiFetch<AdminTag>(`/api/admin/tags/${id}`, {
      method: "PUT",
      json: input,
    });
  },

  /** 删除标签：只解关联，不删文章。 */
  removeTag(id: number) {
    return apiFetch<DeletedResult>(`/api/admin/tags/${id}`, {
      method: "DELETE",
    });
  },

  /** 全量系列，带未删除文章数。 */
  series() {
    return apiFetch<AdminSeriesListResult>("/api/admin/series");
  },

  /** 新建系列。 */
  createSeries(input: TaxonomyInput) {
    return apiFetch<AdminSeries>("/api/admin/series", { json: input });
  },

  /** 局部更新系列。 */
  updateSeries(id: number, input: TaxonomyUpdateInput) {
    return apiFetch<AdminSeries>(`/api/admin/series/${id}`, {
      method: "PUT",
      json: input,
    });
  },

  /** 删除系列：把 `posts.series_id` 置 NULL，不删文章。 */
  removeSeries(id: number) {
    return apiFetch<DeletedResult>(`/api/admin/series/${id}`, {
      method: "DELETE",
    });
  },

  /** 全量友链（含 hidden），按 sort、id 升序。 */
  links() {
    return apiFetch<AdminLinkListResult>("/api/admin/links");
  },

  /** 新建友链。 */
  createLink(input: LinkInput) {
    return apiFetch<AdminLink>("/api/admin/links", { json: input });
  },

  /** 局部更新友链。 */
  updateLink(id: number, input: LinkUpdateInput) {
    return apiFetch<AdminLink>(`/api/admin/links/${id}`, {
      method: "PUT",
      json: input,
    });
  },

  /** 删除友链。 */
  removeLink(id: number) {
    return apiFetch<DeletedResult>(`/api/admin/links/${id}`, {
      method: "DELETE",
    });
  },

  /** 全量跳转，按 id 倒序。 */
  redirects() {
    return apiFetch<AdminRedirectListResult>("/api/admin/redirects");
  },

  /** 新建跳转；fromPath 重复抛 1004。 */
  createRedirect(input: RedirectInput) {
    return apiFetch<AdminRedirect>("/api/admin/redirects", { json: input });
  },

  /** 局部更新跳转。 */
  updateRedirect(id: number, input: RedirectUpdateInput) {
    return apiFetch<AdminRedirect>(`/api/admin/redirects/${id}`, {
      method: "PUT",
      json: input,
    });
  },

  /** 删除跳转。 */
  removeRedirect(id: number) {
    return apiFetch<DeletedResult>(`/api/admin/redirects/${id}`, {
      method: "DELETE",
    });
  },
};
