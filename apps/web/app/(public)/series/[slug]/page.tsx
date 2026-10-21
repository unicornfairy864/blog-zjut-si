import type { Metadata } from "next";
import Link from "next/link";
import { notFound } from "next/navigation";

import { isApiCode, publicApi } from "@/lib/api/public";
import { absoluteUrl, buildOpenGraph } from "@/lib/seo";
import { formatDate } from "@/lib/utils";

export const dynamic = "force-dynamic";

interface SeriesDetailPageProps {
  params: Promise<{ slug: string }>;
}

/** 系列详情的 title / description / canonical（agent.md §11）。 */
export async function generateMetadata({
  params,
}: SeriesDetailPageProps): Promise<Metadata> {
  const { slug } = await params;
  try {
    const { series } = await publicApi.seriesDetail(slug);
    const description = series.description || `系列 ${series.name}。`;
    return {
      title: series.name,
      description,
      alternates: { canonical: `/series/${series.slug}` },
      openGraph: buildOpenGraph({
        title: series.name,
        description,
        url: absoluteUrl(`/series/${series.slug}`),
      }),
    };
  } catch {
    return { title: "系列" };
  }
}

/** 系列详情：按 series_order 顺序列出文章。 */
export default async function SeriesDetailPage({
  params,
}: SeriesDetailPageProps) {
  const { slug } = await params;

  let result;
  try {
    result = await publicApi.seriesDetail(slug);
  } catch (error) {
    if (isApiCode(error, 1002)) {
      notFound();
    }
    throw error;
  }

  return (
    <div className="mx-auto w-full max-w-3xl space-y-8">
      <header className="space-y-2">
        <h1 className="text-2xl font-semibold tracking-tight">
          {result.series.name}
        </h1>
        {result.series.description ? (
          <p className="text-sm leading-6 text-muted-foreground">
            {result.series.description}
          </p>
        ) : null}
        <p className="text-xs text-muted-foreground">
          共 {result.total} 篇 ·{" "}
          <Link href="/series" className="underline-offset-4 hover:underline">
            全部系列
          </Link>
        </p>
      </header>

      {result.list.length === 0 ? (
        <p className="rounded-lg border border-dashed border-border px-4 py-12 text-center text-sm text-muted-foreground">
          这个系列下还没有文章。
        </p>
      ) : (
        <ol className="space-y-3">
          {result.list.map((post, index) => (
            <li key={post.id} className="flex items-baseline gap-3 text-sm">
              <span className="w-6 shrink-0 text-right font-mono text-xs text-muted-foreground">
                {index + 1}
              </span>
              <Link
                href={`/posts/${post.slug}`}
                className="min-w-0 break-words underline-offset-4 hover:underline"
              >
                {post.title}
              </Link>
              <time
                dateTime={formatDate(post.publishedAt)}
                className="ml-auto shrink-0 font-mono text-xs text-muted-foreground"
              >
                {formatDate(post.publishedAt)}
              </time>
            </li>
          ))}
        </ol>
      )}
    </div>
  );
}
