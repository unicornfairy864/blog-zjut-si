import Link from "next/link";

import type { PublicPostListItem } from "@/lib/api/public";
import { formatDate } from "@/lib/utils";

import { PostImage } from "./post-image";

export interface PostCardProps {
  post: PublicPostListItem;
  /** 首屏第一张封面用 eager 加载。 */
  priority?: boolean;
}

/** 列表里的单篇文章卡片（首页 / 列表 / 标签 / 系列共用）。 */
export function PostCard({ post, priority = false }: PostCardProps) {
  return (
    <article className="border-b border-border pb-8 last:border-b-0">
      {post.cover ? (
        <Link
          href={`/posts/${post.slug}`}
          className="mb-4 block"
          tabIndex={-1}
          aria-hidden="true"
        >
          <PostImage media={post.cover} priority={priority} />
        </Link>
      ) : null}

      <h2 className="text-xl font-semibold leading-snug">
        <Link
          href={`/posts/${post.slug}`}
          className="underline-offset-4 hover:underline"
        >
          {post.title}
        </Link>
      </h2>

      <p className="mt-2 flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-muted-foreground">
        <time dateTime={formatDate(post.publishedAt)}>
          {formatDate(post.publishedAt)}
        </time>
        <span>{post.readingTime} 分钟</span>
        <span>{post.wordCount} 字</span>
        {post.isPinned ? (
          <span className="rounded border border-border px-1.5 py-0.5">
            置顶
          </span>
        ) : null}
      </p>

      {post.excerpt ? (
        <p className="mt-3 text-sm leading-6 text-muted-foreground">
          {post.excerpt}
        </p>
      ) : null}

      {post.tags.length > 0 ? (
        <ul className="mt-3 flex flex-wrap gap-2 text-xs">
          {post.tags.map((tag) => (
            <li key={tag.slug}>
              <Link
                href={`/tags/${tag.slug}`}
                className="rounded-full bg-muted px-2 py-0.5 text-muted-foreground hover:text-foreground"
              >
                #{tag.name}
              </Link>
            </li>
          ))}
        </ul>
      ) : null}
    </article>
  );
}
