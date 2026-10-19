"use client";

import {
  CalendarClock,
  Check,
  Copy,
  History,
  Link2,
  RotateCcw,
  Trash2,
} from "lucide-react";
import { useState } from "react";
import useSWR from "swr";

import { ApiError } from "@/lib/api/fetcher";
import {
  adminPostApi,
  type PostDetail,
  type PostRevision,
  type PreviewLink,
} from "@/lib/api/posts";
import { swrKeys } from "@/lib/swr/keys";
import { formatDateTime } from "@/lib/utils";
import { Alert, AlertDescription } from "@/ui/shadcn/alert";
import { Button } from "@/ui/shadcn/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/ui/shadcn/card";
import { Input } from "@/ui/shadcn/input";
import { Label } from "@/ui/shadcn/label";
import { Loading } from "@/ui/shared/loading";

/** 同一时刻只允许一个动作在跑；写操作都有副作用，重复点击没有意义。 */
type PublishingAction =
  | "schedule"
  | "cancel"
  | "restore"
  | "snapshot"
  | "link"
  | "revoke";

export interface PostPublishingProps {
  /** null 表示草稿还没保存过——这些接口都要文章 id，只能先提示保存。 */
  post: PostDetail | null;
  /** 成功后的权威结果交回编辑器，由它刷新表单与状态徽章。 */
  onSaved: (post: PostDetail) => void;
  /** 编辑器正在自动保存 / 发布时，这里也要跟着禁用。 */
  disabled?: boolean;
}

/**
 * 把 Unix 秒写成站点时区（Asia/Shanghai）的 `YYYY-MM-DD HH:mm`。
 *
 * 不用 `formatDateTime`：它给的是 `2026/10/20 10:30`，而"将于 … 发布"这类提示
 * 用 ISO 风格更易读，也顺手省掉秒。
 */
function formatScheduleAt(unix: number): string {
  const parts = new Intl.DateTimeFormat("en-CA", {
    timeZone: "Asia/Shanghai",
    year: "numeric",
    month: "2-digit",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    hour12: false,
  }).formatToParts(new Date(unix * 1000));
  const pick = (type: Intl.DateTimeFormatPartTypes) =>
    parts.find((part) => part.type === type)?.value ?? "";
  return `${pick("year")}-${pick("month")}-${pick("day")} ${pick("hour")}:${pick("minute")}`;
}

/** 只展示 ApiError 的人话 message，其他异常给兜底文案（不把内部错误抛给作者）。 */
function errorMessage(error: unknown, fallback: string): string {
  return error instanceof ApiError ? error.message : fallback;
}

/**
 * 发布控制（D13）：定时发布、版本回滚、一次性预览链接。
 *
 * 三块都挂在编辑器里，但都只在文章已保存（有 id）后才有意义；
 * 预览链接的明文 token 只回一次，所以生成后必须立刻给作者复制/作废的入口。
 */
export function PostPublishing({
  post,
  onSaved,
  disabled = false,
}: PostPublishingProps) {
  const postId = post?.id ?? null;

  const [scheduleAt, setScheduleAt] = useState("");
  const [scheduleError, setScheduleError] = useState<string | null>(null);
  const [revisionError, setRevisionError] = useState<string | null>(null);
  const [previewError, setPreviewError] = useState<string | null>(null);
  const [previewLink, setPreviewLink] = useState<PreviewLink | null>(null);
  /** 绝对地址只能在浏览器里拼（`window.location.origin`），所以生成时算好存起来。 */
  const [previewUrl, setPreviewUrl] = useState("");
  const [copied, setCopied] = useState(false);
  const [action, setAction] = useState<PublishingAction | null>(null);

  const {
    data: revisions,
    error: revisionsLoadError,
    isLoading: revisionsLoading,
    mutate: mutateRevisions,
  } = useSWR<{ list: PostRevision[] }, ApiError>(
    // 没保存过草稿就没有 id，传 null 让 SWR 不发请求（而不是拿 0 去撞接口）。
    postId ? swrKeys.adminPostRevisions(postId) : null,
    () => adminPostApi.revisions(postId as number),
  );

  const busy = disabled || action !== null;

  /** 定时发布：先在前端拦掉空值与过去时间，省一次注定 400 的往返。 */
  async function handleSchedule() {
    if (!post) {
      return;
    }
    const seconds = scheduleAt
      ? Math.floor(new Date(scheduleAt).getTime() / 1000)
      : Number.NaN;
    if (!scheduleAt || Number.isNaN(seconds)) {
      setScheduleError("请选择发布时间");
      return;
    }
    if (seconds <= Math.floor(Date.now() / 1000)) {
      setScheduleError("发布时间必须晚于当前时间");
      return;
    }

    setAction("schedule");
    setScheduleError(null);
    try {
      onSaved(await adminPostApi.schedule(post.id, { publishedAt: seconds }));
      setScheduleAt("");
    } catch (caught) {
      setScheduleError(errorMessage(caught, "定时发布失败，请稍后重试"));
    } finally {
      setAction(null);
    }
  }

  /** 取消定时：复用 unpublish（服务端同时撤掉待执行的 publish_scheduled 任务）。 */
  async function handleCancelSchedule() {
    if (!post) {
      return;
    }
    setAction("cancel");
    setScheduleError(null);
    try {
      onSaved(await adminPostApi.unpublish(post.id));
    } catch (caught) {
      setScheduleError(errorMessage(caught, "取消定时失败，请稍后重试"));
    } finally {
      setAction(null);
    }
  }

  /** 回滚：覆盖正文是不可逆的观感，所以二次确认；note 记来源，历史不被改写。 */
  async function handleRestore(revision: PostRevision) {
    if (!post) {
      return;
    }
    const label = revision.title || "（无标题）";
    const confirmed = window.confirm(
      `回滚到版本 #${revision.revisionNo}《${label}》？\n当前正文会被该版本覆盖（服务端会再存一条新版本，历史不会丢）。`,
    );
    if (!confirmed) {
      return;
    }

    setAction("restore");
    setRevisionError(null);
    try {
      onSaved(
        await adminPostApi.restoreRevision(
          post.id,
          revision.id,
          `回滚到版本 #${revision.revisionNo}`,
        ),
      );
      // 回滚本身也会写一条新 revision，列表必须重新取。
      await mutateRevisions();
    } catch (caught) {
      setRevisionError(errorMessage(caught, "回滚失败，请稍后重试"));
    } finally {
      setAction(null);
    }
  }

  /**
   * 为线上内容记一个新版本：复用 publish——服务端只在内容与最新版本不同时才写快照，
   * 内容没变会返回 409/1004（那正是"重复发布"的定义）。
   */
  async function handleSnapshot() {
    if (!post) {
      return;
    }
    setAction("snapshot");
    setRevisionError(null);
    try {
      onSaved(await adminPostApi.publish(post.id, "为线上改动记新版本"));
      await mutateRevisions();
    } catch (caught) {
      setRevisionError(errorMessage(caught, "记录新版本失败，请稍后重试"));
    } finally {
      setAction(null);
    }
  }

  async function handleCreateLink() {    if (!post) {
      return;
    }
    setAction("link");
    setPreviewError(null);
    setCopied(false);
    try {
      const link = await adminPostApi.createPreviewLink(post.id);
      setPreviewLink(link);
      setPreviewUrl(`${window.location.origin}${link.url}`);
    } catch (caught) {
      setPreviewError(errorMessage(caught, "生成预览链接失败，请稍后重试"));
    } finally {
      setAction(null);
    }
  }

  async function handleCopyLink() {
    const clipboard = navigator.clipboard;
    // http 隧道下 clipboard 可能不可用，此时明确提示手抄，别假装复制成功。
    if (!clipboard) {
      setPreviewError("当前环境不支持自动复制，请手动选中链接复制");
      return;
    }
    try {
      await clipboard.writeText(previewUrl);
      setCopied(true);
    } catch {
      setPreviewError("复制失败，请手动选中链接复制");
    }
  }

  async function handleRevokeLink() {
    if (!previewLink) {
      return;
    }
    setAction("revoke");
    setPreviewError(null);
    try {
      await adminPostApi.deletePreviewLink(previewLink.token);
      setPreviewLink(null);
      setPreviewUrl("");
      setCopied(false);
    } catch (caught) {
      setPreviewError(errorMessage(caught, "作废失败，请稍后重试"));
    } finally {
      setAction(null);
    }
  }

  if (!post) {
    return (
      <Card>
        <CardHeader className="p-4">
          <CardTitle className="text-base">发布控制</CardTitle>
          <CardDescription>
            先保存草稿，才能定时发布、回滚版本或生成预览链接。
          </CardDescription>
        </CardHeader>
      </Card>
    );
  }

  return (
    <section aria-label="发布控制" className="space-y-4">
      <Card>
        <CardHeader className="p-4">
          <CardTitle className="flex items-center gap-2 text-base">
            <CalendarClock className="h-4 w-4" aria-hidden="true" />
            定时发布
          </CardTitle>
          <CardDescription>
            到点由后台任务发布（调度器每 60 秒扫一次）；重新提交会替换上一次的定时。
          </CardDescription>
        </CardHeader>
        <CardContent className="space-y-3 p-4 pt-0">
          {post.status === "scheduled" ? (
            <div className="flex flex-wrap items-center gap-3 rounded-md bg-muted/40 px-3 py-2 text-sm">
              <span>
                将于{" "}
                {post.publishedAt ? formatScheduleAt(post.publishedAt) : "—"}{" "}
                发布
              </span>
              <Button
                type="button"
                size="sm"
                variant="outline"
                onClick={() => void handleCancelSchedule()}
                disabled={busy}
              >
                {action === "cancel" ? "取消中…" : "取消定时"}
              </Button>
            </div>
          ) : null}

          <div className="flex flex-wrap items-end gap-3">
            <div className="space-y-2">
              <Label htmlFor="post-schedule-at">发布时间</Label>
              <Input
                id="post-schedule-at"
                type="datetime-local"
                className="w-auto"
                value={scheduleAt}
                onChange={(event) => setScheduleAt(event.target.value)}
                disabled={busy}
              />
            </div>
            <Button
              type="button"
              onClick={() => void handleSchedule()}
              disabled={busy}
            >
              {action === "schedule" ? "提交中…" : "定时发布"}
            </Button>
          </div>

          {scheduleError ? (
            <Alert variant="destructive">
              <AlertDescription>{scheduleError}</AlertDescription>
            </Alert>
          ) : null}
        </CardContent>
      </Card>

      <Card>
        <CardHeader className="p-4">
          <CardTitle className="flex items-center gap-2 text-base">
            <History className="h-4 w-4" aria-hidden="true" />
            版本历史
          </CardTitle>
          <CardDescription>
            每次发布与回滚都会存一条快照（最多保留 50 条）；回滚用旧内容覆盖正文，历史不会被改写。
          </CardDescription>
          {post.status === "published" ? (
            <div className="pt-2">
              {/* 线上改完字要点这里：服务端只在内容与最新版本不同时才记账，重复点会 409。 */}
              <Button
                type="button"
                size="sm"
                variant="outline"
                onClick={() => void handleSnapshot()}
                disabled={busy}
              >
                {action === "snapshot" ? "记录中…" : "为当前内容记新版本"}
              </Button>
            </div>
          ) : null}
        </CardHeader>
        <CardContent className="space-y-3 p-4 pt-0">
          {revisionError ? (
            <Alert variant="destructive">
              <AlertDescription>{revisionError}</AlertDescription>
            </Alert>
          ) : null}
          {revisionsLoadError ? (
            <Alert variant="destructive">
              <AlertDescription>{revisionsLoadError.message}</AlertDescription>
            </Alert>
          ) : null}

          {revisionsLoading && !revisions ? (
            <Loading label="正在加载版本…" />
          ) : null}

          {revisions && revisions.list.length === 0 ? (
            <p className="text-sm text-muted-foreground">
              还没有版本快照，发布之后才会生成。
            </p>
          ) : null}

          {revisions && revisions.list.length > 0 ? (
            <ul className="space-y-2">
              {revisions.list.map((revision) => (
                <li
                  key={revision.id}
                  className="flex flex-wrap items-center justify-between gap-3 rounded-md border border-border px-3 py-2"
                >
                  <div className="min-w-0 space-y-0.5">
                    <p className="flex flex-wrap items-center gap-2 text-sm">
                      <span className="font-medium">#{revision.revisionNo}</span>
                      <span className="break-words">
                        {revision.title || "（无标题）"}
                      </span>
                      <time className="text-xs text-muted-foreground">
                        {/* 版本可能跨天，只给时分看不出是哪天，这里带日期。 */}
                        {formatDateTime(revision.createdAt)}
                      </time>
                    </p>
                    {revision.note ? (
                      <p className="break-words text-xs text-muted-foreground">
                        {revision.note}
                      </p>
                    ) : null}
                  </div>
                  <Button
                    type="button"
                    size="sm"
                    variant="outline"
                    onClick={() => void handleRestore(revision)}
                    disabled={busy}
                  >
                    <RotateCcw className="h-4 w-4" aria-hidden="true" />
                    回滚到该版本
                  </Button>
                </li>
              ))}
            </ul>
          ) : null}
        </CardContent>
      </Card>

      <Card>
        <CardHeader className="p-4">
          <CardTitle className="flex items-center gap-2 text-base">
            <Link2 className="h-4 w-4" aria-hidden="true" />
            预览链接
          </CardTitle>
          <CardDescription>
            一次性链接，24 小时过期；首次打开后即失效，可以拿去给外部看排版。
          </CardDescription>
        </CardHeader>
        <CardContent className="space-y-3 p-4 pt-0">
          <div className="flex flex-wrap items-center gap-3">
            <Button
              type="button"
              variant="outline"
              onClick={() => void handleCreateLink()}
              disabled={busy}
            >
              <Link2 className="h-4 w-4" aria-hidden="true" />
              {action === "link"
                ? "生成中…"
                : previewLink
                  ? "重新生成"
                  : "生成预览链接"}
            </Button>
            {previewLink ? (
              <span className="text-xs text-muted-foreground">
                过期时间：{formatDateTime(previewLink.expiresAt)}
              </span>
            ) : null}
          </div>

          {previewLink ? (
            <div className="space-y-2 rounded-md border border-border p-3">
              <p className="break-all font-mono text-xs">{previewUrl}</p>
              <div className="flex flex-wrap gap-2">
                <Button
                  type="button"
                  size="sm"
                  variant="outline"
                  onClick={() => void handleCopyLink()}
                >
                  {copied ? (
                    <Check className="h-4 w-4" aria-hidden="true" />
                  ) : (
                    <Copy className="h-4 w-4" aria-hidden="true" />
                  )}
                  {copied ? "已复制" : "复制"}
                </Button>
                <Button
                  type="button"
                  size="sm"
                  variant="destructive"
                  onClick={() => void handleRevokeLink()}
                  disabled={busy}
                >
                  <Trash2 className="h-4 w-4" aria-hidden="true" />
                  {action === "revoke" ? "作废中…" : "作废"}
                </Button>
              </div>
              <p className="text-xs text-muted-foreground">
                重新生成不会自动作废上一条，旧链接在到期或使用前仍然有效。
              </p>
            </div>
          ) : null}

          {previewError ? (
            <Alert variant="destructive">
              <AlertDescription>{previewError}</AlertDescription>
            </Alert>
          ) : null}
        </CardContent>
      </Card>
    </section>
  );
}
