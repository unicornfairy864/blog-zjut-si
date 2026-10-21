import type { Metadata } from "next";
import { notFound } from "next/navigation";
import { cache } from "react";

import { isApiCode, publicApi, type PageDetail } from "@/lib/api/public";
import { SITE } from "@/lib/constants/site";
import { absoluteUrl, buildOpenGraph, excerptFromHtml } from "@/lib/seo";
import { formatDate } from "@/lib/utils";
import { PostContent } from "@/ui/post/post-content";

export const dynamic = "force-dynamic";

interface StaticPageProps {
  params: Promise<{ slug: string }>;
}

/**
 * 取静态页（`/about` 等，agent.md §4 `pages`）。
 *
 * 静态路由（/posts、/tags、/archive、/search、/series、/links）优先于本动态段，
 * 所以这里只会收到「像静态页的」单段路径；不存在就 404。
 */
const loadPage = cache(async (slug: string): Promise<PageDetail> => {
  try {
    return await publicApi.page(slug);
  } catch (error) {
    if (isApiCode(error, 1002) || isApiCode(error, 1007)) {
      notFound();
    }
    throw error;
  }
});

/** 静态页的 description：SEO 描述 → 正文前 160 字纯文本 → 站点描述（agent.md §11）。 */
function descriptionOf(page: PageDetail): string {
  return (
    page.seoDescription || excerptFromHtml(page.contentHtml) || SITE.description
  );
}

export async function generateMetadata({
  params,
}: StaticPageProps): Promise<Metadata> {
  const { slug } = await params;
  try {
    const page = await loadPage(slug);
    const title = page.seoTitle || page.title;
    const description = descriptionOf(page);
    return {
      title,
      description,
      alternates: { canonical: `/${page.slug}` },
      openGraph: buildOpenGraph({
        title,
        description,
        url: absoluteUrl(`/${page.slug}`),
      }),
    };
  } catch {
    return { title: "页面" };
  }
}

/** 静态页渲染：正文同样走 PostContent（KaTeX / 代码块增强）。 */
export default async function StaticPage({ params }: StaticPageProps) {
  const { slug } = await params;
  const page = await loadPage(slug);

  return (
    <article className="mx-auto w-full max-w-3xl space-y-6">
      <h1 className="break-words text-3xl font-semibold leading-tight tracking-tight">
        {page.title}
      </h1>
      <PostContent html={page.contentHtml} />
      <p className="border-t border-border pt-4 text-xs text-muted-foreground">
        最后更新 {formatDate(page.updatedAt)}
      </p>
    </article>
  );
}
