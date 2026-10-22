import type { Metadata } from "next";
import Link from "next/link";

import { publicApi } from "@/lib/api/public";
import { absoluteUrl, buildOpenGraph } from "@/lib/seo";
import { Divider } from "@/ui/layout/divider";

export const dynamic = "force-dynamic";

const DESCRIPTION = "成系列的长文集合。";

/** 系列列表的 title / description / canonical（agent.md §11）。 */
export async function generateMetadata(): Promise<Metadata> {
  return {
    title: "系列",
    description: DESCRIPTION,
    alternates: { canonical: "/series" },
    openGraph: buildOpenGraph({
      title: "系列",
      description: DESCRIPTION,
      url: absoluteUrl("/series"),
    }),
  };
}

/** 系列列表（agent.md §5 `GET /series`）。 */
export default async function SeriesListPage() {
  const { list } = await publicApi.series();

  return (
    <div className="mx-auto w-full max-w-3xl space-y-6">
      <header className="space-y-2">
        <h1 className="text-2xl font-semibold tracking-tight">系列</h1>
        <p className="text-xs text-muted-foreground">共 {list.length} 个</p>
      </header>
      <Divider />

      {list.length === 0 ? (
        <p className="rounded-lg border border-dashed border-border px-4 py-12 text-center text-sm text-muted-foreground">
          还没有系列。
        </p>
      ) : (
        <ul className="space-y-4">
          {list.map((series) => (
            <li
              key={series.id}
              className="rounded-lg border border-border p-4"
            >
              <h2 className="text-lg font-semibold">
                <Link
                  href={`/series/${series.slug}`}
                  className="underline-offset-4 hover:underline"
                >
                  {series.name}
                </Link>
                <span className="ml-2 text-xs font-normal text-muted-foreground">
                  {series.count} 篇
                </span>
              </h2>
              {series.description ? (
                <p className="mt-2 text-sm leading-6 text-muted-foreground">
                  {series.description}
                </p>
              ) : null}
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
