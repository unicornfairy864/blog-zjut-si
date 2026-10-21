"use client";

import Link from "next/link";
import useSWR from "swr";

import type { ApiError } from "@/lib/api/fetcher";
import { adminStatsApi, type AdminStatsOverview } from "@/lib/api/stats";
import { swrKeys } from "@/lib/swr/keys";
import { Alert, AlertDescription } from "@/ui/shadcn/alert";
import { Button } from "@/ui/shadcn/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/ui/shadcn/card";
import { Loading } from "@/ui/shared/loading";

/** 仪表盘上的一格计数：数字 + 说明 + 跳转入口。 */
interface CountCard {
  title: string;
  href: string;
  rows: { label: string; value: number }[];
}

/** 只挑运营每天要看的那几个数；全量明细在 `/admin/stats`。 */
function buildCards(data: AdminStatsOverview): CountCard[] {
  return [
    {
      title: "内容",
      href: "/admin/posts",
      rows: [
        { label: "已发布", value: data.posts.published },
        { label: "草稿", value: data.posts.draft },
        { label: "定时", value: data.posts.scheduled },
        { label: "回收站", value: data.posts.deleted },
      ],
    },
    {
      title: "互动",
      href: "/admin/stats",
      rows: [
        { label: "今日 PV", value: data.views.today },
        { label: "今日 UV", value: data.views.uvToday },
        { label: "累计 PV", value: data.views.total },
        { label: "点赞", value: data.likes.total },
      ],
    },
    {
      title: "待处理",
      href: "/admin/comments",
      rows: [
        { label: "待审核评论", value: data.comments.pending },
        { label: "转码中图片", value: data.media.processing },
        { label: "转码失败", value: data.media.failed },
      ],
    },
  ];
}

/**
 * 管理端仪表盘：直接展示看板计数（D12 的 `GET /stats/overview`）。
 *
 * 之前这里只有一段静态文案，运营进后台看不到任何数字（接口早就有了）。
 */
export default function AdminDashboardPage() {
  const { data, error, isLoading } = useSWR<AdminStatsOverview, ApiError>(
    swrKeys.adminStatsOverview,
    () => adminStatsApi.overview(),
  );

  return (
    <section className="space-y-6">
      <header className="space-y-1">
        <h1 className="text-2xl font-semibold tracking-tight">仪表盘</h1>
        <p className="text-sm text-muted-foreground">
          内容与互动概览；完整图表与 Top 榜在{" "}
          <Link href="/admin/stats" className="underline underline-offset-4">
            统计
          </Link>
          。
        </p>
      </header>

      {isLoading && !data ? <Loading /> : null}

      {error ? (
        <Alert variant="destructive">
          <AlertDescription>{error.message}</AlertDescription>
        </Alert>
      ) : null}

      {data ? (
        <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
          {buildCards(data).map((card) => (
            <Card key={card.title}>
              <CardHeader className="p-4 pb-2">
                <CardTitle className="text-base">{card.title}</CardTitle>
              </CardHeader>
              <CardContent className="space-y-3 p-4 pt-0">
                <dl className="space-y-1.5 text-sm">
                  {card.rows.map((row) => (
                    <div
                      key={row.label}
                      className="flex items-baseline justify-between gap-4"
                    >
                      <dt className="text-muted-foreground">{row.label}</dt>
                      <dd className="font-medium tabular-nums">{row.value}</dd>
                    </div>
                  ))}
                </dl>
                <Button asChild size="sm" variant="outline">
                  <Link href={card.href}>查看</Link>
                </Button>
              </CardContent>
            </Card>
          ))}

          {data.topPosts.length > 0 ? (
            <Card className="sm:col-span-2 lg:col-span-3">
              <CardHeader className="p-4 pb-2">
                <CardTitle className="text-base">阅读最多的文章</CardTitle>
              </CardHeader>
              <CardContent className="p-4 pt-0">
                <ol className="space-y-2 text-sm">
                  {data.topPosts.map((post, index) => (
                    <li
                      key={post.id}
                      className="flex items-baseline justify-between gap-4"
                    >
                      <span className="min-w-0 truncate">
                        <span className="mr-2 text-muted-foreground">
                          {index + 1}.
                        </span>
                        <Link
                          href={`/admin/posts/${post.id}`}
                          className="underline-offset-4 hover:underline"
                        >
                          {post.title}
                        </Link>
                      </span>
                      <span className="shrink-0 tabular-nums text-muted-foreground">
                        {post.viewCount} 阅读 · {post.likeCount} 赞
                      </span>
                    </li>
                  ))}
                </ol>
              </CardContent>
            </Card>
          ) : null}
        </div>
      ) : null}
    </section>
  );
}
