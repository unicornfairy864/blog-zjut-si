import type { Metadata } from "next";
import Link from "next/link";

import { publicApi } from "@/lib/api/public";

export const dynamic = "force-dynamic";

export const metadata: Metadata = {
  title: "标签",
  description: "按标签浏览文章。",
};

/** 标签云：只列有已发布文章的标签（agent.md §5）。 */
export default async function TagsPage() {
  const { list } = await publicApi.tags();

  return (
    <div className="mx-auto w-full max-w-3xl space-y-6">
      <header className="space-y-2">
        <h1 className="text-2xl font-semibold tracking-tight">标签</h1>
        <p className="text-xs text-muted-foreground">共 {list.length} 个</p>
      </header>

      {list.length === 0 ? (
        <p className="rounded-lg border border-dashed border-border px-4 py-12 text-center text-sm text-muted-foreground">
          还没有标签。
        </p>
      ) : (
        <ul className="flex flex-wrap gap-3">
          {list.map((tag) => (
            <li key={tag.slug}>
              <Link
                href={`/tags/${tag.slug}`}
                className="inline-flex items-baseline gap-2 rounded-full border border-border px-3 py-1 text-sm underline-offset-4 hover:underline"
              >
                <span>#{tag.name}</span>
                <span className="text-xs text-muted-foreground">
                  {tag.count}
                </span>
              </Link>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
