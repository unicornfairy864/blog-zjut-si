import type { Metadata } from "next";

import { SITE } from "@/lib/constants/site";

/**
 * SEO 的纯函数层（agent.md §11「SEO」/ §12）。
 *
 * route handler 与页面只负责取数，XML / JSON-LD / OG 的拼装全在这里：
 * 不依赖 React、不发网络请求，便于单测覆盖（tests/seo.test.ts）。
 */

/** sitemap 的变更频率取值（sitemaps.org 协议）。 */
export type ChangeFreq =
  | "always"
  | "hourly"
  | "daily"
  | "weekly"
  | "monthly"
  | "yearly"
  | "never";

/** sitemap 里的一条 URL。 */
export interface SitemapEntry {
  /** 站内路径（如 `/posts/x`）；已经是绝对地址时原样使用。 */
  path: string;
  /** 最后修改时间（Unix 秒）；缺省时整条不输出 `<lastmod>`。 */
  lastmod?: number;
  changefreq?: ChangeFreq;
  priority?: number;
}

/** sitemap 的文章入参；`status` 只是防御性字段（公开 API 不返回状态）。 */
export interface SitemapPostInput {
  slug: string;
  updatedAt: number;
  status?: string;
}

/** sitemap 的静态页入参（`GET /api/pages`）。 */
export interface SitemapPageInput {
  slug: string;
  updatedAt: number;
}

/** sitemap 的分类入参（`GET /api/tags` / `GET /api/series`）。 */
export interface SitemapTaxonomyInput {
  slug: string;
  count: number;
}

/** 只需 `url` 与 `variants` 的最小封面形状，避免 lib/seo 依赖 API 层。 */
export interface SeoCover {
  url: string;
  variants: Record<string, string>;
}

/** 拼绝对地址：站内路径补 `SITE.url`，已是 http(s) 的原样返回。 */
export function absoluteUrl(path: string): string {
  if (/^https?:\/\//i.test(path)) {
    return path;
  }
  return `${SITE.url}${path.startsWith("/") ? path : `/${path}`}`;
}

/** XML 文本转义；`&` 必须最先替换，否则会把后面替换出来的实体再转一次。 */
export function xmlEscape(value: string): string {
  return value
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;")
    .replace(/'/g, "&apos;");
}

/**
 * 包成 CDATA 段。
 *
 * 正文里出现 `]]>` 会提前闭合 CDATA，必须拆成 `]]]]><![CDATA[>`（两个相邻的段）。
 */
export function cdata(value: string): string {
  return `<![CDATA[${value.replace(/]]>/g, "]]]]><![CDATA[>")}]]>`;
}

const WEEKDAYS = ["Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"] as const;
const MONTHS = [
  "Jan",
  "Feb",
  "Mar",
  "Apr",
  "May",
  "Jun",
  "Jul",
  "Aug",
  "Sep",
  "Oct",
  "Nov",
  "Dec",
] as const;

function pad2(value: number): string {
  return String(value).padStart(2, "0");
}

/** Unix 秒 → RFC 822 / RFC 1123 的 GMT 时间串（RSS 的 `<pubDate>` 与 `<lastBuildDate>`）。 */
export function rfc822(unixSeconds: number): string {
  const date = new Date(unixSeconds * 1000);
  const weekday = WEEKDAYS[date.getUTCDay()];
  const month = MONTHS[date.getUTCMonth()];
  const time = `${pad2(date.getUTCHours())}:${pad2(date.getUTCMinutes())}:${pad2(date.getUTCSeconds())}`;
  return `${weekday}, ${pad2(date.getUTCDate())} ${month} ${date.getUTCFullYear()} ${time} GMT`;
}

/** Unix 秒 → 秒级 ISO 8601（`2023-11-14T22:13:20Z`）；sitemap 与 JSON-LD 共用。 */
export function isoDate(unixSeconds: number): string {
  return new Date(unixSeconds * 1000).toISOString().replace(/\.\d{3}Z$/, "Z");
}

/** 常见实体的最小还原表（服务端渲染出来的正文只用得到这些）。 */
const ENTITIES: Record<string, string> = {
  "&nbsp;": " ",
  "&amp;": "&",
  "&lt;": "<",
  "&gt;": ">",
  "&quot;": '"',
  "&#39;": "'",
  "&apos;": "'",
};

/** 去掉标签与常见实体、压平空白，得到纯文本（摘要与 RSS 描述用）。 */
export function stripHtml(html: string): string {
  return html
    .replace(/<[^>]*>/g, " ")
    .replace(/&(?:nbsp|amp|lt|gt|quot|#39|apos);/g, (match) => ENTITIES[match] ?? " ")
    .replace(/\s+/g, " ")
    .trim();
}

/**
 * 正文前 `limit` 个字符，作为没有 `seoDescription` / `excerpt` 时的兜底描述
 * （agent.md §11「摘要：未填 excerpt 时取正文前 160 字纯文本」）。
 */
export function excerptFromHtml(html: string, limit = 160): string {
  const text = stripHtml(html);
  return text.length > limit ? text.slice(0, limit) : text;
}

/**
 * 封面取 1600 档（agent.md §10 的四档变体）。
 *
 * 变体还没生成时 `variants` 为空对象，此时用后端给好的 `url`（就是 1600 的预期地址）。
 */
export function coverImageUrl(
  cover: SeoCover | null | undefined,
  options: { absolute?: boolean } = {},
): string | null {
  if (!cover) {
    return null;
  }
  const path = (cover.variants ?? {})["1600"] || cover.url;
  if (!path) {
    return null;
  }
  return options.absolute ? absoluteUrl(path) : path;
}

/** 固定页的 `<lastmod>`：取全站最近一次内容更新；一篇内容都没有时用 `fallback`。 */
export function latestUpdate(timestamps: number[], fallback: number): number {
  let latest = 0;
  for (const timestamp of timestamps) {
    if (timestamp > latest) {
      latest = timestamp;
    }
  }
  return latest > 0 ? latest : fallback;
}

/**
 * 只保留已发布内容（agent.md §11「草稿隔离」）。
 *
 * 公开 API 本身只返回 published，这里再断言一次：带 `status` 的条目必须是
 * `published`，不带 `status` 的（公开接口的形状）原样通过。
 */
export function publishedOnly<T extends { status?: string }>(items: T[]): T[] {
  return items.filter(
    (item) => item.status === undefined || item.status === "published",
  );
}

/** 文章 → sitemap 条目；`/posts/{slug}` 的 `<lastmod>` 用 `updatedAt`。 */
export function postSitemapEntries(posts: SitemapPostInput[]): SitemapEntry[] {
  return publishedOnly(posts).map((post) => ({
    path: `/posts/${post.slug}`,
    lastmod: post.updatedAt,
    changefreq: "monthly" as const,
    priority: 0.8,
  }));
}

/** 静态页 → sitemap 条目（`pages` 表承载的 `/about` 等）。 */
export function pageSitemapEntries(pages: SitemapPageInput[]): SitemapEntry[] {
  return pages.map((page) => ({
    path: `/${page.slug}`,
    lastmod: page.updatedAt,
    changefreq: "monthly" as const,
    priority: 0.5,
  }));
}

/** 分类 → sitemap 条目；`count` 为 0 的不收录，避免把空列表页交给爬虫。 */
export function taxonomySitemapEntries(
  kind: "tags" | "series",
  items: SitemapTaxonomyInput[],
): SitemapEntry[] {
  return items
    .filter((item) => item.count > 0)
    .map((item) => ({
      path: `/${kind}/${item.slug}`,
      changefreq: "weekly" as const,
      priority: 0.4,
    }));
}

/** 固定页清单；`lastmod` 用全站最近一次内容更新时间。 */
export function fixedSitemapEntries(lastmod: number): SitemapEntry[] {
  return [
    { path: "/", lastmod, changefreq: "daily", priority: 1 },
    { path: "/posts", lastmod, changefreq: "daily", priority: 0.9 },
    { path: "/archive", lastmod, changefreq: "weekly", priority: 0.7 },
    { path: "/tags", lastmod, changefreq: "weekly", priority: 0.6 },
    { path: "/series", lastmod, changefreq: "weekly", priority: 0.6 },
    { path: "/links", lastmod, changefreq: "monthly", priority: 0.5 },
  ];
}

/** 拼 sitemap.xml（sitemaps.org 0.9 协议）；只做序列化，过滤由调用方负责。 */
export function buildSitemapXml(entries: SitemapEntry[]): string {
  const lines = [
    '<?xml version="1.0" encoding="UTF-8"?>',
    '<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">',
  ];
  for (const entry of entries) {
    lines.push("  <url>");
    lines.push(`    <loc>${xmlEscape(absoluteUrl(entry.path))}</loc>`);
    if (entry.lastmod) {
      lines.push(`    <lastmod>${isoDate(entry.lastmod)}</lastmod>`);
    }
    if (entry.changefreq) {
      lines.push(`    <changefreq>${entry.changefreq}</changefreq>`);
    }
    if (entry.priority !== undefined) {
      lines.push(`    <priority>${entry.priority.toFixed(1)}</priority>`);
    }
    lines.push("  </url>");
  }
  lines.push("</urlset>", "");
  return lines.join("\n");
}

/** RSS 的一条 `<item>`。 */
export interface RssItemInput {
  title: string;
  /** 绝对地址。 */
  link: string;
  /** 绝对地址；与 `link` 相同，用 `isPermaLink="true"` 声明。 */
  guid: string;
  /** 发布时间（Unix 秒）。 */
  pubDate: number;
  /** 纯文本摘要，输出前会被实体转义。 */
  description: string;
  /** 正文 HTML，进 `<content:encoded>` 的 CDATA 段。 */
  contentHtml: string;
}

/** RSS 频道入参。 */
export interface RssInput {
  title: string;
  link: string;
  description: string;
  language: string;
  /** `atom:link rel="self"` 的地址。 */
  selfLink: string;
  /** 频道构建时间（Unix 秒）。 */
  lastBuildDate: number;
  items: RssItemInput[];
}

/** 拼 RSS 2.0（全文输出，agent.md §11「rss.xml 全文」）。 */
export function buildRssXml(input: RssInput): string {
  const lines = [
    '<?xml version="1.0" encoding="UTF-8"?>',
    '<rss version="2.0" xmlns:atom="http://www.w3.org/2005/Atom" xmlns:content="http://purl.org/rss/1.0/modules/content/">',
    "  <channel>",
    `    <title>${xmlEscape(input.title)}</title>`,
    `    <link>${xmlEscape(input.link)}</link>`,
    `    <description>${xmlEscape(input.description)}</description>`,
    `    <language>${xmlEscape(input.language)}</language>`,
    `    <atom:link href="${xmlEscape(input.selfLink)}" rel="self" type="application/rss+xml"/>`,
    `    <lastBuildDate>${rfc822(input.lastBuildDate)}</lastBuildDate>`,
  ];

  for (const item of input.items) {
    lines.push("    <item>");
    lines.push(`      <title>${xmlEscape(item.title)}</title>`);
    lines.push(`      <link>${xmlEscape(item.link)}</link>`);
    lines.push(
      `      <guid isPermaLink="true">${xmlEscape(item.guid)}</guid>`,
    );
    lines.push(`      <pubDate>${rfc822(item.pubDate)}</pubDate>`);
    lines.push(
      `      <description>${xmlEscape(item.description)}</description>`,
    );
    lines.push(`      <content:encoded>${cdata(item.contentHtml)}</content:encoded>`);
    lines.push("    </item>");
  }

  lines.push("  </channel>", "</rss>", "");
  return lines.join("\n");
}

/** JSON-LD 序列化：`<` 转成 `\u003c`，避免正文里的 `</script>` 提前闭合脚本标签。 */
export function serializeJsonLd(data: unknown): string {
  return JSON.stringify(data).replace(/</g, "\\u003c");
}

/** 文章页 `Article` JSON-LD 的入参。 */
export interface ArticleJsonLdInput {
  /** canonical 的绝对地址。 */
  url: string;
  title: string;
  description: string;
  publishedAt: number | null;
  updatedAt: number;
  /** 封面绝对地址；没有封面时留空。 */
  images?: string[];
  /** 标签名。 */
  keywords?: string[];
  lang?: string;
  wordCount?: number;
  /** 系列名；公开接口暂不返回，缺省即不输出 `articleSection`。 */
  section?: string;
}

/**
 * 文章页的 `Article` 结构化数据（agent.md §11「文章页输出 Article JSON-LD」）。
 *
 * `image`、`datePublished`、`keywords`、`wordCount`、`articleSection` 都只在有值时出现，
 * 避免把空字段喂给爬虫。
 */
export function buildArticleJsonLd(
  input: ArticleJsonLdInput,
): Record<string, unknown> {
  const ld: Record<string, unknown> = {
    "@context": "https://schema.org",
    "@type": "Article",
    headline: input.title,
    description: input.description,
    dateModified: isoDate(input.updatedAt),
    author: { "@type": "Person", name: SITE.author },
    publisher: { "@type": "Organization", name: SITE.name },
    mainEntityOfPage: { "@type": "WebPage", "@id": input.url },
    inLanguage: input.lang || SITE.locale,
  };
  if (input.images && input.images.length > 0) {
    ld.image = input.images;
  }
  if (input.publishedAt) {
    ld.datePublished = isoDate(input.publishedAt);
  }
  if (input.keywords && input.keywords.length > 0) {
    ld.keywords = input.keywords.join(", ");
  }
  if (input.wordCount) {
    ld.wordCount = input.wordCount;
  }
  if (input.section) {
    ld.articleSection = input.section;
  }
  return ld;
}

/** 首页的 `WebSite` 结构化数据；`SearchAction` 指向站内搜索（agent.md §11）。 */
export function buildWebsiteJsonLd(): Record<string, unknown> {
  return {
    "@context": "https://schema.org",
    "@type": "WebSite",
    name: SITE.name,
    url: SITE.url,
    inLanguage: SITE.locale,
    potentialAction: {
      "@type": "SearchAction",
      target: {
        "@type": "EntryPoint",
        urlTemplate: `${SITE.url}/search?q={search_term_string}`,
      },
      "query-input": "required name=search_term_string",
    },
  };
}

/** OG 入参。 */
export interface OpenGraphInput {
  title: string;
  description: string;
  /** 该页 canonical 的绝对地址。 */
  url: string;
  type?: "website" | "article";
  publishedTime?: string;
  modifiedTime?: string;
  tags?: string[];
  images?: string[];
}

/**
 * 拼该页的 `openGraph`。
 *
 * Next 的 metadata 对 `openGraph` 是**整体替换**而不是深合并，所以站点级默认
 * （`siteName` / `locale`）必须由每个页面自己带上，不能只写在根布局里。
 */
export function buildOpenGraph(
  input: OpenGraphInput,
): NonNullable<Metadata["openGraph"]> {
  const shared = {
    siteName: SITE.name,
    locale: SITE.locale,
    title: input.title,
    description: input.description,
    url: input.url,
    images: input.images,
  };
  if (input.type === "article") {
    return {
      ...shared,
      type: "article",
      publishedTime: input.publishedTime,
      modifiedTime: input.modifiedTime,
      tags: input.tags,
    };
  }
  return { ...shared, type: "website" };
}
