import { describe, expect, it } from "vitest";

import {
  absoluteUrl,
  buildArticleJsonLd,
  buildRssXml,
  buildSitemapXml,
  cdata,
  coverImageUrl,
  excerptFromHtml,
  fixedSitemapEntries,
  isoDate,
  latestUpdate,
  postSitemapEntries,
  publishedOnly,
  rfc822,
  serializeJsonLd,
  taxonomySitemapEntries,
  xmlEscape,
} from "@/lib/seo";

/**
 * SEO 纯函数（agent.md §11「SEO」）。
 *
 * route handler 只负责取数，这里覆盖拼装与过滤：XML 转义 / CDATA 拆分 /
 * 时间格式 / 绝对地址 / JSON-LD 字段 / sitemap 只出已发布。
 */

describe("xmlEscape", () => {
  it("转义 XML 的五个特殊字符", () => {
    expect(xmlEscape(`&<>"'`)).toBe("&amp;&lt;&gt;&quot;&apos;");
  });

  it("先转义 & 再转义其它字符，不会二次转义", () => {
    expect(xmlEscape("A & B <tag>")).toBe("A &amp; B &lt;tag&gt;");
  });

  it("普通文本原样返回", () => {
    expect(xmlEscape("中文与 English 123")).toBe("中文与 English 123");
  });
});

describe("cdata", () => {
  it("包成 CDATA 段", () => {
    expect(cdata("<p>正文</p>")).toBe("<![CDATA[<p>正文</p>]]>");
  });

  it("把内容里的 ]]> 拆成两段，避免提前闭合", () => {
    expect(cdata("a]]>b")).toBe("<![CDATA[a]]]]><![CDATA[>b]]>");
  });

  it("拆出来的片段拼回去仍是原内容", () => {
    const raw = "x]]>y";
    const wrapped = cdata(raw);
    const inner = wrapped
      .slice("<![CDATA[".length, -"]]>".length)
      .split("]]]]><![CDATA[>")
      .join("]]>");
    expect(inner).toBe(raw);
  });
});

describe("rfc822", () => {
  it("固定时间戳 → 固定的 GMT 串", () => {
    // 1700000000 = 2023-11-14T22:13:20Z
    expect(rfc822(1700000000)).toBe("Tue, 14 Nov 2023 22:13:20 GMT");
  });

  it("个位数的日与时都补零", () => {
    // 1704067200 = 2024-01-01T00:00:00Z
    expect(rfc822(1704067200)).toBe("Mon, 01 Jan 2024 00:00:00 GMT");
  });
});

describe("isoDate", () => {
  it("秒级 ISO 8601，不带毫秒", () => {
    expect(isoDate(1700000000)).toBe("2023-11-14T22:13:20Z");
  });
});

describe("absoluteUrl", () => {
  it("站内路径补站点地址", () => {
    expect(absoluteUrl("/posts/hello")).toBe(
      "https://blog.zjut.si/posts/hello",
    );
  });

  it("缺少前导斜杠时自动补上", () => {
    expect(absoluteUrl("about")).toBe("https://blog.zjut.si/about");
  });

  it("已是绝对地址时原样返回", () => {
    expect(absoluteUrl("https://example.com/a")).toBe("https://example.com/a");
  });
});

describe("stripHtml / excerptFromHtml", () => {
  it("去标签、还原常见实体并压平空白", () => {
    expect(excerptFromHtml("<p>a &amp; b</p>\n<p>c</p>")).toBe("a & b c");
  });

  it("超过上限时按字符截断", () => {
    expect(excerptFromHtml("<p>123456</p>", 3)).toBe("123");
  });
});

describe("coverImageUrl", () => {
  const cover = {
    url: "/uploads/aa/bb/1600.webp",
    variants: { "480": "/uploads/aa/bb/480.webp", "1600": "/uploads/aa/bb/1600.webp" },
  };

  it("取 1600 档", () => {
    expect(coverImageUrl(cover)).toBe("/uploads/aa/bb/1600.webp");
  });

  it("需要绝对地址时补站点地址", () => {
    expect(coverImageUrl(cover, { absolute: true })).toBe(
      "https://blog.zjut.si/uploads/aa/bb/1600.webp",
    );
  });

  it("变体缺失时退回 url，没有封面时返回 null", () => {
    expect(coverImageUrl({ url: "/uploads/aa/bb/1600.webp", variants: {} })).toBe(
      "/uploads/aa/bb/1600.webp",
    );
    expect(coverImageUrl(null)).toBeNull();
  });
});

describe("latestUpdate", () => {
  it("取最大值，空集合用兜底值", () => {
    expect(latestUpdate([10, 30, 20], 99)).toBe(30);
    expect(latestUpdate([], 99)).toBe(99);
  });
});

describe("publishedOnly / postSitemapEntries", () => {
  const posts = [
    { slug: "published-a", updatedAt: 1700000000, status: "published" },
    { slug: "draft-b", updatedAt: 1700000001, status: "draft" },
    { slug: "scheduled-c", updatedAt: 1700000002, status: "scheduled" },
    { slug: "archived-d", updatedAt: 1700000003, status: "archived" },
    // 公开 API 不返回 status，这种形状必须原样通过。
    { slug: "published-e", updatedAt: 1700000004 },
  ];

  it("只留下已发布（带 status 的必须等于 published）", () => {
    expect(publishedOnly(posts).map((post) => post.slug)).toEqual([
      "published-a",
      "published-e",
    ]);
  });

  it("草稿 / 定时 / 归档的 slug 不会出现在 sitemap 输出里", () => {
    const xml = buildSitemapXml(postSitemapEntries(posts));
    expect(xml).toContain(
      "<loc>https://blog.zjut.si/posts/published-a</loc>",
    );
    expect(xml).toContain(
      "<loc>https://blog.zjut.si/posts/published-e</loc>",
    );
    for (const slug of ["draft-b", "scheduled-c", "archived-d"]) {
      expect(xml).not.toContain(slug);
    }
  });

  it("文章的 lastmod 用 updatedAt 的秒级 ISO 8601", () => {
    const xml = buildSitemapXml(postSitemapEntries(posts));
    expect(xml).toContain("<lastmod>2023-11-14T22:13:20Z</lastmod>");
  });
});

describe("taxonomySitemapEntries", () => {
  it("count 为 0 的标签 / 系列不收录", () => {
    const entries = taxonomySitemapEntries("series", [
      { slug: "has-posts", count: 3 },
      { slug: "empty", count: 0 },
    ]);
    expect(entries.map((entry) => entry.path)).toEqual(["/series/has-posts"]);
  });
});

describe("buildSitemapXml", () => {
  it("输出 urlset 命名空间、固定页与 XML 声明", () => {
    const xml = buildSitemapXml(fixedSitemapEntries(1700000000));
    expect(xml.startsWith('<?xml version="1.0" encoding="UTF-8"?>')).toBe(true);
    expect(xml).toContain(
      '<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">',
    );
    expect(xml).toContain("<loc>https://blog.zjut.si/</loc>");
    expect(xml).toContain("<loc>https://blog.zjut.si/posts</loc>");
    expect(xml).toContain("<loc>https://blog.zjut.si/archive</loc>");
    expect(xml).toContain("<loc>https://blog.zjut.si/tags</loc>");
    expect(xml).toContain("<loc>https://blog.zjut.si/series</loc>");
    expect(xml).toContain("<loc>https://blog.zjut.si/links</loc>");
    expect(xml).toContain("<priority>1.0</priority>");
  });
});

describe("buildRssXml", () => {
  const xml = buildRssXml({
    title: "zjut.si",
    link: "https://blog.zjut.si",
    description: "站点描述 & 说明",
    language: "zh-CN",
    selfLink: "https://blog.zjut.si/rss.xml",
    lastBuildDate: 1700000000,
    items: [
      {
        title: "标题 <A>",
        link: "https://blog.zjut.si/posts/a",
        guid: "https://blog.zjut.si/posts/a",
        pubDate: 1700000000,
        description: "摘要 & 实体",
        contentHtml: "<p>正文]]>结束</p>",
      },
    ],
  });

  it("声明 rss 2.0 与 atom / content 命名空间", () => {
    expect(xml).toContain('<rss version="2.0"');
    expect(xml).toContain('xmlns:atom="http://www.w3.org/2005/Atom"');
    expect(xml).toContain(
      'xmlns:content="http://purl.org/rss/1.0/modules/content/"',
    );
    expect(xml).toContain(
      '<atom:link href="https://blog.zjut.si/rss.xml" rel="self" type="application/rss+xml"/>',
    );
  });

  it("频道字段齐备且实体转义", () => {
    expect(xml).toContain("<language>zh-CN</language>");
    expect(xml).toContain("<description>站点描述 &amp; 说明</description>");
    expect(xml).toContain(
      "<lastBuildDate>Tue, 14 Nov 2023 22:13:20 GMT</lastBuildDate>",
    );
  });

  it("条目带 guid / pubDate，正文进 CDATA 且 ]]> 被拆开", () => {
    expect(xml).toContain("<title>标题 &lt;A&gt;</title>");
    expect(xml).toContain(
      '<guid isPermaLink="true">https://blog.zjut.si/posts/a</guid>',
    );
    expect(xml).toContain(
      "<pubDate>Tue, 14 Nov 2023 22:13:20 GMT</pubDate>",
    );
    expect(xml).toContain("<description>摘要 &amp; 实体</description>");
    expect(xml).toContain(
      "<content:encoded><![CDATA[<p>正文]]]]><![CDATA[>结束</p>]]></content:encoded>",
    );
  });
});

describe("serializeJsonLd", () => {
  it("把 < 转义成 \\u003c，防止提前闭合 script", () => {
    const serialized = serializeJsonLd({ headline: "</script><b>" });
    expect(serialized).not.toContain("<");
    expect(serialized).toContain("\\u003c");
    expect(JSON.parse(serialized)).toEqual({ headline: "</script><b>" });
  });
});

describe("buildArticleJsonLd", () => {
  const ld = buildArticleJsonLd({
    url: "https://blog.zjut.si/posts/a",
    title: "标题",
    description: "描述",
    publishedAt: 1700000000,
    updatedAt: 1700003600,
    images: ["https://blog.zjut.si/uploads/aa/bb/1600.webp"],
    keywords: ["Go", "SQLite"],
    lang: "zh-CN",
    wordCount: 1200,
  });

  it("关键字段符合 Article 契约", () => {
    expect(ld["@context"]).toBe("https://schema.org");
    expect(ld["@type"]).toBe("Article");
    expect(ld.headline).toBe("标题");
    expect(ld.description).toBe("描述");
    expect(ld.datePublished).toBe("2023-11-14T22:13:20Z");
    expect(ld.dateModified).toBe("2023-11-14T23:13:20Z");
    expect(ld.author).toEqual({ "@type": "Person", name: "unicornfairy864" });
    expect(ld.publisher).toEqual({ "@type": "Organization", name: "zjut.si" });
    expect(ld.mainEntityOfPage).toEqual({
      "@type": "WebPage",
      "@id": "https://blog.zjut.si/posts/a",
    });
    expect(ld.keywords).toBe("Go, SQLite");
    expect(ld.inLanguage).toBe("zh-CN");
    expect(ld.wordCount).toBe(1200);
    expect(ld.image).toEqual([
      "https://blog.zjut.si/uploads/aa/bb/1600.webp",
    ]);
  });

  it("没有封面 / 发布时间 / 标签时省略对应字段", () => {
    const minimal = buildArticleJsonLd({
      url: "https://blog.zjut.si/posts/b",
      title: "标题",
      description: "描述",
      publishedAt: null,
      updatedAt: 1700000000,
    });
    expect(minimal).not.toHaveProperty("image");
    expect(minimal).not.toHaveProperty("datePublished");
    expect(minimal).not.toHaveProperty("keywords");
    expect(minimal).not.toHaveProperty("wordCount");
    expect(minimal).not.toHaveProperty("articleSection");
    expect(minimal.inLanguage).toBe("zh-CN");
  });
});
