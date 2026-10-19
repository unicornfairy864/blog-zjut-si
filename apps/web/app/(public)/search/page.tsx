import type { Metadata } from "next";

import { isApiError, publicApi, type Paged, type PublicPostListItem } from "@/lib/api/public";
import { Pagination } from "@/ui/shared/pagination";
import { PostCard } from "@/ui/post/post-card";

export const dynamic = "force-dynamic";

const PAGE_SIZE = 10;
/** 与服务端一致的关键词长度上限（agent.md §5）。 */
const MAX_KEYWORD_LENGTH = 64;

export const metadata: Metadata = {
  title: "搜索",
  description: "站内全文搜索。",
};

interface SearchPageProps {
  searchParams: Promise<Record<string, string | string[] | undefined>>;
}

function first(raw: string | string[] | undefined): string {
  return (Array.isArray(raw) ? raw[0] : raw) ?? "";
}

function parsePage(raw: string | string[] | undefined): number {
  const page = Number.parseInt(first(raw) || "1", 10);
  return Number.isFinite(page) && page > 0 ? page : 1;
}

const EMPTY: Paged<PublicPostListItem> = {
  list: [],
  total: 0,
  page: 1,
  pageSize: PAGE_SIZE,
};

/** 搜索页：`?q=` + `?page=`，用原生 GET 表单，无 JS 也能用。 */
export default async function SearchPage({ searchParams }: SearchPageProps) {
  const params = await searchParams;
  const keyword = first(params.q).trim();
  const page = parsePage(params.page);
  const tooLong = keyword.length > MAX_KEYWORD_LENGTH;

  let result: Paged<PublicPostListItem> | null = null;
  if (keyword && !tooLong) {
    try {
      result = await publicApi.search(keyword, { page, pageSize: PAGE_SIZE });
    } catch (error) {
      if (!isApiError(error, 400)) {
        throw error;
      }
      result = EMPTY;
    }
  }

  return (
    <div className="mx-auto w-full max-w-3xl space-y-8">
      <header className="space-y-4">
        <h1 className="text-2xl font-semibold tracking-tight">搜索</h1>
        <form action="/search" method="get" className="flex gap-2">
          <label htmlFor="q" className="sr-only">
            关键词
          </label>
          <input
            id="q"
            name="q"
            type="search"
            defaultValue={keyword}
            maxLength={MAX_KEYWORD_LENGTH}
            placeholder="输入关键词，回车搜索"
            className="h-10 w-full min-w-0 rounded-md border border-input bg-background px-3 text-sm outline-none ring-offset-background focus-visible:ring-2 focus-visible:ring-ring"
          />
          <button
            type="submit"
            className="h-10 shrink-0 rounded-md bg-primary px-4 text-sm font-medium text-primary-foreground"
          >
            搜索
          </button>
        </form>
      </header>

      {tooLong ? (
        <p className="text-sm text-muted-foreground">
          关键词最多 {MAX_KEYWORD_LENGTH} 个字符。
        </p>
      ) : null}

      {!keyword ? (
        <p className="text-sm text-muted-foreground">
          输入关键词开始搜索：中文 2 字即可命中，英文按词匹配。
        </p>
      ) : null}

      {result ? (
        <>
          <p className="text-xs text-muted-foreground">
            「{keyword}」命中 {result.total} 篇
          </p>

          <section className="space-y-8">
            {result.list.length === 0 ? (
              <p className="rounded-lg border border-dashed border-border px-4 py-12 text-center text-sm text-muted-foreground">
                没有匹配的文章。
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
              `/search?q=${encodeURIComponent(keyword)}${
                target > 1 ? `&page=${target}` : ""
              }`
            }
          />
        </>
      ) : null}
    </div>
  );
}
