import type { Metadata } from "next";
import Link from "next/link";

import { publicApi } from "@/lib/api/public";
import { SITE } from "@/lib/constants/site";
import { buildOpenGraph, buildWebsiteJsonLd } from "@/lib/seo";
import { PostCard } from "@/ui/post/post-card";
import { JsonLd } from "@/ui/seo/json-ld";

/** 公开页按请求渲染（agent.md §8）。 */
export const dynamic = "force-dynamic";

/** 首页只放最新一屏，更多走 /posts。 */
const HOME_PAGE_SIZE = 10;

/** 首页的 title / description / canonical 与站点级默认一致（agent.md §11）。 */
export async function generateMetadata(): Promise<Metadata> {
  return {
    // `absolute` 避开根布局的 `%s · zjut.si` 模板，否则会拼成「zjut.si · zjut.si」。
    title: { absolute: SITE.title },
    description: SITE.description,
    alternates: { canonical: "/" },
    openGraph: buildOpenGraph({
      title: SITE.title,
      description: SITE.description,
      url: SITE.url,
    }),
  };
}

/** 首页：站点简介 + 置顶优先的最新文章。 */
export default async function HomePage() {
  const [site, paged] = await Promise.all([
    publicApi.site(),
    publicApi.posts({ page: 1, pageSize: HOME_PAGE_SIZE }),
  ]);

  return (
    <div className="mx-auto w-full max-w-3xl space-y-10">
      <JsonLd data={buildWebsiteJsonLd()} />

      <section className="space-y-3">
        <h1 className="text-2xl font-semibold tracking-tight">{SITE.name}</h1>
        <p className="text-sm leading-6 text-muted-foreground">
          {SITE.description}
        </p>
        <p className="text-xs text-muted-foreground">
          已发布 {site.counts.posts} 篇 · 标签 {site.counts.tags} 个 · 系列{" "}
          {site.counts.series} 个 · 友链 {site.counts.links} 条
        </p>
      </section>

      <section className="space-y-8" aria-label="最新文章">
        {paged.list.length === 0 ? (
          <p className="rounded-lg border border-dashed border-border px-4 py-12 text-center text-sm text-muted-foreground">
            还没有发布的文章。
          </p>
        ) : (
          paged.list.map((post, index) => (
            <PostCard key={post.id} post={post} priority={index === 0} />
          ))
        )}
      </section>

      {paged.total > HOME_PAGE_SIZE ? (
        <p className="text-sm">
          <Link
            href="/posts"
            className="underline-offset-4 hover:underline"
          >
            查看全部 {paged.total} 篇 →
          </Link>
        </p>
      ) : null}
    </div>
  );
}
