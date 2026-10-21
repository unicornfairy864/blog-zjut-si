import type { Metadata } from "next";
import Link from "next/link";
import { notFound } from "next/navigation";
import { cache } from "react";

import { isApiCode, publicApi, type PublicPostDetail } from "@/lib/api/public";
import { SITE } from "@/lib/constants/site";
import {
  absoluteUrl,
  buildArticleJsonLd,
  buildOpenGraph,
  coverImageUrl,
  excerptFromHtml,
} from "@/lib/seo";
import { formatDate } from "@/lib/utils";
import { CommentSection } from "@/ui/post/comment-section";
import { PostCard } from "@/ui/post/post-card";
import { PostContent } from "@/ui/post/post-content";
import { PostImage } from "@/ui/post/post-image";
import { PostInteractions } from "@/ui/post/post-interactions";
import { PostToc, extractToc } from "@/ui/post/post-toc";
import { JsonLd } from "@/ui/seo/json-ld";

export const dynamic = "force-dynamic";

interface PostDetailPageProps {
  params: Promise<{ slug: string }>;
}

/**
 * 取文章详情；同一请求内 `generateMetadata` 与页面共用一次取数。
 *
 * 归档 / 已删除（code 1007）与不存在（code 1002）都走 `notFound()`：1007 的
 * 正常路径由 `middleware.ts` 提前拦截并返回真正的 410 页面。
 */
const loadPost = cache(async (slug: string): Promise<PublicPostDetail> => {
  try {
    return await publicApi.post(slug);
  } catch (error) {
    if (isApiCode(error, 1002) || isApiCode(error, 1007)) {
      notFound();
    }
    throw error;
  }
});

/** 文章页的 canonical：优先作者填的 `canonicalUrl`，否则用自身路径。 */
function canonicalPath(post: PublicPostDetail): string {
  return post.canonicalUrl || `/posts/${post.slug}`;
}

/** 文章页的 description：SEO 描述 → 摘要 → 站点描述。 */
function descriptionOf(post: PublicPostDetail): string {
  return post.seoDescription || post.excerpt || SITE.description;
}

export async function generateMetadata({
  params,
}: PostDetailPageProps): Promise<Metadata> {
  const { slug } = await params;
  try {
    const post = await loadPost(slug);
    const title = post.seoTitle || post.title;
    const description = descriptionOf(post);
    const cover = coverImageUrl(post.cover);
    return {
      title,
      description,
      alternates: { canonical: canonicalPath(post) },
      openGraph: buildOpenGraph({
        type: "article",
        title,
        description,
        url: absoluteUrl(canonicalPath(post)),
        publishedTime: post.publishedAt
          ? new Date(post.publishedAt * 1000).toISOString()
          : undefined,
        modifiedTime: new Date(post.updatedAt * 1000).toISOString(),
        tags: post.tags.map((tag) => tag.name),
        // 封面取 1600 档；没有封面就不给 images（agent.md §11）。
        images: cover ? [cover] : undefined,
      }),
    };
  } catch {
    return { title: "文章" };
  }
}

/** 文章详情：正文 + 目录 + 上下篇 + 相关文章（agent.md §11）。 */
export default async function PostDetailPage({
  params,
}: PostDetailPageProps) {
  const { slug } = await params;
  const post = await loadPost(slug);

  const related = await publicApi
    .related(slug)
    .then((result) => result.list)
    .catch(() => []);
  const comments = await publicApi
    .comments(slug)
    .catch(() => ({ total: 0, list: [] }));
  const toc = extractToc(post.contentHtml);
  const cover = coverImageUrl(post.cover);

  return (
    <div className="mx-auto w-full max-w-5xl">
      <JsonLd
        data={buildArticleJsonLd({
          url: absoluteUrl(canonicalPath(post)),
          title: post.seoTitle || post.title,
          description:
            post.seoDescription ||
            post.excerpt ||
            excerptFromHtml(post.contentHtml),
          publishedAt: post.publishedAt,
          updatedAt: post.updatedAt,
          images: cover ? [absoluteUrl(cover)] : undefined,
          keywords: post.tags.map((tag) => tag.name),
          lang: post.lang,
          wordCount: post.wordCount,
        })}
      />
      <div className="lg:grid lg:grid-cols-[minmax(0,1fr)_14rem] lg:gap-10">
        <article className="min-w-0 space-y-6">
          <header className="space-y-3">
            <h1 className="break-words text-3xl font-semibold leading-tight tracking-tight">
              {post.title}
            </h1>
            <p className="flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-muted-foreground">
              <time dateTime={formatDate(post.publishedAt)}>
                {formatDate(post.publishedAt)}
              </time>
              <span>{post.readingTime} 分钟</span>
              <span>{post.wordCount} 字</span>
            </p>
            {/* 阅读数 / 点赞数只在这里由客户端异步补上，不进 SSR HTML（§17.1）。 */}
            <PostInteractions slug={post.slug} />
            {post.tags.length > 0 ? (
              <ul className="flex flex-wrap gap-2 text-xs">
                {post.tags.map((tag) => (
                  <li key={tag.slug}>
                    <Link
                      href={`/tags/${tag.slug}`}
                      className="rounded-full bg-muted px-2 py-0.5 text-muted-foreground hover:text-foreground"
                    >
                      #{tag.name}
                    </Link>
                  </li>
                ))}
              </ul>
            ) : null}
          </header>

          {post.cover ? (
            <figure className="space-y-2">
              <PostImage media={post.cover} priority />
              {post.cover.origUrl ? (
                <figcaption className="text-right text-xs text-muted-foreground">
                  <a
                    href={post.cover.origUrl}
                    target="_blank"
                    rel="noopener noreferrer"
                    className="underline-offset-4 hover:underline"
                  >
                    查看原图
                  </a>
                </figcaption>
              ) : null}
            </figure>
          ) : null}

          {toc.length > 1 ? (
            <details className="rounded-lg border border-border p-4 lg:hidden">
              <summary className="cursor-pointer text-sm font-medium">
                目录
              </summary>
              <PostToc items={toc} className="mt-3" />
            </details>
          ) : null}

          <PostContent html={post.contentHtml} />

          {post.prev || post.next ? (
            <nav
              aria-label="上下篇"
              className="flex flex-col gap-3 border-t border-border pt-6 text-sm sm:flex-row sm:justify-between"
            >
              {post.prev ? (
                <Link
                  href={`/posts/${post.prev.slug}`}
                  className="underline-offset-4 hover:underline"
                >
                  ← {post.prev.title}
                </Link>
              ) : (
                <span />
              )}
              {post.next ? (
                <Link
                  href={`/posts/${post.next.slug}`}
                  className="underline-offset-4 hover:underline sm:text-right"
                >
                  {post.next.title} →
                </Link>
              ) : null}
            </nav>
          ) : null}
        </article>

        <aside className="hidden lg:block">
          <div className="sticky top-6">
            <PostToc items={toc} />
          </div>
        </aside>
      </div>

      {related.length > 0 ? (
        <section className="mt-14 space-y-8" aria-label="相关文章">
          <h2 className="text-lg font-semibold">相关文章</h2>
          {related.map((item) => (
            <PostCard key={item.id} post={item} />
          ))}
        </section>
      ) : null}

      <div className="mt-14">
        <CommentSection
          slug={post.slug}
          total={comments.total}
          comments={comments.list}
          allowComments={post.allowComments}
        />
      </div>
    </div>
  );
}
