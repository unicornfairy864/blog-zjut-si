"use client";

import { useState, type FormEvent } from "react";

import { ApiError } from "@/lib/api/fetcher";
import {
  publicApi,
  type CommentCreateInput,
  type PublicComment,
} from "@/lib/api/public";
import { formatDateTime } from "@/lib/utils";
import { Alert, AlertDescription } from "@/ui/shadcn/alert";
import { Button } from "@/ui/shadcn/button";
import { Input } from "@/ui/shadcn/input";
import { Textarea } from "@/ui/shadcn/textarea";

/** 与服务端一致的字段上限（agent.md §5）。 */
const NAME_MAX = 50;
const CONTENT_MAX = 2000;

export interface CommentSectionProps {
  slug: string;
  total: number;
  comments: PublicComment[];
  /** 文章是否开放评论；关闭时只展示已有评论。 */
  allowComments: boolean;
}

/** 把接口错误翻译成人话，429 单独提示。 */
function describeError(caught: unknown): string {
  if (caught instanceof ApiError) {
    if (caught.status === 429 || caught.code === 1005) {
      return "评论太频繁了，请 10 分钟后再试。";
    }
    return caught.message;
  }
  return "提交失败，请稍后重试。";
}

interface CommentFormProps {
  slug: string;
  /** 顶层评论为 null；回复时传被回复的评论 id。 */
  parentId: number | null;
  onCancel?: () => void;
}

/** 评论表单：顶层与回复共用；提交成功后留在原地提示"待审核"（新评论不会立刻公开）。 */
function CommentForm({ slug, parentId, onCancel }: CommentFormProps) {
  const [authorName, setAuthorName] = useState("");
  const [authorEmail, setAuthorEmail] = useState("");
  const [authorUrl, setAuthorUrl] = useState("");
  const [contentMd, setContentMd] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [done, setDone] = useState(false);

  async function handleSubmit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    setError(null);

    if (!authorName.trim()) {
      setError("请填写昵称。");
      return;
    }
    if (!contentMd.trim()) {
      setError("请填写评论内容。");
      return;
    }

    const payload: CommentCreateInput = {
      authorName: authorName.trim(),
      contentMd: contentMd.trim(),
      parentId,
    };
    if (authorEmail.trim()) {
      payload.authorEmail = authorEmail.trim();
    }
    if (authorUrl.trim()) {
      payload.authorUrl = authorUrl.trim();
    }

    setSubmitting(true);
    try {
      await publicApi.createComment(slug, payload);
      setContentMd("");
      setDone(true);
    } catch (caught) {
      setError(describeError(caught));
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <form onSubmit={handleSubmit} className="space-y-3">
      <div className="grid gap-3 sm:grid-cols-3">
        <Input
          value={authorName}
          onChange={(event) => setAuthorName(event.target.value)}
          maxLength={NAME_MAX}
          placeholder="昵称（必填）"
          aria-label="昵称"
          required
        />
        <Input
          value={authorEmail}
          onChange={(event) => setAuthorEmail(event.target.value)}
          type="email"
          maxLength={200}
          placeholder="邮箱（选填，不公开）"
          aria-label="邮箱"
        />
        <Input
          value={authorUrl}
          onChange={(event) => setAuthorUrl(event.target.value)}
          type="url"
          maxLength={200}
          placeholder="网址（选填）"
          aria-label="网址"
        />
      </div>

      <Textarea
        value={contentMd}
        onChange={(event) => setContentMd(event.target.value)}
        maxLength={CONTENT_MAX}
        rows={parentId ? 3 : 5}
        placeholder="说点什么…支持 Markdown"
        aria-label="评论内容"
        required
      />

      {error ? (
        <Alert variant="destructive">
          <AlertDescription>{error}</AlertDescription>
        </Alert>
      ) : null}

      {done ? (
        <Alert>
          <AlertDescription>
            评论已提交，审核通过后展示。
          </AlertDescription>
        </Alert>
      ) : null}

      <div className="flex items-center gap-2">
        <Button type="submit" size="sm" disabled={submitting}>
          {submitting ? "提交中…" : parentId ? "回复" : "发表评论"}
        </Button>
        {onCancel ? (
          <Button
            type="button"
            size="sm"
            variant="ghost"
            onClick={onCancel}
            disabled={submitting}
          >
            取消
          </Button>
        ) : null}
        <span className="text-xs text-muted-foreground">
          邮箱仅用于识别，不会公开。
        </span>
      </div>
    </form>
  );
}

interface CommentItemProps {
  slug: string;
  comment: PublicComment;
}

/** 一条评论（顶层）及其回复。 */
function CommentItem({ slug, comment }: CommentItemProps) {
  const [replying, setReplying] = useState(false);

  return (
    <li className="space-y-3 border-t border-border pt-4 first:border-t-0 first:pt-0">
      <CommentBody comment={comment} />

      {comment.replies && comment.replies.length > 0 ? (
        <ul className="space-y-4 border-l border-border pl-4">
          {comment.replies.map((reply) => (
            <li key={reply.id} className="space-y-1">
              <CommentBody comment={reply} />
            </li>
          ))}
        </ul>
      ) : null}

      {replying ? (
        <CommentForm
          slug={slug}
          parentId={comment.id}
          onCancel={() => setReplying(false)}
        />
      ) : (
        <Button
          type="button"
          size="sm"
          variant="ghost"
          onClick={() => setReplying(true)}
        >
          回复
        </Button>
      )}
    </li>
  );
}

/** 评论正文 + 作者行；正文是服务端渲染并过滤过的 HTML。 */
function CommentBody({ comment }: { comment: PublicComment }) {
  return (
    <div className="space-y-1">
      <p className="flex flex-wrap items-center gap-x-3 text-xs text-muted-foreground">
        {comment.authorUrl ? (
          <a
            href={comment.authorUrl}
            target="_blank"
            rel="noopener noreferrer nofollow"
            className="font-medium text-foreground underline-offset-4 hover:underline"
          >
            {comment.authorName}
          </a>
        ) : (
          <span className="font-medium text-foreground">
            {comment.authorName}
          </span>
        )}
        <time dateTime={formatDateTime(comment.createdAt)}>
          {formatDateTime(comment.createdAt)}
        </time>
      </p>
      <div
        className="prose-comment"
        // 内容由服务端 goldmark + bluemonday 渲染，不是用户直传 HTML（§9 / §11）。
        dangerouslySetInnerHTML={{ __html: comment.contentHtml }}
      />
    </div>
  );
}

/** 评论区：列表（SSR 传入）+ 表单（客户端提交）。 */
export function CommentSection({
  slug,
  total,
  comments,
  allowComments,
}: CommentSectionProps) {
  return (
    <section id="comments" className="space-y-6" aria-label="评论">
      <h2 className="text-lg font-semibold">
        评论
        <span className="ml-2 text-xs font-normal text-muted-foreground">
          {total} 条
        </span>
      </h2>

      {comments.length === 0 ? (
        <p className="rounded-lg border border-dashed border-border px-4 py-8 text-center text-sm text-muted-foreground">
          还没有评论，来说第一句。
        </p>
      ) : (
        <ul className="space-y-6">
          {comments.map((comment) => (
            <CommentItem key={comment.id} slug={slug} comment={comment} />
          ))}
        </ul>
      )}

      {allowComments ? (
        <div className="space-y-3 border-t border-border pt-6">
          <h3 className="text-sm font-medium">发表评论</h3>
          <CommentForm slug={slug} parentId={null} />
        </div>
      ) : (
        <p className="border-t border-border pt-6 text-sm text-muted-foreground">
          这篇文章已关闭评论。
        </p>
      )}
    </section>
  );
}
