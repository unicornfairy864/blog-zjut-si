import type { Metadata } from "next";

import { publicApi } from "@/lib/api/public";
import { Pagination } from "@/ui/shared/pagination";
import { PostCard } from "@/ui/post/post-card";

export const dynamic = "force-dynamic";

const PAGE_SIZE = 10;

export const metadata: Metadata = {
  title: "全部文章",
  description: "按发布时间倒序的全部文章。",
};

/** 把 `?page=` 收敛成合法页码。 */
function parsePage(raw: string | string[] | undefined): number {
  const value = Array.isArray(raw) ? raw[0] : raw;
  const page = Number.parseInt(value ?? "1", 10);
  return Number.isFinite(page) && page > 0 ? page : 1;
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
