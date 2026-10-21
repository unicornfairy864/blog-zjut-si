import type { Metadata } from "next";
import Link from "next/link";

import { publicApi } from "@/lib/api/public";
import { absoluteUrl, buildOpenGraph } from "@/lib/seo";
import { formatDate } from "@/lib/utils";

export const dynamic = "force-dynamic";

const DESCRIPTION = "按年月归档的全部文章。";

/** 归档页的 title / description / canonical（agent.md §11）。 */
export async function generateMetadata(): Promise<Metadata> {
  return {
    title: "归档",
    description: DESCRIPTION,
    alternates: { canonical: "/archive" },
    openGraph: buildOpenGraph({
      title: "归档",
      description: DESCRIPTION,
      url: absoluteUrl("/archive"),
    }),
  };
}

/** 归档页：年 → 月 → 文章（agent.md §5 `GET /archive`）。 */
export default async function ArchivePage() {
  const archive = await publicApi.archive();

  return (
    <div className="mx-auto w-full max-w-3xl space-y-8">
      <header className="space-y-2">
        <h1 className="text-2xl font-semibold tracking-tight">归档</h1>
        <p className="text-xs text-muted-foreground">
          共 {archive.total} 篇
        </p>
      </header>

      {archive.years.length === 0 ? (
        <p className="rounded-lg border border-dashed border-border px-4 py-12 text-center text-sm text-muted-foreground">
          还没有文章。
        </p>
      ) : (
        <div className="space-y-10">
          {archive.years.map((year) => (
            <section key={year.year} className="space-y-6">
              <h2 className="text-lg font-semibold">
                {year.year}
                <span className="ml-2 text-xs font-normal text-muted-foreground">
                  {year.count} 篇
                </span>
              </h2>

              {year.months.map((month) => (
                <div key={month.month} className="space-y-3">
                  <h3 className="text-sm font-medium text-muted-foreground">
                    {month.month} 月
                    <span className="ml-2 text-xs">{month.count} 篇</span>
                  </h3>
                  <ul className="space-y-2">
                    {month.posts.map((post) => (
                      <li
                        key={post.id}
                        className="flex items-baseline gap-3 text-sm"
                      >
                        <time
                          dateTime={formatDate(post.publishedAt)}
                          className="shrink-0 font-mono text-xs text-muted-foreground"
                        >
                          {formatDate(post.publishedAt).slice(5)}
                        </time>
                        <Link
                          href={`/posts/${post.slug}`}
                          className="min-w-0 break-words underline-offset-4 hover:underline"
                        >
                          {post.title}
                        </Link>
                      </li>
                    ))}
                  </ul>
                </div>
              ))}
            </section>
          ))}
        </div>
      )}
    </div>
  );
}
