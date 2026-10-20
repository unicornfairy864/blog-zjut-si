import type { Metadata } from "next";
import { notFound } from "next/navigation";

import { isApiCode } from "@/lib/api/public";
import { publicPreviewApi, type PreviewPost } from "@/lib/api/preview";
import { formatDateTime } from "@/lib/utils";
import { PostStatusBadge } from "@/ui/admin/post-status-badge";
import { PostContent } from "@/ui/post/post-content";
import { Alert, AlertDescription, AlertTitle } from "@/ui/shadcn/alert";

/** 预览是访客态、且 token 一次性，绝不能进任何缓存（agent.md §8 / §12）。 */
export const dynamic = "force-dynamic";

export const metadata: Metadata = {
  title: "预览",
  robots: { index: false, follow: false, nocache: true },
};

interface PreviewPageProps {
  params: Promise<{ token: string }>;
}

/** token 是 64 位 hex；明显超长的直接当不存在，省一次必然 404 的往返。 */
const MAX_TOKEN_LENGTH = 128;

/**
 * 预览链接失效（`code 2006`，HTTP 200）的说明页。
 *
 * 一次性链接"已用过 / 已过期"是它的**正常归宿**，不是错误，所以这里正常 200 渲染，
 * 不抛异常也不 `notFound()`——否则作者只会看到一个没有上下文的 404。
 */
function ExpiredPreview() {
  return (
    <main className="container max-w-2xl space-y-4 py-16">
      <h1 className="text-2xl font-semibold tracking-tight">预览链接已失效</h1>
      <p className="text-sm leading-6 text-muted-foreground">
        这个链接是一次性的：已经打开过，或者生成后超过了 24 小时。
        请回到管理端重新生成一条预览链接。
      </p>
      <Alert>
        <AlertTitle>为什么会这样</AlertTitle>
        <AlertDescription>
          预览链接能读到草稿，所以设计成「看过即失效」，避免被转发或被爬虫捡走。
        </AlertDescription>
      </Alert>
    </main>
  );
}

/** 页头那行元信息：状态徽章 + 字数 / 阅读时长 + 更新时间与过期时间。 */
function PreviewMeta({ post }: { post: PreviewPost }) {
  return (
    <p className="flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-muted-foreground">
      <PostStatusBadge status={post.status} />
      <span>{post.wordCount} 字</span>
      <span>约 {post.readingTime} 分钟</span>
      <span>更新于 {formatDateTime(post.updatedAt)}</span>
      {post.publishedAt ? (
        <span>发布于 {formatDateTime(post.publishedAt)}</span>
      ) : null}
      <span>链接过期：{formatDateTime(post.expiresAt)}</span>
    </p>
  );
}

/**
 * 草稿预览页（/preview/{token}，D13）。
 *
 * 正文复用 `PostContent`，与发布结果同一条渲染管线（公式、代码复制按钮一致）；
 * 按 §17.1 与 D13 口径，这里**不显示评论，也不显示点赞 / 阅读数**。
 *
 * 取数只在服务端做一次：token 是一次性的，多调一次就会把链接用掉。
 * 所以 `generateMetadata` 不碰这个接口，标题保持静态。
 */
export default async function PreviewPage({ params }: PreviewPageProps) {
  const { token } = await params;
  if (!token || token.length > MAX_TOKEN_LENGTH) {
    notFound();
  }

  let post: PreviewPost;
  try {
    post = await publicPreviewApi.get(token);
  } catch (error) {
    if (isApiCode(error, 1002)) {
      notFound();
    }
    if (isApiCode(error, 1007)) {
      return <ExpiredPreview />;
    }
    // 其余错误（网络 / 5xx）交给 error.tsx 兜底，不要伪装成"链接失效"。
    throw error;
  }

  return (
    <main className="container max-w-3xl space-y-6 py-10">
      <Alert>
        <AlertTitle>一次性预览链接</AlertTitle>
        <AlertDescription>
          刷新或再次打开这条链接就会失效；确认没问题后请回到管理端发布。
        </AlertDescription>
      </Alert>

      <article className="space-y-6">
        <header className="space-y-3">
          <h1 className="break-words text-3xl font-semibold leading-tight tracking-tight">
            {post.title}
          </h1>
          <PreviewMeta post={post} />
          {post.excerpt ? (
            <p className="text-sm leading-6 text-muted-foreground">
              {post.excerpt}
            </p>
          ) : null}
        </header>

        <PostContent html={post.contentHtml} />
      </article>
    </main>
  );
}
