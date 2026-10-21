import type { Metadata } from "next";
import Link from "next/link";
import { notFound } from "next/navigation";

import { isApiCode, publicApi } from "@/lib/api/public";
import { absoluteUrl, buildOpenGraph } from "@/lib/seo";
import { Pagination } from "@/ui/shared/pagination";
import { PostCard } from "@/ui/post/post-card";

export const dynamic = "force-dynamic";

const PAGE_SIZE = 10;

interface TagPageProps {
  params: Promise<{ slug: string }>;
  searchParams: Promise<Record<string, string | string[] | undefined>>;
}

function parsePage(raw: string | string[] | undefined): number {
  const value = Array.isArray(raw) ? raw[0] : raw;
  const page = Number.parseInt(value ?? "1", 10);
  return Number.isFinite(page) && page > 0 ? page : 1;
}

/**
 * 标签详情的 title / description / canonical。
 *
 * 这个页面也分页，所以 canonical 同样要带上 `?page=N`，否则每页的 canonical 会撞在一起。
 */
export async function generateMetadata({
  params,
  searchParams,
}: TagPageProps): Promise<Metadata> {
  const { slug } = await params;
  const query = await searchParams;
  const page = parsePage(query.page);
  try {
    const { tag } = await publicApi.tag(slug);
    const title = `#${tag.name}${page > 1 ? ` · 第 ${page} 页` : ""}`;
    const description = tag.description || `标签 ${tag.name} 下的文章。`;
    const canonical =
      page > 1 ? `/tags/${tag.slug}?page=${page}` : `/tags/${tag.slug}`;
    return {
      title,
      description,
      alternates: { canonical },
      openGraph: buildOpenGraph({
        title,
        description,
        url: absoluteUrl(canonical),
      }),
    };
  } catch {
    return { title: "标签" };
  }
}

/** 标签详情：标签下的分页文章（agent.md §16 G4「标签组合正确」）。 */
export default async function TagDetailPage({
  params,
  searchParams,
}: TagPageProps) {
  const { slug } = await params;
  const query = await searchParams;
  const page = parsePage(query.page);

  let result;
  try {
    result = await publicApi.tag(slug, { page, pageSize: PAGE_SIZE });
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
          #{result.tag.name}
        </h1>
        {result.tag.description ? (
          <p className="text-sm leading-6 text-muted-foreground">
            {result.tag.description}
          </p>
        ) : null}
        <p className="text-xs text-muted-foreground">
          共 {result.total} 篇 ·{" "}
          <Link href="/tags" className="underline-offset-4 hover:underline">
            全部标签
          </Link>
        </p>
      </header>

      <section className="space-y-8">
        {result.list.length === 0 ? (
          <p className="rounded-lg border border-dashed border-border px-4 py-12 text-center text-sm text-muted-foreground">
            这个标签下还没有文章。
          </p>
        ) : (
          result.list.map((post, index) => (
            <PostCard key={post.id} post={post} priority={index === 0} />
          ))
        )}
      </section>

      <Pagination
        page={result.page}
        pageSize={result.pageSize}
        total={result.total}
        hrefFor={(target) =>
          target > 1
            ? `/tags/${result.tag.slug}?page=${target}`
            : `/tags/${result.tag.slug}`
        }
      />
    </div>
  );
}
