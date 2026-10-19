"use client";

import { useState } from "react";

import type { PublicMedia } from "@/lib/api/public";
import { cn } from "@/lib/utils";

import { BlurhashPlaceholder } from "./blurhash-placeholder";

/** 变体档位，与 agent.md §10 的四档一致。 */
const TIERS = [480, 960, 1600, 2400] as const;

/** 默认 `sizes`：正文最大 768px，窄屏铺满。 */
const DEFAULT_SIZES = "(min-width: 1024px) 768px, 100vw";

/** 用 variants 拼 `srcset`；不足两档时不输出，避免浏览器选到缺失的档位。 */
function buildSrcSet(media: PublicMedia): string | undefined {
  const entries: Array<[number, string]> = [];
  for (const tier of TIERS) {
    const url = media.variants[String(tier)];
    if (url) {
      entries.push([tier, url]);
    }
  }
  if (entries.length < 2) {
    return undefined;
  }
  return entries.map(([tier, url]) => `${url} ${tier}w`).join(", ");
}

export interface PostImageProps {
  media: PublicMedia;
  sizes?: string;
  className?: string;
  /** 首屏封面用 eager，其余 lazy（agent.md §15）。 */
  priority?: boolean;
}

/**
 * 站点统一的图片组件（agent.md §8）：不使用 `next/image`，
 * 自己输出 `srcset`（480/960/1600/2400）+ blurhash 占位 + 固定宽高比避免抖动。
 */
export function PostImage({
  media,
  sizes = DEFAULT_SIZES,
  className,
  priority = false,
}: PostImageProps) {
  const [loaded, setLoaded] = useState(false);
  const srcSet = buildSrcSet(media);
  const ratio =
    media.width > 0 && media.height > 0
      ? `${media.width} / ${media.height}`
      : "16 / 9";

  return (
    <span
      className="relative block overflow-hidden rounded-lg bg-muted"
      style={{ aspectRatio: ratio }}
    >
      {!loaded && media.blurhash ? (
        <BlurhashPlaceholder
          hash={media.blurhash}
          className="absolute inset-0"
        />
      ) : null}
      <img
        src={media.url}
        srcSet={srcSet}
        sizes={srcSet ? sizes : undefined}
        width={media.width || undefined}
        height={media.height || undefined}
        alt={media.alt || ""}
        loading={priority ? "eager" : "lazy"}
        decoding={priority ? "sync" : "async"}
        fetchPriority={priority ? "high" : "auto"}
        onLoad={() => setLoaded(true)}
        className={cn(
          "relative h-full w-full object-contain transition-opacity duration-300",
          loaded ? "opacity-100" : "opacity-0",
          className,
        )}
      />
    </span>
  );
}
