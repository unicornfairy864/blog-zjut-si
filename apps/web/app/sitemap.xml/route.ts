import { publicApi, type PublicPostListItem } from "@/lib/api/public";
import {
  buildSitemapXml,
  fixedSitemapEntries,
  latestUpdate,
  pageSitemapEntries,
  postSitemapEntries,
  taxonomySitemapEntries,
} from "@/lib/seo";

/**
 * sitemap.xml（agent.md §11 / §12）。
 *
 * 用 route handler 而不是 `sitemap.ts`：数据每次请求现取，构建期不访问 API（§8）。
 * 只收录已发布文章与未删除静态页；标签 / 系列只收 `count > 0` 的，
 * 草稿、定时、归档、已删除内容既不会出现在公开 API 里，这里也不做任何兜底拼接。
 */
export const dynamic = "force-dynamic";

/** `pageSize` 上限 50（agent.md §5）。 */
const PAGE_SIZE = 50;

/** 翻页上限：50 × 20 = 1000 篇，防御性地兜住异常的分页返回。 */
const MAX_PAGES = 20;

/** 按页取全量已发布文章，直到累计条数追平 `total`。 */
async function listAllPublishedPosts(): Promise<PublicPostListItem[]> {
  const posts: PublicPostListItem[] = [];
  for (let page = 1; page <= MAX_PAGES; page += 1) {
    const paged = await publicApi.posts({ page, pageSize: PAGE_SIZE });
    posts.push(...paged.list);
    if (paged.list.length === 0 || posts.length >= paged.total) {
      break;
    }
  }
  return posts;
}

/** 输出 `<urlset>`；头带 10 分钟浏览器缓存。 */
export async function GET(): Promise<Response> {
  const now = Math.floor(Date.now() / 1000);
  const [posts, pages, tags, series] = await Promise.all([
    listAllPublishedPosts(),
    publicApi.pages(),
    publicApi.tags(),
    publicApi.series(),
  ]);

  // 固定页没有自己的 updatedAt，用全站最近一次内容更新当 lastmod。
  const lastmod = latestUpdate(
    [
      ...posts.map((post) => post.updatedAt),
      ...pages.list.map((page) => page.updatedAt),
    ],
    now,
  );

  const xml = buildSitemapXml([
    ...fixedSitemapEntries(lastmod),
    ...postSitemapEntries(posts),
    ...pageSitemapEntries(pages.list),
    ...taxonomySitemapEntries("tags", tags.list),
    ...taxonomySitemapEntries("series", series.list),
  ]);

  return new Response(xml, {
    headers: {
      "Content-Type": "application/xml; charset=utf-8",
      "Cache-Control": "public, max-age=600",
    },
  });
}
