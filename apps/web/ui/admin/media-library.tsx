"use client";

import Link from "next/link";
import { useRef, useState } from "react";
import useSWR from "swr";

import { ApiError } from "@/lib/api/fetcher";
import {
  adminMediaApi,
  type MediaItem,
  type MediaListResult,
} from "@/lib/api/media";
import { swrKeys } from "@/lib/swr/keys";
import { formatBytes } from "@/lib/utils";
import { Alert, AlertDescription } from "@/ui/shadcn/alert";
import { Button } from "@/ui/shadcn/button";
import { Loading } from "@/ui/shared/loading";

/** 每页图片数；与文章列表保持一致的翻页交互。 */
const PAGE_SIZE = 24;

/** 把 2002 的引用清单压成一句人话。 */
function describeRefs(details: unknown): string {
  if (!Array.isArray(details) || details.length === 0) {
    return "";
  }
  return details
    .map((entry) => {
      const ref = entry as { title?: string; kind?: string };
      return `《${ref.title ?? "未命名"}》（${ref.kind ?? "引用"}）`;
    })
    .join("、");
}

/**
 * 媒体库（D7）：流式上传、缩略图、复制 Markdown/链接、删除前引用检查。
 */
export function MediaLibrary() {
  const [page, setPage] = useState(1);
  const [uploading, setUploading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [copied, setCopied] = useState<string | null>(null);
  const inputRef = useRef<HTMLInputElement | null>(null);

  const { data, error: loadError, isLoading, mutate } = useSWR<
    MediaListResult,
    ApiError
  >(swrKeys.adminMedia(page, PAGE_SIZE), () =>
    adminMediaApi.list({ page, pageSize: PAGE_SIZE }),
  );

  const total = data?.total ?? 0;
  const totalPages = Math.max(1, Math.ceil(total / PAGE_SIZE));

  async function handleUpload(file: File) {
    setUploading(true);
    setError(null);
    try {
      await adminMediaApi.upload(file);
      setPage(1);
      await mutate();
    } catch (caught) {
      setError(caught instanceof ApiError ? caught.message : "上传失败，请稍后重试");
    } finally {
      setUploading(false);
      if (inputRef.current) {
        inputRef.current.value = "";
      }
    }
  }

  async function handleDelete(item: MediaItem) {
    if (!window.confirm(`确定删除 ${item.originalName}？`)) {
      return;
    }
    setError(null);
    try {
      await adminMediaApi.remove(item.id);
      await mutate();
    } catch (caught) {
      if (caught instanceof ApiError && caught.code === 2002) {
        setError(
          `图片仍被文章引用，不能删除：${describeRefs(caught.details) || "请先移除引用"}`,
        );
      } else {
        setError(caught instanceof ApiError ? caught.message : "删除失败，请稍后重试");
      }
    }
  }

  async function handleRetry(item: MediaItem) {
    setError(null);
    try {
      await adminMediaApi.retry(item.id);
      await mutate();
    } catch (caught) {
      setError(caught instanceof ApiError ? caught.message : "重新处理失败");
    }
  }

  async function copyText(text: string, key: string) {
    try {
      await navigator.clipboard.writeText(text);
      setCopied(key);
      window.setTimeout(() => setCopied(null), 1500);
    } catch {
      setError("复制失败，请手动选中链接复制");
    }
  }

  return (
    <section className="space-y-6">
      <header className="flex flex-wrap items-end justify-between gap-4">
        <div className="space-y-1">
          <h1 className="text-2xl font-semibold tracking-tight">媒体库</h1>
          <p className="text-sm text-muted-foreground">
            共 {total} 张；支持 JPEG / PNG / WebP / GIF，单张 ≤30MB、≤5000 万像素。
          </p>
        </div>
        <div className="flex items-center gap-3">
          <input
            ref={inputRef}
            id="media-upload"
            type="file"
            accept="image/jpeg,image/png,image/webp,image/gif"
            className="sr-only"
            onChange={(event) => {
              const file = event.target.files?.[0];
              if (file) {
                void handleUpload(file);
              }
            }}
          />
          <label
            htmlFor="media-upload"
            className="inline-flex h-10 cursor-pointer items-center rounded-md bg-primary px-4 text-sm font-medium text-primary-foreground hover:bg-primary/90 aria-disabled:opacity-50"
            aria-disabled={uploading}
          >
            {uploading ? "上传中…" : "上传图片"}
          </label>
          <Button asChild variant="outline">
            <Link href="/admin/posts">返回文章</Link>
          </Button>
        </div>
      </header>

      {error ? (
        <Alert variant="destructive">
          <AlertDescription>{error}</AlertDescription>
        </Alert>
      ) : null}

      {loadError ? (
        <Alert variant="destructive">
          <AlertDescription>{loadError.message}</AlertDescription>
        </Alert>
      ) : null}

      {isLoading ? <Loading label="正在加载图片…" /> : null}

      {data ? (
        data.list.length === 0 ? (
          <p className="rounded-lg border border-dashed border-border px-4 py-12 text-center text-sm text-muted-foreground">
            还没有图片，点右上角上传第一张。
          </p>
        ) : (
          <ul className="grid gap-4 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4">
            {data.list.map((item) => {
              const name = item.alt || item.originalName || "图片";
              const markdown = `![${name}](${item.url})`;
              return (
                <li
                  key={item.id}
                  className="overflow-hidden rounded-lg border border-border bg-card"
                >
                  <div className="flex aspect-[4/3] items-center justify-center bg-muted">
                    <img
                      src={item.url}
                      alt={name}
                      loading="lazy"
                      className="h-full w-full object-contain"
                    />
                  </div>
                  <div className="space-y-2 p-3">
                    <p className="truncate text-sm font-medium" title={item.originalName}>
                      {item.originalName}
                    </p>
                    <p className="text-xs text-muted-foreground">
                      {item.width}×{item.height} · {formatBytes(item.bytes)}
                      {item.status !== "ready" ? ` · ${item.status}` : ""}
                    </p>
                    <div className="flex flex-wrap gap-1">
                      <Button
                        size="sm"
                        variant="outline"
                        onClick={() => void copyText(markdown, `md-${item.id}`)}
                      >
                        {copied === `md-${item.id}` ? "已复制" : "复制 Markdown"}
                      </Button>
                      <Button
                        size="sm"
                        variant="outline"
                        onClick={() => void copyText(item.url, `url-${item.id}`)}
                      >
                        复制链接
                      </Button>
                      {item.status !== "ready" ? (
                        <Button
                          size="sm"
                          variant="outline"
                          onClick={() => void handleRetry(item)}
                        >
                          重试
                        </Button>
                      ) : null}
                      <Button
                        size="sm"
                        variant="destructive"
                        onClick={() => void handleDelete(item)}
                      >
                        删除
                      </Button>
                    </div>
                  </div>
                </li>
              );
            })}
          </ul>
        )
      ) : null}

      {data ? (
        <nav
          aria-label="分页"
          className="flex flex-wrap items-center justify-between gap-4 text-sm text-muted-foreground"
        >
          <span>
            共 {total} 张，第 {page} / {totalPages} 页
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
