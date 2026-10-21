"use client";

import Link from "next/link";
import { useMemo, useState } from "react";
import useSWR from "swr";

import type { ApiError } from "@/lib/api/fetcher";
import {
  adminStatsApi,
  type AdminStatsDaily,
  type AdminStatsOverview,
  type AdminTopPost,
} from "@/lib/api/stats";
import { swrKeys } from "@/lib/swr/keys";
import { Alert, AlertDescription } from "@/ui/shadcn/alert";
import { Button } from "@/ui/shadcn/button";
import { Card, CardContent, CardHeader, CardTitle } from "@/ui/shadcn/card";
import { Loading } from "@/ui/shared/loading";

/** 站点时区（agent.md §4）：stats_daily 的切日与日期标签都按上海时间。 */
const SITE_TIME_ZONE = "Asia/Shanghai";

/** 可选的统计区间；90 天已足够看趋势，远小于服务端 366 天的上限。 */
const RANGES = [7, 30, 90] as const;

type RangeDays = (typeof RANGES)[number];

const DAY_MS = 24 * 60 * 60 * 1000;

/** 横轴最多标几个日期，避免 90 根柱子时标签挤成一团。 */
const MAX_DATE_LABELS = 6;

/** 当天没有 PV 也要留一点高度，否则看不出"这天存在"。 */
const MIN_BAR_PERCENT = 2;

/** 柱状图的一个点：没有数据的日期由前端补零。 */
interface DailyPoint {
  date: string;
  pv: number;
  uv: number;
}

/** 把 Date 格式化成站点时区的 YYYY-MM-DD，与 stats_daily.date 同口径。 */
function toSiteDate(date: Date): string {
  const parts = new Intl.DateTimeFormat("en-CA", {
    timeZone: SITE_TIME_ZONE,
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
  }).formatToParts(date);
  const value = (type: "year" | "month" | "day") =>
    parts.find((part) => part.type === type)?.value ?? "";
  return `${value("year")}-${value("month")}-${value("day")}`;
}

/**
 * 区间 [今天-(days-1), 今天]。
 *
 * 按毫秒回推而不是做日期运算：上海没有夏令时，一天恒为 24 小时。
 */
function buildRange(days: RangeDays): { from: string; to: string } {
  const now = Date.now();
  return {
    from: toSiteDate(new Date(now - (days - 1) * DAY_MS)),
    to: toSiteDate(new Date(now)),
  };
}

/** 把区间展开成逐日列表；from 按 UTC 零点解析，加 24h 后回上海仍是同一天。 */
function enumerateDates(from: string, days: number): string[] {
  const start = Date.parse(`${from}T00:00:00Z`);
  if (Number.isNaN(start)) {
    return [];
  }
  const dates: string[] = [];
  for (let index = 0; index < days; index += 1) {
    dates.push(toSiteDate(new Date(start + index * DAY_MS)));
  }
  return dates;
}

/** 卡片里的一行计数。 */
interface CountRow {
  label: string;
  value: number;
}

/** 一组状态计数。 */
interface CountCard {
  title: string;
  rows: CountRow[];
}

/** 把看板数据摊平成卡片；顺序与运营视角一致（内容 → 互动 → 流量）。 */
function buildCountCards(data: AdminStatsOverview): CountCard[] {
  return [
    {
      title: "文章",
      rows: [
        { label: "已发布", value: data.posts.published },
        { label: "草稿", value: data.posts.draft },
        { label: "定时", value: data.posts.scheduled },
        { label: "归档", value: data.posts.archived },
        { label: "回收站", value: data.posts.deleted },
      ],
    },
    {
      title: "评论",
      rows: [
        { label: "待审核", value: data.comments.pending },
        { label: "已通过", value: data.comments.approved },
        { label: "垃圾", value: data.comments.spam },
      ],
    },
    {
      title: "媒体",
      rows: [
        { label: "就绪", value: data.media.ready },
        { label: "处理中", value: data.media.processing },
        { label: "失败", value: data.media.failed },
      ],
    },
    {
      title: "阅读",
      rows: [
        { label: "今日 PV", value: data.views.today },
        { label: "今日 UV", value: data.views.uvToday },
        { label: "累计 PV", value: data.views.total },
      ],
    },
    {
      title: "点赞",
      rows: [
        { label: "今日", value: data.likes.today },
        { label: "累计", value: data.likes.total },
      ],
    },
  ];
}

/**
 * 统计看板（D12）：状态计数 + 日粒度 PV/UV + 阅读 Top 5。
 *
 * 管理页是纯 CSR（agent.md §8 / C9），所以这里的"今天"取浏览器时钟；
 * 服务端返回的 from/to 与请求一致，缺数据的日期在前端补零。
 */
export function StatsDashboard() {
  const [days, setDays] = useState<RangeDays>(30);
  // 区间依赖"今天"，用 useMemo 固定在一次渲染内，避免同一帧里算出两个日期。
  const range = useMemo(() => buildRange(days), [days]);

  const {
    data: overview,
    error: overviewError,
    isLoading: overviewLoading,
  } = useSWR<AdminStatsOverview, ApiError>(swrKeys.adminStatsOverview, () =>
    adminStatsApi.overview(),
  );

  const {
    data: daily,
    error: dailyError,
    isLoading: dailyLoading,
  } = useSWR<AdminStatsDaily, ApiError>(
    swrKeys.adminStatsDaily(range.from, range.to),
    () => adminStatsApi.daily(range),
  );

  const points = useMemo<DailyPoint[]>(() => {
    const rows = new Map((daily?.list ?? []).map((row) => [row.date, row]));
    return enumerateDates(range.from, days).map((date) => {
      const row = rows.get(date);
      return { date, pv: row?.pv ?? 0, uv: row?.uv ?? 0 };
    });
  }, [daily, days, range.from]);

  const loadError = overviewError ?? dailyError;

  return (
    <section className="space-y-8">
      <header className="space-y-2">
        <h1 className="text-xl font-semibold">统计</h1>
        <p className="text-sm text-muted-foreground">
          计数按站点时区（Asia/Shanghai）切日；阅读与点赞为客户端打点的结果，
          与文章页展示的数字同源。
        </p>
      </header>

      {loadError ? (
        <Alert variant="destructive">
          <AlertDescription>{loadError.message}</AlertDescription>
        </Alert>
      ) : null}

      {(overviewLoading && !overview) || (dailyLoading && !daily) ? (
        <Loading />
      ) : null}

      {overview ? (
        <>
          <section className="space-y-3">
            <h2 className="text-base font-semibold">计数总览</h2>
            <CountCards data={overview} />
          </section>

          <TopPostsTable posts={overview.topPosts} />
        </>
      ) : null}

      <section className="space-y-3">
        <div className="flex flex-wrap items-end justify-between gap-3">
          <div className="space-y-1">
            <h2 className="text-base font-semibold">日粒度</h2>
            <p className="text-sm text-muted-foreground">
              {daily
                ? `区间合计 PV ${daily.totalPv} · UV ${daily.totalUv}（${daily.from} ~ ${daily.to}）`
                : "正在加载日粒度…"}
            </p>
          </div>
          <div
            className="flex flex-wrap items-center gap-2"
            role="group"
            aria-label="统计区间"
          >
            {RANGES.map((value) => (
              <Button
                key={value}
                type="button"
                size="sm"
                variant={days === value ? "default" : "outline"}
                aria-pressed={days === value}
                onClick={() => setDays(value)}
              >
                最近 {value} 天
              </Button>
            ))}
          </div>
        </div>

        {daily ? <DailyChart points={points} /> : null}
      </section>
    </section>
  );
}

/** 状态计数卡片组。 */
function CountCards({ data }: { data: AdminStatsOverview }) {
  const cards = buildCountCards(data);

  return (
    <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
      {cards.map((card) => (
        <Card key={card.title}>
          <CardHeader className="p-4 pb-2">
            <CardTitle className="text-base">{card.title}</CardTitle>
          </CardHeader>
          <CardContent className="p-4 pt-0">
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
          </CardContent>
        </Card>
      ))}
    </div>
  );
}

/**
 * 日粒度柱状图。
 *
 * 只用 div 的高度做柱：不引图表依赖（agent.md §17.2 白名单），
 * 高度是运行时算出来的，只能走内联 style。
 */
function DailyChart({ points }: { points: DailyPoint[] }) {
  const maxPv = Math.max(1, ...points.map((point) => point.pv));
  const labelStep = Math.max(1, Math.ceil(points.length / MAX_DATE_LABELS));

  return (
    <div className="space-y-2">
      <ol className="flex h-40 items-end gap-1" aria-label="每日 PV 柱状图">
        {points.map((point) => {
          const percent = Math.max(
            MIN_BAR_PERCENT,
            Math.round((point.pv / maxPv) * 100),
          );
          return (
            <li
              key={point.date}
              className="flex h-full flex-1 items-end"
              title={`${point.date} PV ${point.pv} / UV ${point.uv}`}
            >
              <div
                className="w-full rounded-t bg-primary/80"
                style={{ height: `${percent}%` }}
              />
            </li>
          );
        })}
      </ol>

      <ol className="flex gap-1 text-xs text-muted-foreground" aria-hidden="true">
        {points.map((point, index) => (
          <li key={point.date} className="flex-1 text-center">
            {index % labelStep === 0 ? point.date.slice(5) : ""}
          </li>
        ))}
      </ol>
    </div>
  );
}

/** 阅读 Top 5 表格。 */
function TopPostsTable({ posts }: { posts: AdminTopPost[] }) {
  return (
    <section className="space-y-3">
      <h2 className="text-base font-semibold">阅读 Top 5</h2>

      {posts.length === 0 ? (
        <p className="rounded-lg border border-dashed border-border px-4 py-8 text-center text-sm text-muted-foreground">
          还没有已发布文章的阅读数据。
        </p>
      ) : (
        <div className="overflow-x-auto rounded-lg border border-border">
          <table className="w-full text-sm">
            <thead className="bg-muted/40 text-left text-xs text-muted-foreground">
              <tr>
                <th scope="col" className="px-4 py-2 font-medium">
                  标题
                </th>
                <th scope="col" className="px-4 py-2 text-right font-medium">
                  阅读
                </th>
                <th scope="col" className="px-4 py-2 text-right font-medium">
                  点赞
                </th>
              </tr>
            </thead>
            <tbody>
              {posts.map((post) => (
                <tr key={post.id} className="border-t border-border">
                  <td className="px-4 py-2">
                    <Link
                      href={`/admin/posts/${post.id}`}
                      className="underline-offset-4 hover:underline"
                    >
                      {post.title || post.slug}
                    </Link>
                  </td>
                  <td className="px-4 py-2 text-right tabular-nums">
                    {post.viewCount}
                  </td>
                  <td className="px-4 py-2 text-right tabular-nums">
                    {post.likeCount}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </section>
  );
}
