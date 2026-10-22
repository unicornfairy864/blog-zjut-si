"use client";

import { zodResolver } from "@hookform/resolvers/zod";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useEffect, useRef, useState } from "react";
import { useForm } from "react-hook-form";
import useSWR from "swr";

import { ApiError, describeApiError } from "@/lib/api/fetcher";
import { adminMediaApi, mediaMarkdown, waitForMediaReady, type MediaItem } from "@/lib/api/media";
import {
  adminPostApi,
  type PostDetail,
  type PostPreview,
  type PostWriteInput,
} from "@/lib/api/posts";
import { adminTaxonomyApi, type AdminSeries, type AdminTag } from "@/lib/api/taxonomy";
import { swrKeys } from "@/lib/swr/keys";
import { cn, formatTime } from "@/lib/utils";
import { postEditorSchema, type PostEditorValues } from "@/lib/zod/post";
import { PostStatusBadge } from "@/ui/admin/post-status-badge";
import {
  EditorToolbar,
  withImageWidth,
  type MarkdownEdit,
} from "@/ui/admin/editor-toolbar";
import { FieldSelect } from "@/ui/admin/field-select";
import { MediaPicker } from "@/ui/admin/media-picker";
import { PostPreviewPane } from "@/ui/admin/post-preview";
import { PostPublishing } from "@/ui/admin/post-publishing";
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
  tagIds: [],
  seriesId: null,
  seriesOrder: 0,
  coverMediaId: null,
  ogMediaId: null,
  isPinned: false,
  allowComments: true,
  lang: "",
};

/** 自动保存的防抖间隔（agent.md §8：30s 或失焦）。 */
const AUTOSAVE_DELAY_MS = 30_000;
/** 预览请求的防抖间隔，避免每个按键都打一次服务端。 */
const PREVIEW_DEBOUNCE_MS = 600;
/** 正文 textarea 的 id；工具栏与上传插入都要按 id 找到它读光标。 */
const CONTENT_TEXTAREA_ID = "post-content";

/** 正文 textarea 的 DOM 引用；插入语法与拖拽上传都依赖它的光标位置。 */
function contentTextarea(): HTMLTextAreaElement | null {
  return document.getElementById(CONTENT_TEXTAREA_ID) as HTMLTextAreaElement | null;
}

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
    tagIds: post.tagIds ?? [],
    seriesId: post.seriesId,
    seriesOrder: post.seriesOrder,
    coverMediaId: post.coverMediaId,
    ogMediaId: post.ogMediaId,
    isPinned: post.isPinned,
    allowComments: post.allowComments,
    lang: post.lang,
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
    // 分类与展示字段：这些此前只有后端支持，编辑器没有入口（D20 补齐）。
    tagIds: values.tagIds,
    seriesId: values.seriesId ?? 0,
    seriesOrder: values.seriesOrder,
    coverMediaId: values.coverMediaId ?? 0,
    ogMediaId: values.ogMediaId ?? 0,
    isPinned: values.isPinned,
    allowComments: values.allowComments,
    lang: values.lang.trim(),
  };
}

function errorMessage(error: unknown, fallback: string): string {
  if (error instanceof ApiError) {
    return error.message;
  }
  // 上传后的轮询超时/转码失败是本地抛的 Error，消息本身就是给用户看的。
  if (error instanceof Error && error.message) {
    return error.message;
  }
  return fallback;
}

/** 封面 / OG 图这类"单张图"槽位：显示当前图 + 从媒体库选一张 + 清除。 */
interface CoverSlotProps {
  label: string;
  hint: string;
  thumb: { id: number; url: string; alt?: string } | null;
  onSelect: (item: MediaItem) => void;
  onClear: () => void;
}

function CoverSlot({ label, hint, thumb, onSelect, onClear }: CoverSlotProps) {
  const [open, setOpen] = useState(false);

  return (
    <div className="space-y-2">
      <div className="flex items-center justify-between gap-2">
        <span className="text-sm font-medium">{label}</span>
        {thumb ? (
          <Button type="button" size="sm" variant="ghost" onClick={onClear}>
            清除
          </Button>
        ) : null}
      </div>

      {thumb ? (
        <div className="flex items-center gap-3 rounded-md border border-border p-2">
          <img
            src={thumb.url}
            alt={thumb.alt || label}
            className="h-16 w-24 rounded object-cover"
          />
          <span className="text-xs text-muted-foreground">媒体 #{thumb.id}</span>
        </div>
      ) : (
        <p className="rounded-md border border-dashed border-border px-3 py-4 text-xs text-muted-foreground">
          未设置
        </p>
      )}

      <Button
        type="button"
        size="sm"
        variant="outline"
        onClick={() => setOpen((previous) => !previous)}
      >
        {open ? "收起媒体库" : "从媒体库选择"}
      </Button>

      {open ? (
        <MediaPicker
          selectedId={thumb?.id ?? null}
          onPick={(item) => {
            onSelect(item);
            setOpen(false);
          }}
        />
      ) : null}

      <p className="text-xs text-muted-foreground">{hint}</p>
    </div>
  );
}

/**
 * 文章编辑器（D5）：Markdown 长文、工具栏插入语法、30 秒防抖自动保存、服务端预览。
 *
 * 预览走 `PostPreviewPane`（内部复用 `PostContent`，与服务端发布同一条渲染管线）；
 * 正文支持拖拽/粘贴图片上传，预览里的图片可以点选并拖拽改宽度（写回 `{width=N}`）。
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
  const [uploadingImages, setUploadingImages] = useState(false);
  const [uploadError, setUploadError] = useState<string | null>(null);
  const [dragActive, setDragActive] = useState(false);

  const { register, reset, setValue, trigger, getValues, watch, formState } =
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
  // 工具栏的图片按钮要展开下方这块 <details>，用 ref 比受控 state 更少牵扯渲染。
  const mediaDetailsRef = useRef<HTMLDetailsElement | null>(null);

  const { data, error, isLoading } = useSWR<PostDetail, ApiError>(
    currentId ? swrKeys.adminPost(currentId) : null,
    () => adminPostApi.get(currentId as number),
  );

  // 分类选项：标签与系列都是全量列表（管理端不分页），挂在编辑器里直接勾/选。
  const { data: tagData } = useSWR<{ list: AdminTag[] }, ApiError>(
    swrKeys.adminTags,
    () => adminTaxonomyApi.tags(),
  );
  const { data: seriesData } = useSWR<{ list: AdminSeries[] }, ApiError>(
    swrKeys.adminSeries,
    () => adminTaxonomyApi.series(),
  );

  // 刚选中的封面 / OG 图：拿到 id 之后要立刻能看见图，不能等下一次保存回填。
  const [coverPreview, setCoverPreview] = useState<MediaItem | null>(null);
  const [ogPreview, setOgPreview] = useState<MediaItem | null>(null);

  // 只在首次拿到该文章时填充表单；保存后不要被 SWR 的旧缓存覆盖。
  useEffect(() => {
    if (!data || loadedRef.current === data.id) {
      return;
    }
    loadedRef.current = data.id;
    setPost(data);
    setLastSavedAt(data.updatedAt);
    setSavedSlug(data.slug);
    // 服务端回显的封面 / OG 图优先于本地预览（保存后以库里的为准）。
    setCoverPreview(null);
    setOgPreview(null);
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
      // 校验失败时服务端在 data 里带字段错误，只显示"参数校验失败"等于没说。
      setSaveError(describeApiError(caught, "保存失败，请稍后重试"));
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

  /** 勾/取消一个标签；上限与服务端一致（20 个），超了直接不勾。 */
  const toggleTag = (tagId: number) => {
    const current = getValues("tagIds");
    if (current.includes(tagId)) {
      setValue(
        "tagIds",
        current.filter((id) => id !== tagId),
        { shouldDirty: true },
      );
      return;
    }
    if (current.length >= 20) {
      setSaveError("一篇文章最多 20 个标签。");
      return;
    }
    setValue("tagIds", [...current, tagId], { shouldDirty: true });
  };

  // 把 Markdown 编辑写进表单，并把光标/选区恢复到插入内容上（工具栏、上传、媒体库共用）。
  const applyEdit = (edit: MarkdownEdit) => {
    setValue("contentMd", edit.value, { shouldDirty: true });
    const textarea = contentTextarea();
    // 等 react-hook-form 把新值写进 textarea 之后再设选区，否则会被覆盖。
    requestAnimationFrame(() => {
      if (!textarea) {
        return;
      }
      textarea.focus();
      textarea.setSelectionRange(edit.selectionStart, edit.selectionEnd);
    });
  };

  // 在光标处插入一段 Markdown（替换选区），用于媒体库选图与拖拽/粘贴上传。
  const insertMarkdown = (snippet: string) => {
    const textarea = contentTextarea();
    const current = getValues("contentMd");
    const start = textarea?.selectionStart ?? current.length;
    const end = textarea?.selectionEnd ?? current.length;
    const position = start + snippet.length;
    applyEdit({
      value: `${current.slice(0, start)}${snippet}${current.slice(end)}`,
      selectionStart: position,
      selectionEnd: position,
    });
  };

  // 工具栏的图片按钮：展开下方的媒体库选择器并滚到可见处。
  const openMediaPicker = () => {
    const details = mediaDetailsRef.current;
    if (!details) {
      return;
    }
    details.open = true;
    details.scrollIntoView({ block: "nearest" });
  };

  /**
   * 拖入或粘贴的图片：逐个上传，成功后按顺序把 `![alt](url)` 插到光标处。
   *
   * 插入点在上传开始时就固定：上传是异步的，等回来再读光标会被用户随后的点击带跑。
   * 正文没被聚焦过时（例如直接往框里拖文件）追加到末尾，避免插到第 0 个字符。
   *
   * 上传接口返回时图片可能还在转码（`status=processing`），那时的 `url` 只是预测
   * 地址：转码失败就是死链。所以先 `waitForMediaReady` 等到 `ready` 再落进正文。
   */
  async function uploadImages(files: File[]) {
    const images = files.filter((file) => file.type.startsWith("image/"));
    if (images.length === 0) {
      return;
    }

    const textarea = contentTextarea();
    const value = getValues("contentMd");
    const focused = textarea !== null && document.activeElement === textarea;
    let cursor = focused ? textarea.selectionStart : value.length;
    const selectionEnd = focused ? textarea.selectionEnd : value.length;

    setUploadingImages(true);
    setUploadError(null);
    try {
      for (const [index, file] of images.entries()) {
        const uploaded = await adminMediaApi.upload(file);
        const item =
          uploaded.status === "ready"
            ? uploaded
            : await waitForMediaReady(uploaded.id);
        const snippet = mediaMarkdown(item);

        const current = getValues("contentMd");
        const from = Math.min(cursor, current.length);
        // 第一张顺带替换掉原选区，后续几张只插在上一个片段之后，顺序与拖入一致。
        const to =
          index === 0 ? Math.min(Math.max(selectionEnd, from), current.length) : from;
        const next = `${current.slice(0, from)}${snippet}${current.slice(to)}`;
        cursor = from + snippet.length;
        applyEdit({
          value: next,
          selectionStart: cursor,
          selectionEnd: cursor,
        });
      }
    } catch (caught) {
      setUploadError(errorMessage(caught, "图片上传失败，请稍后重试"));
    } finally {
      setUploadingImages(false);
    }
  }

  /**
   * 预览里拖完手柄后把宽度写回正文（agent.md §11「图片尺寸」）。
   *
   * 只在松开手柄时调用：拖动过程只改内联样式，否则会不停触发服务端预览。
   */
  const handleImageWidth = (index: number, width: number) => {
    const current = getValues("contentMd");
    const next = withImageWidth(current, index, width);
    if (next !== current) {
      setValue("contentMd", next, { shouldDirty: true });
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
        {status === "scheduled" ? (
          // 定时中的文章主操作是取消定时（unpublish 会同时撤掉待执行的发布任务），
          // 而不是"发布"：到点由后台任务接管，这里重复发布只会打乱已定的时间。
          <Button
            type="button"
            variant="outline"
            onClick={handleUnpublish}
            disabled={busy}
          >
            {busyAction === "unpublish" ? "取消中…" : "取消定时"}
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
              <Label htmlFor={CONTENT_TEXTAREA_ID}>正文（Markdown）</Label>
              <span className="text-xs text-muted-foreground">
                {values.contentMd.length} 字符
              </span>
            </div>
            <EditorToolbar
              textareaId={CONTENT_TEXTAREA_ID}
              value={values.contentMd}
              onInsert={applyEdit}
              onPickImage={openMediaPicker}
              disabled={uploadingImages}
            />
            {uploadingImages ? (
              <p aria-live="polite" className="text-xs text-muted-foreground">
                图片上传并转码中…（转码完成后才会插入正文）
              </p>
            ) : null}
            {uploadError ? (
              <Alert variant="destructive">
                <AlertDescription>{uploadError}</AlertDescription>
              </Alert>
            ) : null}
            <Textarea
              id={CONTENT_TEXTAREA_ID}
              className={cn(
                "min-h-[60vh] font-mono text-sm leading-6",
                // 拖拽高亮用 ring：只改边框色会让 60vh 的框体在拖动时轻微跳动。
                dragActive && "border-primary ring-2 ring-ring",
              )}
              placeholder="# 标题&#10;&#10;开始写正文，支持 GFM 表格、任务列表与代码块；图片可直接拖进来或粘贴。"
              {...register("contentMd", { onBlur: saveOnBlur })}
              onDragOver={(event) => {
                // 只接管文件拖拽，普通文本拖放交给浏览器默认行为。
                if (!event.dataTransfer.types.includes("Files")) {
                  return;
                }
                event.preventDefault();
                setDragActive(true);
              }}
              onDragLeave={() => setDragActive(false)}
              onDrop={(event) => {
                const files = Array.from(event.dataTransfer.files);
                if (files.length === 0) {
                  return;
                }
                event.preventDefault();
                setDragActive(false);
                void uploadImages(files);
              }}
              onPaste={(event) => {
                const files = Array.from(event.clipboardData.files);
                // 剪贴板里没有图片时完全不拦截，普通文本粘贴照常。
                if (!files.some((file) => file.type.startsWith("image/"))) {
                  return;
                }
                event.preventDefault();
                void uploadImages(files);
              }}
            />
            <details
              ref={mediaDetailsRef}
              className="rounded-lg border border-border p-3"
            >
              <summary className="cursor-pointer text-sm font-medium">插入图片</summary>
              <div className="mt-3">
                <MediaPicker
                  onPick={(item) => {
                    insertMarkdown(`\n\n${mediaMarkdown(item)}\n\n`);
                    if (mediaDetailsRef.current) {
                      mediaDetailsRef.current.open = false;
                    }
                  }}
                />
              </div>
            </details>
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
                <PostPreviewPane
                  html={preview.html}
                  onImageWidth={handleImageWidth}
                />
              ) : null}
              {preview ? (
                <p className="mt-4 text-xs text-muted-foreground">
                  点击图片选中，拖右下角手柄改宽度；宽度按 10px 取整后写回正文的{" "}
                  <code>{"{width=N}"}</code>。
                </p>
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
            分类与展示
          </summary>
          <div className="mt-4 space-y-5">
            <fieldset className="space-y-2">
              <legend className="text-sm font-medium">
                标签（已选 {values.tagIds.length} 个，最多 20 个）
              </legend>
              {tagData && tagData.list.length > 0 ? (
                <ul className="flex max-h-52 flex-wrap gap-x-4 gap-y-2 overflow-y-auto rounded-md border border-border p-3">
                  {tagData.list.map((tag) => {
                    const checked = values.tagIds.includes(tag.id);
                    return (
                      <li key={tag.id}>
                        <label className="flex items-center gap-2 text-sm">
                          <input
                            type="checkbox"
                            className="h-4 w-4"
                            checked={checked}
                            onChange={() => toggleTag(tag.id)}
                          />
                          {tag.name}
                          <span className="text-xs text-muted-foreground">
                            {tag.count}
                          </span>
                        </label>
                      </li>
                    );
                  })}
                </ul>
              ) : (
                <p className="rounded-md border border-dashed border-border px-3 py-4 text-xs text-muted-foreground">
                  还没有标签，先去{" "}
                  <Link href="/admin/tags" className="underline underline-offset-4">
                    标签管理
                  </Link>{" "}
                  建一个。
                </p>
              )}
            </fieldset>

            <div className="grid gap-4 md:grid-cols-[minmax(0,2fr)_minmax(0,1fr)]">
              <div className="space-y-2">
                <Label htmlFor="post-series">系列</Label>
                <FieldSelect
                  id="post-series"
                  value={values.seriesId === null ? "" : String(values.seriesId)}
                  onChange={(event) => {
                    const next = event.target.value;
                    setValue("seriesId", next === "" ? null : Number(next), {
                      shouldDirty: true,
                    });
                  }}
                >
                  <option value="">不归系列</option>
                  {(seriesData?.list ?? []).map((series) => (
                    <option key={series.id} value={series.id}>
                      {series.name}（{series.count}）
                    </option>
                  ))}
                </FieldSelect>
                <p className="text-xs text-muted-foreground">
                  一篇文章只属于一个系列；系列页按下面的序号排序。
                </p>
              </div>

              <div className="space-y-2">
                <Label htmlFor="post-series-order">系列内序号</Label>
                <Input
                  id="post-series-order"
                  type="number"
                  min={0}
                  aria-invalid={formState.errors.seriesOrder ? true : undefined}
                  {...register("seriesOrder", { valueAsNumber: true })}
                />
                {formState.errors.seriesOrder ? (
                  <p className="text-xs text-destructive">
                    {formState.errors.seriesOrder.message}
                  </p>
                ) : null}
              </div>
            </div>

            <div className="grid gap-4 md:grid-cols-2">
              <CoverSlot
                label="封面图"
                hint="用于公开列表的封面与 OG 卡片（取 1600 档变体）。"
                thumb={
                  coverPreview
                    ? { id: coverPreview.id, url: coverPreview.url, alt: coverPreview.alt }
                    : (post?.cover ?? null)
                }
                onSelect={(item) => {
                  setCoverPreview(item);
                  setValue("coverMediaId", item.id, { shouldDirty: true });
                }}
                onClear={() => {
                  setCoverPreview(null);
                  setValue("coverMediaId", null, { shouldDirty: true });
                }}
              />

              <CoverSlot
                label="OG 图"
                hint="社交卡片专用；留空则用封面图。"
                thumb={
                  ogPreview
                    ? { id: ogPreview.id, url: ogPreview.url, alt: ogPreview.alt }
                    : (post?.ogImage ?? null)
                }
                onSelect={(item) => {
                  setOgPreview(item);
                  setValue("ogMediaId", item.id, { shouldDirty: true });
                }}
                onClear={() => {
                  setOgPreview(null);
                  setValue("ogMediaId", null, { shouldDirty: true });
                }}
              />
            </div>

            <div className="grid gap-4 md:grid-cols-3">
              <label className="flex items-center gap-2 text-sm">
                <input
                  type="checkbox"
                  className="h-4 w-4"
                  {...register("isPinned")}
                />
                置顶
              </label>
              <label className="flex items-center gap-2 text-sm">
                <input
                  type="checkbox"
                  className="h-4 w-4"
                  {...register("allowComments")}
                />
                允许评论
              </label>
              <div className="space-y-2">
                <Label htmlFor="post-lang">语言标签（lang）</Label>
                <Input
                  id="post-lang"
                  placeholder="zh-CN"
                  aria-invalid={formState.errors.lang ? true : undefined}
                  {...register("lang")}
                />
                {formState.errors.lang ? (
                  <p className="text-xs text-destructive">
                    {formState.errors.lang.message}
                  </p>
                ) : (
                  <p className="text-xs text-muted-foreground">
                    留空按服务端默认 zh-CN。
                  </p>
                )}
              </div>
            </div>
          </div>
        </details>

        {/*
          发布控制（D13）：定时发布 / 版本回滚 / 预览链接。
          放在「分类与展示」下面：这些是发布前后的收尾动作，跟着文章元信息走。
          post 为 null 时组件自己只显示"先保存草稿"，所以这里不用额外判断。
        */}
        <PostPublishing post={post} onSaved={applySaved} disabled={busy} />

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
