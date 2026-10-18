"use client";

import Link from "next/link";
import useSWR from "swr";

import type { ApiError } from "@/lib/api/fetcher";
import { adminMediaApi, type MediaListResult } from "@/lib/api/media";
import { swrKeys } from "@/lib/swr/keys";
import { Alert, AlertDescription } from "@/ui/shadcn/alert";

/** 编辑器里的图片选择器：点击缩略图把 Markdown 插到光标处。 */
export function MediaPicker({
  onPick,
}: {
  onPick: (markdown: string) => void;
}) {
  const { data, error, isLoading } = useSWR<MediaListResult, ApiError>(
    swrKeys.adminMedia(1, 12),
    () => adminMediaApi.list({ page: 1, pageSize: 12 }),
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
          return (
            <li key={item.id}>
              <button
                type="button"
                aria-label={`插入 ${name}`}
                className="block w-full overflow-hidden rounded-md border border-border focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2"
                onClick={() => onPick(`![${name}](${item.url})`)}
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
        点击缩略图插入 Markdown；更多图片见{" "}
        <Link href="/admin/media" className="underline underline-offset-4">
          媒体库
        </Link>
        。
      </p>
    </div>
  );
}
