"use client";

import Link from "next/link";
import { useEffect, useState } from "react";
import useSWR from "swr";

import type { ApiError } from "@/lib/api/fetcher";
import { adminPostApi, type PostListResult } from "@/lib/api/posts";
import { swrKeys } from "@/lib/swr/keys";
import { formatDateTime } from "@/lib/utils";
import { Alert, AlertDescription } from "@/ui/shadcn/alert";
import { Button } from "@/ui/shadcn/button";
import { PostStatusBadge } from "@/ui/admin/post-status-badge";
import { Loading } from "@/ui/shared/loading";

const FILTERS = [
  { value: "", label: "全部" },
  { value: "draft", label: "草稿" },
  { value: "scheduled", label: "定时" },
  { value: "published", label: "已发布" },
  { value: "archived", label: "已归档" },
] as const;

/** 每页条数；服务端上限 50（agent.md §5）。 */
const PAGE_SIZE = 20;

/**
 * 管理端文章列表（D5）。
 *
 * 只做导航与状态浏览；发布、归档、删除都在编辑器里完成，避免列表误操作。
 */
export function PostList() {
  const [status, setStatus] = useState<string>("");
  const [page, setPage] = useState(1);

  const { data, error, isLoading } = useSWR<PostListResult, ApiError>(
    swrKeys.adminPosts(status, page, PAGE_SIZE),
    () => adminPostApi.list({ status, page, pageSize: PAGE_SIZE }),
  );

  const total = data?.total ?? 0;
  const totalPages = Math.max(1, Math.ceil(total / PAGE_SIZE));

  // 删除或切换筛选后当前页可能越界，退回到最后一页。
  useEffect(() => {
    if (data && page > totalPages) {
      setPage(totalPages);
    }
  }, [data, page, totalPages]);

  function changeStatus(next: string) {
    setStatus(next);
    setPage(1);
  }

  return (
    <section className="space-y-6">
      <header className="flex flex-wrap items-end justify-between gap-4">
        <div className="space-y-1">
          <h1 className="text-2xl font-semibold tracking-tight">文章</h1>
          <p className="text-sm text-muted-foreground">
            共 {total} 篇；草稿不会出现在公开站。
          </p>
        </div>
        <Button asChild>
          <Link href="/admin/posts/new">新建文章</Link>
        </Button>
      </header>

      <div className="flex flex-wrap gap-2" role="group" aria-label="按状态筛选">
        {FILTERS.map((filter) => (
          <Button
            key={filter.value}
            size="sm"
            variant={status === filter.value ? "default" : "outline"}
            aria-pressed={status === filter.value}
            onClick={() => changeStatus(filter.value)}
          >
            {filter.label}
          </Button>
        ))}
      </div>

      {error ? (
        <Alert variant="destructive">
          <AlertDescription>{error.message}</AlertDescription>
        </Alert>
      ) : null}

      {isLoading ? <Loading label="正在加载文章…" /> : null}

      {data ? (
        data.list.length === 0 ? (
          <p className="rounded-lg border border-dashed border-border px-4 py-12 text-center text-sm text-muted-foreground">
            这一页还没有文章。
          </p>
        ) : (
          <div className="overflow-x-auto rounded-lg border border-border">
            <table className="w-full text-sm">
              <caption className="sr-only">文章列表</caption>
              <thead className="bg-muted/60 text-left text-xs text-muted-foreground">
                <tr>
                  <th scope="col" className="px-4 py-3 font-medium">
                    标题
                  </th>
                  <th scope="col" className="px-4 py-3 font-medium">
                    状态
                  </th>
                  <th scope="col" className="px-4 py-3 font-medium">
                    分类
                  </th>
                  <th scope="col" className="px-4 py-3 font-medium">
                    字数
                  </th>
                  <th scope="col" className="px-4 py-3 font-medium">
                    更新时间
                  </th>
                  <th scope="col" className="px-4 py-3 text-right font-medium">
                    操作
                  </th>
                </tr>
              </thead>
              <tbody>
                {data.list.map((post) => (
                  <tr key={post.id} className="border-t border-border">
                    <td className="max-w-[28rem] px-4 py-3">
                      <Link
                        href={`/admin/posts/${post.id}`}
                        className="line-clamp-1 font-medium underline-offset-4 hover:underline"
                      >
                        {post.title}
                      </Link>
                      <p className="line-clamp-1 text-xs text-muted-foreground">
                        /posts/{post.slug}
                      </p>
                    </td>
                    <td className="px-4 py-3">
                      <PostStatusBadge status={post.status} />
                    </td>
                    <td className="px-4 py-3">
                      {/* 系列与标签是"文章归类"的唯一可见处：空着说明还没挂（D20 起编辑器可挂）。 */}
                      {post.seriesName || post.tags.length > 0 ? (
                        <div className="flex flex-wrap items-center gap-1">
                          {post.seriesName ? (
                            <span className="rounded bg-muted px-1.5 py-0.5 text-xs">
                              {post.seriesName}
                            </span>
                          ) : null}
                          {post.tags.map((tag) => (
                            <span
                              key={tag.slug}
                              className="rounded border border-border px-1.5 py-0.5 text-xs text-muted-foreground"
                            >
                              {tag.name}
                            </span>
                          ))}
                        </div>
                      ) : (
                        <span className="text-xs text-muted-foreground">—</span>
                      )}
                    </td>
                    <td className="px-4 py-3 text-muted-foreground">
                      {post.wordCount}
                    </td>
                    <td className="px-4 py-3 text-muted-foreground">
                      {formatDateTime(post.updatedAt)}
                    </td>
                    <td className="px-4 py-3 text-right">
                      <Button asChild size="sm" variant="outline">
                        <Link href={`/admin/posts/${post.id}`}>编辑</Link>
                      </Button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )
      ) : null}

      {data ? (
        <nav
          aria-label="分页"
          className="flex flex-wrap items-center justify-between gap-4 text-sm text-muted-foreground"
        >
          <span>
            共 {total} 篇，第 {page} / {totalPages} 页
          </span>
          <div className="flex items-center gap-2">
            <Button
              type="button"
              variant="outline"
              size="sm"
              onClick={() => setPage((current) => Math.max(1, current - 1))}
              disabled={page <= 1}
            >
              上一页
            </Button>
            <span aria-live="polite" className="px-1">
              {page} / {totalPages}
            </span>
            <Button
              type="button"
              variant="outline"
              size="sm"
              onClick={() => setPage((current) => Math.min(totalPages, current + 1))}
              disabled={page >= totalPages}
            >
              下一页
            </Button>
          </div>
        </nav>
      ) : null}
    </section>
  );
}
