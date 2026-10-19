import type { Metadata } from "next";
import { notFound } from "next/navigation";
import { cache } from "react";

import { isApiError, publicApi, type PageDetail } from "@/lib/api/public";
import { SITE } from "@/lib/constants/site";
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
    if (isApiError(error, 404) || isApiError(error, 410)) {
      notFound();
    }
    throw error;
  }
});

export async function generateMetadata({
  params,
}: StaticPageProps): Promise<Metadata> {
  const { slug } = await params;
  try {
    const page = await loadPage(slug);
    return {
      title: page.seoTitle || page.title,
      description: page.seoDescription || SITE.description,
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
