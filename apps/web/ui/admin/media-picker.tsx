"use client";

import Link from "next/link";
import useSWR from "swr";

import type { ApiError } from "@/lib/api/fetcher";
import { adminMediaApi, type MediaItem, type MediaListResult } from "@/lib/api/media";
import { swrKeys } from "@/lib/swr/keys";
import { Alert, AlertDescription } from "@/ui/shadcn/alert";

export interface MediaPickerProps {
  /** 点中一张图后拿到完整条目：插正文用 `item.url`，选封面/OG 用 `item.id`。 */
  onPick: (item: MediaItem) => void;
  /** 取多少张；编辑器插正文要一排缩略图，选封面可以多给几张。 */
  pageSize?: number;
  /** 选中态（选封面/OG 时把当前那张标出来）。 */
  selectedId?: number | null;
}

/**
 * 编辑器里的图片选择器：点击缩略图回调完整条目。
 *
 * 只列 `ready` 的图：`processing` 的条目 URL 还是预测值，选进正文可能变成死链。
 */
export function MediaPicker({ onPick, pageSize = 12, selectedId = null }: MediaPickerProps) {
  const { data, error, isLoading } = useSWR<MediaListResult, ApiError>(
    swrKeys.adminMedia(1, pageSize),
    () => adminMediaApi.list({ page: 1, pageSize }),
  );

  if (error) {
    return (
      <Alert variant="destructive">
        <AlertDescription>{error.message}</AlertDescription>
      </Alert>
    );
  }

  if (isLoading) {
    return <p className="text-xs text-muted-foreground">正在加载媒体库…</p>;
  }

  if (!data || data.list.length === 0) {
    return (
      <p className="text-xs text-muted-foreground">
        媒体库还是空的，先去{" "}
        <Link href="/admin/media" className="underline underline-offset-4">
          媒体库
        </Link>{" "}
        上传一张。
      </p>
    );
  }

  const ready = data.list.filter((item) => item.status === "ready");
  if (ready.length === 0) {
    return (
      <p className="text-xs text-muted-foreground">
        图片还在转码，稍等片刻再打开这里。
      </p>
    );
  }

  return (
    <div className="space-y-2">
      <ul className="grid grid-cols-3 gap-2 sm:grid-cols-4">
        {ready.map((item) => {
          const name = item.alt || item.originalName || "图片";
          // 既没有 alt 也没有原文件名时，退到 id，避免出现"选择 图片"这种零信息量的可访问名。
          const label =
            item.alt || item.originalName
              ? `选择 ${name}`
              : `选择图片 #${item.id}`;
          const selected = item.id === selectedId;
          return (
            <li key={item.id}>
              <button
                type="button"
                aria-label={label}
                aria-pressed={selected}
                className={
                  selected
                    ? "block w-full overflow-hidden rounded-md border-2 border-primary focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2"
                    : "block w-full overflow-hidden rounded-md border border-border focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2"
                }
                onClick={() => onPick(item)}
              >
                <img
                  src={item.url}
                  alt={name}
                  loading="lazy"
                  className="h-20 w-full object-cover"
                />
              </button>
            </li>
          );
        })}
      </ul>
      <p className="text-xs text-muted-foreground">
        点击缩略图选择；更多图片见{" "}
        <Link href="/admin/media" className="underline underline-offset-4">
          媒体库
        </Link>
        。
      </p>
    </div>
  );
}
