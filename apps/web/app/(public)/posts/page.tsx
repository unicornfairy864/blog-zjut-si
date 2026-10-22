import type { Metadata } from "next";

import { publicApi } from "@/lib/api/public";
import { absoluteUrl, buildOpenGraph } from "@/lib/seo";
import { Divider } from "@/ui/layout/divider";
import { Pagination } from "@/ui/shared/pagination";
import { PostCard } from "@/ui/post/post-card";

export const dynamic = "force-dynamic";

const PAGE_SIZE = 10;

/** 列表页固定文案（agent.md §11）。 */
const LIST_DESCRIPTION = "按发布时间倒序的全部文章。";

/** 把 `?page=` 收敛成合法页码。 */
function parsePage(raw: string | string[] | undefined): number {
  const value = Array.isArray(raw) ? raw[0] : raw;
  const page = Number.parseInt(value ?? "1", 10);
  return Number.isFinite(page) && page > 0 ? page : 1;
}

/**
 * 每页唯一的 title / description / canonical（agent.md §11）。
 *
 * 第 2 页起 canonical 就是 `?page=N` 本身，不能一律指回第 1 页。
 */
export async function generateMetadata({
  searchParams,
}: {
  searchParams: Promise<Record<string, string | string[] | undefined>>;
}): Promise<Metadata> {
  const params = await searchParams;
  const page = parsePage(params.page);
  const title = page > 1 ? `文章 · 第 ${page} 页` : "文章";
  const canonical = page > 1 ? `/posts?page=${page}` : "/posts";
  return {
    title,
    description: LIST_DESCRIPTION,
    alternates: { canonical },
    openGraph: buildOpenGraph({
      title,
      description: LIST_DESCRIPTION,
      url: absoluteUrl(canonical),
    }),
  };
}

/** 文章列表：`?page=` 分页（agent.md §16 G4「分页正确」）。 */
export default async function PostListPage({
  searchParams,
}: {
  searchParams: Promise<Record<string, string | string[] | undefined>>;
}) {
  const params = await searchParams;
  const page = parsePage(params.page);
  const paged = await publicApi.posts({ page, pageSize: PAGE_SIZE });

  return (
    <div className="mx-auto w-full max-w-3xl space-y-8">
      <header className="space-y-2">
        <h1 className="text-2xl font-semibold tracking-tight">全部文章</h1>
        <p className="text-xs text-muted-foreground">共 {paged.total} 篇</p>
      </header>

      {/* 标题区与列表之间一条分割线（§8）。 */}
      <Divider />

      <section className="space-y-8">
        {paged.list.length === 0 ? (
          <p className="rounded-lg border border-dashed border-border px-4 py-12 text-center text-sm text-muted-foreground">
            这一页没有文章。
          </p>
        ) : (
          paged.list.map((post, index) => (
            <PostCard key={post.id} post={post} priority={index === 0} />
          ))
        )}
      </section>

      <Pagination
        page={paged.page}
        pageSize={paged.pageSize}
        total={paged.total}
        hrefFor={(target) => (target > 1 ? `/posts?page=${target}` : "/posts")}
      />
    </div>
  );
}
