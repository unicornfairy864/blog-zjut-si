"use client";

import { useEffect, useState } from "react";
import useSWR from "swr";

import { ApiError } from "@/lib/api/fetcher";
import { publicApi, type LikeState, type PostStats } from "@/lib/api/public";
import { swrKeys } from "@/lib/swr/keys";
import { Button } from "@/ui/shadcn/button";

export interface PostInteractionsProps {
  slug: string;
}

/** 未加载时的占位：SSR HTML 里绝不能出现真实计数（agent.md §17.1）。 */
const PLACEHOLDER = "—";

/** 把点赞失败翻译成人话；限流（code 1005）单独说，用户才知道等一会儿还有效。 */
function describeActionError(caught: unknown): string {
  if (caught instanceof ApiError && caught.code === 1005) {
    return "操作过于频繁，请稍后再试";
  }
  return "操作失败，请稍后再试";
}

/**
 * 文章互动条（D12）：阅读数 / 点赞数 + 点赞按钮。
 *
 * 计数一律客户端异步取（§17.1）：SSR 出来的 HTML 只有占位符，所以首屏
 * 不会有真实数字，也不会因为计数变化而让整页重新渲染。
 */
export function PostInteractions({ slug }: PostInteractionsProps) {
  const { data, error, mutate } = useSWR<PostStats, ApiError>(
    swrKeys.postStats(slug),
    () => publicApi.postStats(slug),
    // 文章存在性已由 SSR 校验过；读接口失败再自动重试只会放大限流。
    { shouldRetryOnError: false },
  );
  const [pending, setPending] = useState(false);
  const [actionError, setActionError] = useState<string | null>(null);

  useEffect(() => {
    // 打点只发生在浏览器，失败静默：统计不该影响阅读（§5「PV 明细」）。
    // 开发环境 StrictMode 会跑两次，服务端 10 分钟去重会挡掉重复计数。
    void publicApi
      .recordView({
        path: `/posts/${slug}`,
        slug,
        referrer: document.referrer || undefined,
      })
      .catch(() => undefined);
  }, [slug]);

  const liked = data?.liked ?? false;
  const viewCount = data ? String(data.viewCount) : PLACEHOLDER;
  const likeCount = data ? String(data.likeCount) : PLACEHOLDER;

  async function toggleLike() {
    if (!data || pending) {
      return;
    }
    setPending(true);
    setActionError(null);
    try {
      const next: LikeState = data.liked
        ? await publicApi.unlike(slug)
        : await publicApi.like(slug);
      // 点赞接口只回 liked / likeCount，阅读数沿用当前值；直接写回缓存，
      // 不必再补一次读请求（服务端返回的就是权威计数）。
      await mutate(
        { ...data, liked: next.liked, likeCount: next.likeCount },
        { revalidate: false },
      );
    } catch (caught) {
      setActionError(describeActionError(caught));
    } finally {
      setPending(false);
    }
  }

  return (
    <div className="flex flex-wrap items-center gap-x-4 gap-y-2 text-sm text-muted-foreground">
      <p className="flex items-center gap-x-3" aria-live="polite">
        <span>阅读 {viewCount}</span>
        <span aria-hidden="true">·</span>
        <span>点赞 {likeCount}</span>
      </p>

      <Button
        type="button"
        size="sm"
        variant={liked ? "outline" : "default"}
        onClick={() => void toggleLike()}
        // 计数没到手就不知道当前是赞还是取消，先禁用，避免点了没反应。
        disabled={pending || !data}
        aria-pressed={liked}
        aria-label="点赞"
      >
        {liked ? `已赞 ${likeCount}` : `点赞 ${likeCount}`}
      </Button>

      {actionError ? (
        <span role="status" className="text-xs text-destructive">
          {actionError}
        </span>
      ) : null}

      {/* 计数没取到就说清楚，并给一次重试：否则按钮永远是灰的，用户只会觉得"点了没反应"。 */}
      {error && !data ? (
        <span className="text-xs text-muted-foreground">
          统计加载失败{" "}
          <button
            type="button"
            className="underline underline-offset-4"
            onClick={() => void mutate()}
          >
            重试
          </button>
        </span>
      ) : null}
    </div>
  );
}
