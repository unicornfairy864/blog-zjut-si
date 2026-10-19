"use client";

import Link from "next/link";
import { useState } from "react";
import useSWR from "swr";

import {
  adminCommentApi,
  type AdminCommentItem,
  type AdminCommentListResult,
  type CommentStatus,
} from "@/lib/api/comments";
import type { ApiError } from "@/lib/api/fetcher";
import { swrKeys } from "@/lib/swr/keys";
import { cn, formatDateTime } from "@/lib/utils";
import { Alert, AlertDescription } from "@/ui/shadcn/alert";
import { Badge } from "@/ui/shadcn/badge";
import { Button } from "@/ui/shadcn/button";
import { Loading } from "@/ui/shared/loading";

/** 每页条数；审核队列一屏够看。 */
const PAGE_SIZE = 20;

/** 状态筛选；all 表示不过滤。 */
const STATUS_TABS = [
  { value: "pending", label: "待审核" },
  { value: "approved", label: "已通过" },
  { value: "spam", label: "垃圾" },
  { value: "deleted", label: "已删除" },
  { value: "all", label: "全部" },
] as const;

/** 状态中文名与徽章样式。 */
const STATUS_LABEL: Record<string, string> = {
  pending: "待审核",
  approved: "已通过",
  spam: "垃圾",
  deleted: "已删除",
};

/**
 * 评论审核（D11）：筛选、单条改状态、批量改状态。
 *
 * 只有 `approved` 会出现在公开页面；`deleted` 是软删，记录仍保留。
 */
export function CommentModeration() {
  const [status, setStatus] = useState<string>("pending");
  const [page, setPage] = useState(1);
  const [selected, setSelected] = useState<number[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  const { data, error: loadError, isLoading, mutate } = useSWR<
    AdminCommentListResult,
    ApiError
  >(swrKeys.adminComments(status, page, PAGE_SIZE), () =>
    adminCommentApi.list({ status, page, pageSize: PAGE_SIZE }),
  );

  const items = data?.list ?? [];
  const total = data?.total ?? 0;
  const totalPages = Math.max(1, Math.ceil(total / PAGE_SIZE));
  const allSelected = items.length > 0 && selected.length === items.length;

  function switchStatus(next: string) {
    setStatus(next);
    setPage(1);
    setSelected([]);
    setError(null);
  }

  function toggle(id: number) {
    setSelected((current) =>
      current.includes(id)
        ? current.filter((item) => item !== id)
        : [...current, id],
    );
  }

  async function applyStatus(ids: number[], next: CommentStatus) {
    if (ids.length === 0) {
      return;
    }
    setBusy(true);
    setError(null);
    try {
      if (ids.length === 1) {
        await adminCommentApi.patch(ids[0], next);
      } else {
        await adminCommentApi.bulk(ids, next);
      }
      setSelected([]);
      await mutate();
    } catch (caught) {
      setError(
        caught instanceof Error ? caught.message : "操作失败，请稍后重试",
      );
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="space-y-6">
      <header className="space-y-2">
        <h1 className="text-xl font-semibold">评论</h1>
        <p className="text-sm text-muted-foreground">
          只有「已通过」会出现在公开页面；「已删除」是软删，记录仍保留。
        </p>
      </header>

      <div className="flex flex-wrap items-center gap-2">
        {STATUS_TABS.map((tab) => (
          <Button
            key={tab.value}
            size="sm"
            variant={status === tab.value ? "default" : "outline"}
            onClick={() => switchStatus(tab.value)}
          >
            {tab.label}
          </Button>
        ))}
      </div>

      {error ? (
        <Alert variant="destructive">
          <AlertDescription>{error}</AlertDescription>
        </Alert>
      ) : null}

      {selected.length > 0 ? (
        <div className="flex flex-wrap items-center gap-2 rounded-lg border border-border bg-muted/40 px-3 py-2">
          <span className="text-sm">已选 {selected.length} 条</span>
          <Button
            size="sm"
            onClick={() => void applyStatus(selected, "approved")}
            disabled={busy}
          >
            批量通过
          </Button>
          <Button
            size="sm"
            variant="outline"
            onClick={() => void applyStatus(selected, "spam")}
            disabled={busy}
          >
            批量标记垃圾
          </Button>
          <Button
            size="sm"
            variant="destructive"
            onClick={() => void applyStatus(selected, "deleted")}
            disabled={busy}
          >
            批量删除
          </Button>
          <Button
            size="sm"
            variant="ghost"
            onClick={() => setSelected([])}
            disabled={busy}
          >
            取消选择
          </Button>
        </div>
      ) : null}

      {isLoading && !data ? <Loading /> : null}

      {loadError ? (
        <Alert variant="destructive">
          <AlertDescription>{loadError.message}</AlertDescription>
        </Alert>
      ) : null}

      {data ? (
        items.length === 0 ? (
          <p className="rounded-lg border border-dashed border-border px-4 py-12 text-center text-sm text-muted-foreground">
            这个状态下没有评论。
          </p>
        ) : (
          <ul className="space-y-3">
            <li className="flex items-center gap-2 text-xs text-muted-foreground">
              <input
                type="checkbox"
                checked={allSelected}
                onChange={() =>
                  setSelected(allSelected ? [] : items.map((item) => item.id))
                }
                aria-label="全选本页"
                className="h-4 w-4"
              />
              全选本页
            </li>
            {items.map((comment) => (
              <CommentRow
                key={comment.id}
                comment={comment}
                selected={selected.includes(comment.id)}
                busy={busy}
                onToggle={() => toggle(comment.id)}
                onStatus={(next) => void applyStatus([comment.id], next)}
              />
            ))}
          </ul>
        )
      ) : null}

      {data && totalPages > 1 ? (
        <nav
          aria-label="分页"
          className="flex items-center justify-between gap-4 text-sm"
        >
          <Button
            size="sm"
            variant="outline"
            disabled={page <= 1}
            onClick={() => setPage((current) => Math.max(1, current - 1))}
          >
            上一页
          </Button>
          <span className="text-xs text-muted-foreground">
            第 {page} / {totalPages} 页 · 共 {total} 条
          </span>
          <Button
            size="sm"
            variant="outline"
            disabled={page >= totalPages}
            onClick={() => setPage((current) => current + 1)}
          >
            下一页
          </Button>
        </nav>
      ) : null}
    </div>
  );
}

interface CommentRowProps {
  comment: AdminCommentItem;
  selected: boolean;
  busy: boolean;
  onToggle: () => void;
  onStatus: (status: CommentStatus) => void;
}

/** 单条评论：作者信息 + 正文 + 状态操作。 */
function CommentRow({
  comment,
  selected,
  busy,
  onToggle,
  onStatus,
}: CommentRowProps) {
  return (
    <li className="space-y-3 rounded-lg border border-border p-4">
      <div className="flex flex-wrap items-start gap-3">
        <input
          type="checkbox"
          checked={selected}
          onChange={onToggle}
          aria-label={`选择 ${comment.authorName} 的评论`}
          className="mt-1 h-4 w-4"
        />
        <div className="min-w-0 flex-1 space-y-1">
          <p className="flex flex-wrap items-center gap-x-3 gap-y-1 text-sm">
            <span className="font-medium">{comment.authorName}</span>
            {comment.authorEmail ? (
              <span className="text-xs text-muted-foreground">
                {comment.authorEmail}
              </span>
            ) : null}
            {comment.authorUrl ? (
              <a
                href={comment.authorUrl}
                target="_blank"
                rel="noopener noreferrer nofollow"
                className="text-xs text-muted-foreground underline-offset-4 hover:underline"
              >
                {comment.authorUrl}
              </a>
            ) : null}
            <time className="text-xs text-muted-foreground">
              {formatDateTime(comment.createdAt)}
            </time>
            <Badge
              variant={comment.status === "approved" ? "default" : "outline"}
            >
              {STATUS_LABEL[comment.status] ?? comment.status}
            </Badge>
          </p>
          <p className="text-xs text-muted-foreground">
            来自{" "}
            <Link
              href={`/posts/${comment.postSlug}`}
              target="_blank"
              className="underline-offset-4 hover:underline"
            >
              《{comment.postTitle || comment.postSlug}》
            </Link>
            {comment.parentId ? ` · 回复 #${comment.parentId}` : ""}
          </p>
        </div>
      </div>

      <div
        className={cn("prose-comment rounded-md bg-muted/40 p-3")}
        // 服务端渲染 + bluemonday 过滤后的 HTML（§9 / §11）。
        dangerouslySetInnerHTML={{ __html: comment.contentHtml }}
      />

      <div className="flex flex-wrap gap-2">
        <Button
          size="sm"
          onClick={() => onStatus("approved")}
          disabled={busy || comment.status === "approved"}
        >
          通过
        </Button>
        <Button
          size="sm"
          variant="outline"
          onClick={() => onStatus("spam")}
          disabled={busy || comment.status === "spam"}
        >
          标记垃圾
        </Button>
        <Button
          size="sm"
          variant="destructive"
          onClick={() => onStatus("deleted")}
          disabled={busy || comment.status === "deleted"}
        >
          删除
        </Button>
      </div>
    </li>
  );
}
