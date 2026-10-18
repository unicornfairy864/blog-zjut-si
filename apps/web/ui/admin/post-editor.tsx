"use client";

import { zodResolver } from "@hookform/resolvers/zod";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useEffect, useRef, useState } from "react";
import { useForm } from "react-hook-form";
import useSWR from "swr";

import { ApiError } from "@/lib/api/fetcher";
import {
  adminPostApi,
  type PostDetail,
  type PostPreview,
  type PostWriteInput,
} from "@/lib/api/posts";
import { swrKeys } from "@/lib/swr/keys";
import { cn, formatTime } from "@/lib/utils";
import { postEditorSchema, type PostEditorValues } from "@/lib/zod/post";
import { PostStatusBadge } from "@/ui/admin/post-status-badge";
import { Alert, AlertDescription } from "@/ui/shadcn/alert";
import { Button } from "@/ui/shadcn/button";
import { Input } from "@/ui/shadcn/input";
import { Label } from "@/ui/shadcn/label";
import { Textarea } from "@/ui/shadcn/textarea";
import { Loading } from "@/ui/shared/loading";

const EMPTY_VALUES: PostEditorValues = {
  title: "",
  slug: "",
  contentMd: "",
  excerpt: "",
  seoTitle: "",
  seoDescription: "",
  canonicalUrl: "",
};

/** 自动保存的防抖间隔（agent.md §8：30s 或失焦）。 */
const AUTOSAVE_DELAY_MS = 30_000;
/** 预览请求的防抖间隔，避免每个按键都打一次服务端。 */
const PREVIEW_DEBOUNCE_MS = 600;

/** 实体 → 表单值。 */
function toFormValues(post: PostDetail): PostEditorValues {
  return {
    title: post.title,
    slug: post.slug,
    contentMd: post.contentMd,
    excerpt: post.excerpt,
    seoTitle: post.seoTitle,
    seoDescription: post.seoDescription,
    canonicalUrl: post.canonicalUrl,
  };
}

/** 表单值 → 接口入参；slug 为空时不传，交给服务端生成。 */
function toWriteInput(
  values: PostEditorValues,
  createRedirect: boolean,
): PostWriteInput {
  const slug = values.slug.trim();
  return {
    title: values.title.trim(),
    contentMd: values.contentMd,
    excerpt: values.excerpt.trim(),
    ...(slug ? { slug } : {}),
    seoTitle: values.seoTitle.trim(),
    seoDescription: values.seoDescription.trim(),
    canonicalUrl: values.canonicalUrl.trim(),
    createRedirect,
  };
}

function errorMessage(error: unknown, fallback: string): string {
  return error instanceof ApiError ? error.message : fallback;
}

/**
 * 文章编辑器（D5）：Markdown 长文、30 秒防抖自动保存、服务端预览。
 *
 * 新建与编辑共用；首次保存会创建草稿，并用 history.replaceState 换地址，
 * 避免 Next 重新挂载组件丢掉未保存内容。
 */
export function PostEditor({ postId }: { postId: number | null }) {
  const router = useRouter();

  const [currentId, setCurrentId] = useState<number | null>(postId);
  const [post, setPost] = useState<PostDetail | null>(null);
  const [saving, setSaving] = useState(false);
  const [saveError, setSaveError] = useState<string | null>(null);
  const [lastSavedAt, setLastSavedAt] = useState<number | null>(null);
  const [busyAction, setBusyAction] = useState<string | null>(null);
  const [previewOpen, setPreviewOpen] = useState(false);
  const [preview, setPreview] = useState<PostPreview | null>(null);
  const [previewLoading, setPreviewLoading] = useState(false);
  const [previewError, setPreviewError] = useState<string | null>(null);
  const [createRedirect, setCreateRedirect] = useState(true);
  const [savedSlug, setSavedSlug] = useState("");

  const { register, reset, trigger, getValues, watch, formState } =
    useForm<PostEditorValues>({
      resolver: zodResolver(postEditorSchema),
      defaultValues: EMPTY_VALUES,
    });

  const values = watch();

  const currentIdRef = useRef<number | null>(currentId);
  currentIdRef.current = currentId;
  const redirectRef = useRef(createRedirect);
  redirectRef.current = createRedirect;
  const savingRef = useRef(false);
  const loadedRef = useRef<number | null>(null);

  const { data, error, isLoading } = useSWR<PostDetail, ApiError>(
    currentId ? swrKeys.adminPost(currentId) : null,
    () => adminPostApi.get(currentId as number),
  );

  // 只在首次拿到该文章时填充表单；保存后不要被 SWR 的旧缓存覆盖。
  useEffect(() => {
    if (!data || loadedRef.current === data.id) {
      return;
    }
    loadedRef.current = data.id;
    setPost(data);
    setLastSavedAt(data.updatedAt);
    setSavedSlug(data.slug);
    reset(toFormValues(data));
  }, [data, reset]);

  /** 保存草稿；返回落库后的文章，失败或校验不通过返回 null。 */
  async function persist(options: { silent?: boolean } = {}) {
    if (savingRef.current) {
      return null;
    }

    // 自动保存不应弹出字段错误：标题还没写就先不动。
    if (options.silent && !getValues().title.trim()) {
      return null;
    }

    const valid = await trigger();
    if (!valid) {
      if (!options.silent) {
        setSaveError("请先补全标题等必填项。");
      }
      return null;
    }

    const snapshot = getValues();
    savingRef.current = true;
    setSaving(true);
    setSaveError(null);
    try {
      const id = currentIdRef.current;
      const input = toWriteInput(snapshot, redirectRef.current);
      let saved: PostDetail;
      if (id) {
        saved = await adminPostApi.update(id, input);
      } else {
        saved = await adminPostApi.create(input);
        currentIdRef.current = saved.id;
        loadedRef.current = saved.id;
        setCurrentId(saved.id);
        // 不触发路由跳转，否则组件会重新挂载、丢掉尚未保存的内容。
        window.history.replaceState(null, "", `/admin/posts/${saved.id}`);
      }

      setPost(saved);
      setLastSavedAt(saved.updatedAt);
      setSavedSlug(saved.slug);

      // 保存期间用户没有继续输入，才用服务端结果回填并标记为"已保存"。
      if (JSON.stringify(getValues()) === JSON.stringify(snapshot)) {
        reset(toFormValues(saved));
      }
      return saved;
    } catch (caught) {
      setSaveError(errorMessage(caught, "保存失败，请稍后重试"));
      return null;
    } finally {
      savingRef.current = false;
      setSaving(false);
    }
  }

  const persistRef = useRef(persist);
  persistRef.current = persist;

  // 30 秒防抖自动保存：每次输入都会重置计时器。
  useEffect(() => {
    if (!formState.isDirty || saving) {
      return;
    }
    const timer = setTimeout(() => {
      void persistRef.current();
    }, AUTOSAVE_DELAY_MS);
    return () => clearTimeout(timer);
  }, [formState.isDirty, saving, values]);

  // 失焦即保存（标题、正文、摘要）。
  const saveOnBlur = () => {
    if (formState.isDirty) {
      void persistRef.current({ silent: true });
    }
  };

  // 预览：打开面板后按 600ms 防抖请求服务端，与发布共用同一渲染管线。
  useEffect(() => {
    if (!previewOpen) {
      return;
    }
    let ignore = false;
    const timer = setTimeout(() => {
      setPreviewLoading(true);
      setPreviewError(null);
      adminPostApi
        .preview({ contentMd: values.contentMd, excerpt: values.excerpt })
        .then((result) => {
          if (!ignore) {
            setPreview(result);
          }
        })
        .catch((caught: unknown) => {
          if (!ignore) {
            setPreviewError(errorMessage(caught, "预览生成失败"));
          }
        })
        .finally(() => {
          if (!ignore) {
            setPreviewLoading(false);
          }
        });
    }, PREVIEW_DEBOUNCE_MS);
    return () => {
      ignore = true;
      clearTimeout(timer);
    };
  }, [previewOpen, values.contentMd, values.excerpt]);

  /** 用服务端返回的文章刷新本地状态。 */
  function applySaved(next: PostDetail) {
    loadedRef.current = next.id;
    currentIdRef.current = next.id;
    setCurrentId(next.id);
    setPost(next);
    setLastSavedAt(next.updatedAt);
    setSavedSlug(next.slug);
    reset(toFormValues(next));
  }

  async function handlePublish() {
    setBusyAction("publish");
    setSaveError(null);
    try {
      const saved = formState.isDirty || !post ? await persist() : post;
      if (!saved) {
        return;
      }
      applySaved(await adminPostApi.publish(saved.id));
    } catch (caught) {
      setSaveError(errorMessage(caught, "发布失败，请稍后重试"));
    } finally {
      setBusyAction(null);
    }
  }

  async function handleUnpublish() {
    if (!post) {
      return;
    }
    setBusyAction("unpublish");
    setSaveError(null);
    try {
      applySaved(await adminPostApi.unpublish(post.id));
    } catch (caught) {
      setSaveError(errorMessage(caught, "操作失败，请稍后重试"));
    } finally {
      setBusyAction(null);
    }
  }

  async function handleArchive() {
    if (!post) {
      return;
    }
    setBusyAction("archive");
    setSaveError(null);
    try {
      applySaved(await adminPostApi.update(post.id, { status: "archived" }));
    } catch (caught) {
      setSaveError(errorMessage(caught, "归档失败，请稍后重试"));
    } finally {
      setBusyAction(null);
    }
  }

  async function handleDelete() {
    if (!post) {
      return;
    }
    if (!window.confirm(`确定把《${post.title}》移入回收站？`)) {
      return;
    }
    setBusyAction("delete");
    setSaveError(null);
    try {
      await adminPostApi.remove(post.id);
      router.replace("/admin/posts");
    } catch (caught) {
      setSaveError(errorMessage(caught, "删除失败，请稍后重试"));
      setBusyAction(null);
    }
  }

  if (postId && isLoading) {
    return <Loading label="正在加载文章…" />;
  }

  if (postId && error) {
    return (
      <div className="space-y-4">
        <Alert variant="destructive">
          <AlertDescription>{error.message}</AlertDescription>
        </Alert>
        <Button asChild variant="outline">
          <Link href="/admin/posts">返回文章列表</Link>
        </Button>
      </div>
    );
  }

  const status = post?.status ?? "draft";
  const busy = saving || Boolean(busyAction);
  const savedHint = saving
    ? "保存中…"
    : formState.isDirty
      ? "有未保存修改"
      : lastSavedAt
        ? `已保存 ${formatTime(lastSavedAt)}`
        : "尚未保存";
  const slugLocked = status === "published" || status === "archived";
  const slugChanged = Boolean(post) && values.slug.trim() !== savedSlug;

  return (
    <section className="space-y-6">
      <header className="flex flex-wrap items-center justify-between gap-4">
        <div className="flex flex-wrap items-center gap-3">
          <Button asChild variant="ghost" size="sm">
            <Link href="/admin/posts">← 文章</Link>
          </Button>
          <h1 className="text-xl font-semibold tracking-tight">
            {post ? "编辑文章" : "新建文章"}
          </h1>
          <PostStatusBadge status={status} />
        </div>
        <div className="flex flex-wrap items-center gap-3">
          <span aria-live="polite" className="text-xs text-muted-foreground">
            {savedHint}
          </span>
          <Button
            type="button"
            variant="outline"
            size="sm"
            aria-pressed={previewOpen}
            onClick={() => setPreviewOpen((open) => !open)}
          >
            {previewOpen ? "关闭预览" : "预览"}
          </Button>
          <Button
            type="button"
            size="sm"
            onClick={() => void persist()}
            disabled={busy}
          >
            {saving ? "保存中…" : "保存草稿"}
          </Button>
        </div>
      </header>

      <div className="flex flex-wrap items-center gap-2">
        {status === "draft" ? (
          <Button type="button" onClick={handlePublish} disabled={busy}>
            {busyAction === "publish" ? "发布中…" : "发布"}
          </Button>
        ) : null}
        {status === "published" ? (
          <>
            <Button
              type="button"
              variant="outline"
              onClick={handleUnpublish}
              disabled={busy}
            >
              取消发布
            </Button>
            <Button
              type="button"
              variant="outline"
              onClick={handleArchive}
              disabled={busy}
            >
              归档
            </Button>
          </>
        ) : null}
        {status === "archived" ? (
          <Button
            type="button"
            variant="outline"
            onClick={handleUnpublish}
            disabled={busy}
          >
            取消归档
          </Button>
        ) : null}
        {post ? (
          <Button
            type="button"
            variant="destructive"
            onClick={handleDelete}
            disabled={busy}
          >
            移入回收站
          </Button>
        ) : null}
        {!post ? (
          <span className="text-xs text-muted-foreground">
            首次保存会自动创建草稿；停止输入 30 秒或离开输入框也会自动保存。
          </span>
        ) : null}
      </div>

      {saveError ? (
        <Alert variant="destructive">
          <AlertDescription>{saveError}</AlertDescription>
        </Alert>
      ) : null}

      <form
        className="space-y-5"
        noValidate
        onSubmit={(event) => {
          event.preventDefault();
          void persist();
        }}
      >
        <div className="space-y-2">
          <Label htmlFor="post-title">标题</Label>
          <Input
            id="post-title"
            placeholder="给文章起个标题"
            aria-invalid={Boolean(formState.errors.title)}
            aria-describedby={formState.errors.title ? "post-title-error" : undefined}
            {...register("title", { onBlur: saveOnBlur })}
          />
          {formState.errors.title ? (
            <p id="post-title-error" className="text-xs text-destructive">
              {formState.errors.title.message}
            </p>
          ) : null}
        </div>

        <div className="grid gap-4 md:grid-cols-[minmax(0,1fr)_minmax(0,2fr)]">
          <div className="space-y-2">
            <Label htmlFor="post-slug">slug</Label>
            <Input
              id="post-slug"
              placeholder="留空由服务端生成"
              aria-invalid={Boolean(formState.errors.slug)}
              aria-describedby={
                formState.errors.slug ? "post-slug-error" : "post-slug-hint"
              }
              {...register("slug", { onBlur: saveOnBlur })}
            />
            {formState.errors.slug ? (
              <p id="post-slug-error" className="text-xs text-destructive">
                {formState.errors.slug.message}
              </p>
            ) : (
              <p id="post-slug-hint" className="text-xs text-muted-foreground">
                /posts/{values.slug.trim() || "自动生成"}
              </p>
            )}
            {slugLocked && slugChanged ? (
              <label className="flex items-start gap-2 text-xs text-muted-foreground">
                <input
                  type="checkbox"
                  className="mt-0.5 h-4 w-4 rounded border-input"
                  checked={createRedirect}
                  onChange={(event) => setCreateRedirect(event.target.checked)}
                />
                同时写入 301 跳转（已发布文章改 slug 必须勾选，否则服务端拒绝）
              </label>
            ) : null}
          </div>

          <div className="space-y-2">
            <Label htmlFor="post-excerpt">摘要</Label>
            <Textarea
              id="post-excerpt"
              className="min-h-[38px]"
              rows={2}
              placeholder="留空自动取正文前 160 字"
              {...register("excerpt", { onBlur: saveOnBlur })}
            />
            {formState.errors.excerpt ? (
              <p className="text-xs text-destructive">
                {formState.errors.excerpt.message}
              </p>
            ) : null}
          </div>
        </div>

        <div className={cn("grid gap-6", previewOpen && "lg:grid-cols-2")}>
          <div className="space-y-2">
            <div className="flex items-center justify-between gap-4">
              <Label htmlFor="post-content">正文（Markdown）</Label>
              <span className="text-xs text-muted-foreground">
                {values.contentMd.length} 字符
              </span>
            </div>
            <Textarea
              id="post-content"
              className="min-h-[60vh] font-mono text-sm leading-6"
              placeholder="# 标题&#10;&#10;开始写正文，支持 GFM 表格、任务列表与代码块。"
              {...register("contentMd", { onBlur: saveOnBlur })}
            />
          </div>

          {previewOpen ? (
            <section
              aria-label="发布预览"
              className="rounded-lg border border-border bg-card p-6"
            >
              <header className="mb-4 flex items-center justify-between border-b border-border pb-3">
                <h2 className="text-sm font-medium">发布预览</h2>
                <span className="text-xs text-muted-foreground">
                  {previewLoading
                    ? "生成中…"
                    : preview
                      ? `${preview.wordCount} 字 · 约 ${preview.readingTime} 分钟`
                      : "输入正文后自动生成"}
                </span>
              </header>
              {previewError ? (
                <Alert variant="destructive">
                  <AlertDescription>{previewError}</AlertDescription>
                </Alert>
              ) : null}
              {preview ? (
                <article
                  className="prose-blog max-w-none"
                  // HTML 已由服务端 goldmark + bluemonday 消毒（agent.md §9）
                  dangerouslySetInnerHTML={{ __html: preview.html }}
                />
              ) : null}
              {preview?.excerpt ? (
                <p className="mt-6 border-t border-border pt-3 text-xs text-muted-foreground">
                  自动摘要：{preview.excerpt}
                </p>
              ) : null}
            </section>
          ) : null}
        </div>

        <details className="rounded-lg border border-border p-4">
          <summary className="cursor-pointer text-sm font-medium">
            SEO 与高级设置
          </summary>
          <div className="mt-4 grid gap-4 md:grid-cols-2">
            <div className="space-y-2">
              <Label htmlFor="post-seo-title">SEO 标题</Label>
              <Input id="post-seo-title" {...register("seoTitle")} />
              {formState.errors.seoTitle ? (
                <p className="text-xs text-destructive">
                  {formState.errors.seoTitle.message}
                </p>
              ) : null}
            </div>
            <div className="space-y-2">
              <Label htmlFor="post-canonical">Canonical URL</Label>
              <Input
                id="post-canonical"
                placeholder="https://blog.zjut.si/posts/…"
                {...register("canonicalUrl")}
              />
              {formState.errors.canonicalUrl ? (
                <p className="text-xs text-destructive">
                  {formState.errors.canonicalUrl.message}
                </p>
              ) : null}
            </div>
            <div className="space-y-2 md:col-span-2">
              <Label htmlFor="post-seo-description">SEO 描述</Label>
              <Textarea
                id="post-seo-description"
                className="min-h-[72px]"
                rows={3}
                {...register("seoDescription")}
              />
              {formState.errors.seoDescription ? (
                <p className="text-xs text-destructive">
                  {formState.errors.seoDescription.message}
                </p>
              ) : null}
            </div>
          </div>
        </details>
      </form>
    </section>
  );
}
