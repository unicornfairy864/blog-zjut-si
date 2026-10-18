"use client";

import Link from "next/link";
import { useState } from "react";
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
  { value: "published", label: "已发布" },
  { value: "archived", label: "已归档" },
] as const;

/**
 * 管理端文章列表（D5）。
 *
 * 只做导航与状态浏览；发布、归档、删除都在编辑器里完成，避免列表误操作。
 */
export function PostList() {
  const [status, setStatus] = useState<string>("");

  const { data, error, isLoading } = useSWR<PostListResult, ApiError>(
    swrKeys.adminPosts(status),
    () => adminPostApi.list({ status, pageSize: 50 }),
  );

  return (
    <section className="space-y-6">
      <header className="flex flex-wrap items-end justify-between gap-4">
        <div className="space-y-1">
          <h1 className="text-2xl font-semibold tracking-tight">文章</h1>
          <p className="text-sm text-muted-foreground">
            共 {data?.total ?? 0} 篇；草稿不会出现在公开站。
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
            onClick={() => setStatus(filter.value)}
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
            还没有文章，先写一篇吧。
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
    </section>
  );
}
