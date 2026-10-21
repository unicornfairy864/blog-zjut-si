import type { MetadataRoute } from "next";

import { SITE } from "@/lib/constants/site";

/**
 * 爬虫入口（agent.md §11「SEO」）。
 *
 * 禁 `/admin`、`/preview`、`/api/`；sitemap 用绝对地址（metadataBase 管不到这里）。
 * 本文件不取数，构建期即可生成，因此不需要 `force-dynamic`。
 */
export default function robots(): MetadataRoute.Robots {
  return {
    rules: {
      userAgent: "*",
      allow: "/",
      disallow: ["/admin", "/preview", "/api/"],
    },
    sitemap: `${SITE.url}/sitemap.xml`,
  };
}
