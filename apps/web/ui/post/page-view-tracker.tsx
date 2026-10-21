"use client";

import { usePathname } from "next/navigation";
import { useEffect } from "react";

import { publicApi } from "@/lib/api/public";

/** 文章详情页由 `PostInteractions` 打点（带 slug，能落到文章行）；这里跳过，避免重复。 */
const POST_DETAIL_PREFIX = "/posts/";

/**
 * 非文章页的 PV 打点（agent.md §5「PV 明细」）。
 *
 * 服务端支持"非文章页"打点（`post_id` 为 NULL、去重键用 `path`），但此前只有文章详情页在打点，
 * 于是管理端的"全站 PV/UV"实际只统计文章页。这个组件挂在公开布局里补齐首页/列表/标签/归档等页面。
 *
 * 失败一律静默：统计不该影响阅读；开发环境 StrictMode 会跑两次，服务端 10 分钟去重会挡掉。
 */
export function PageViewTracker() {
  const pathname = usePathname();

  useEffect(() => {
    if (!pathname || pathname.startsWith(POST_DETAIL_PREFIX)) {
      return;
    }
    void publicApi
      .recordView({ path: pathname, referrer: document.referrer || undefined })
      .catch(() => undefined);
  }, [pathname]);

  return null;
}
