import { publicApi, type PublicPostDetail } from "@/lib/api/public";
import { SITE } from "@/lib/constants/site";
import {
  absoluteUrl,
  buildRssXml,
  excerptFromHtml,
  type RssItemInput,
} from "@/lib/seo";

/**
 * rss.xml（agent.md §11「rss.xml 全文」）。
 *
 * 用 route handler 而不是预生成的静态文件：每次请求现取，构建期不访问 API（§8）。
 * 列表只取最近 20 篇已发布文章，正文再逐篇取详情；草稿 / 定时 / 归档 / 已删除
 * 内容进不了公开列表，也就进不了这个 feed。
 */
export const dynamic = "force-dynamic";

/** 频道条数上限（agent.md §11）。 */
const FEED_SIZE = 20;

/** 单篇详情 → `<item>`；描述优先 SEO 描述，其次摘要，最后正文前 160 字纯文本。 */
function toRssItem(post: PublicPostDetail): RssItemInput {
  const link = absoluteUrl(`/posts/${post.slug}`);
  return {
    title: post.seoTitle || post.title,
    link,
    guid: link,
    pubDate: post.publishedAt ?? post.updatedAt,
    description:
      post.seoDescription || post.excerpt || excerptFromHtml(post.contentHtml),
    contentHtml: post.contentHtml,
  };
}

/** 输出 RSS 2.0；头带 10 分钟浏览器缓存。 */
export async function GET(): Promise<Response> {
  const paged = await publicApi.posts({ page: 1, pageSize: FEED_SIZE });

  // 逐篇取正文；某篇恰好在这一刻下线（归档 / 删除）时跳过它，别让整条 feed 挂掉。
  const details = await Promise.all(
    paged.list.map((item) => publicApi.post(item.slug).catch(() => null)),
  );
  const posts = details.filter(
    (detail): detail is PublicPostDetail => detail !== null,
  );
  if (paged.list.length > 0 && posts.length === 0) {
    // 列表有内容却一篇正文都取不到，说明 API 出了问题，宁可报错也不要输出空 feed。
    throw new Error("RSS 取正文失败");
  }

  const xml = buildRssXml({
    title: SITE.title,
    link: SITE.url,
    description: SITE.description,
    language: SITE.locale,
    selfLink: `${SITE.url}/rss.xml`,
    lastBuildDate: Math.floor(Date.now() / 1000),
    items: posts.map(toRssItem),
  });

  return new Response(xml, {
    headers: {
      "Content-Type": "application/rss+xml; charset=utf-8",
      "Cache-Control": "public, max-age=600",
    },
  });
}
