"use client";

import { zodResolver } from "@hookform/resolvers/zod";
import { usePathname, useRouter, useSearchParams } from "next/navigation";
import { useEffect, useState } from "react";
import { useForm } from "react-hook-form";
import useSWR from "swr";

import { ApiError } from "@/lib/api/fetcher";
import {
  adminAttachmentApi,
  attachmentGoPath,
  type AdminAttachment,
  type AdminAttachmentListResult,
  type AttachmentCreateInput,
} from "@/lib/api/ops";
import { swrKeys } from "@/lib/swr/keys";
import { formatBytes, formatDateTime } from "@/lib/utils";
import {
  attachmentFormSchema,
  type AttachmentFormValues,
} from "@/lib/zod/attachment";
import { Alert, AlertDescription } from "@/ui/shadcn/alert";
import { Button } from "@/ui/shadcn/button";
import { Input } from "@/ui/shadcn/input";
import { Label } from "@/ui/shadcn/label";
import { Loading } from "@/ui/shared/loading";
import { Pagination } from "@/ui/shared/pagination";

/** 每页条数；服务端上限 50（agent.md §5）。 */
const PAGE_SIZE = 20;

/** 空表单；sort 默认 0，因为服务端按 sort、id 升序排，留空会把新附件插到列表中间。 */
const EMPTY_FORM: AttachmentFormValues = {
  postId: "",
  title: "",
  shareUrl: "",
  fileName: "",
  bytes: "",
  sha256: "",
  version: "",
  sort: "0",
};

/** 从查询串读页码；缺省或非法值一律回第 1 页。 */
function readPage(params: { get(name: string): string | null }): number {
  const raw = Number(params.get("page"));
  return Number.isInteger(raw) && raw >= 1 ? raw : 1;
}

/** 字段错误的 DOM id；`aria-describedby` 与错误提示共用，避免两边写歪。 */
function errorId(field: keyof AttachmentFormValues): string {
  return `attachment-${field}-error`;
}

/** 实体 → 表单值；数字字段统一按字符串编辑，提交时再转 number。 */
function toFormValues(item: AdminAttachment | null): AttachmentFormValues {
  if (!item) {
    return EMPTY_FORM;
  }
  return {
    postId: String(item.postId),
    title: item.title,
    shareUrl: item.shareUrl,
    fileName: item.fileName,
    bytes: item.bytes ? String(item.bytes) : "",
    sha256: item.sha256,
    version: item.version,
    sort: String(item.sort),
  };
}

/**
 * 表单值 → 接口入参。
 *
 * 文本字段即使被清空也照发：更新是整体 PUT，清空就是要把值改掉；
 * 数字字段的空串没法表达，只能不传，让服务端保留原值。
 */
function toWriteInput(values: AttachmentFormValues): AttachmentCreateInput {
  const input: AttachmentCreateInput = {
    postId: Number(values.postId),
    title: values.title.trim(),
    shareUrl: values.shareUrl.trim(),
    fileName: values.fileName.trim(),
    sha256: values.sha256.trim(),
    version: values.version.trim(),
  };
  if (values.bytes.trim()) {
    input.bytes = Number(values.bytes);
  }
  if (values.sort.trim()) {
    input.sort = Number(values.sort);
  }
  return input;
}

interface FieldErrorProps {
  id: string;
  message?: string;
}

/** 字段错误提示；没有错误时不渲染，避免表单行高跳动。 */
function FieldError({ id, message }: FieldErrorProps) {
  if (!message) {
    return null;
  }
  return (
    <p id={id} className="text-xs text-destructive">
      {message}
    </p>
  );
}

interface AttachmentFormProps {
  /** 正在编辑的附件；null 表示新建。 */
  editing: AdminAttachment | null;
  busy: boolean;
  onSubmit: (input: AttachmentCreateInput) => void;
  onCancel: () => void;
}

/**
 * 附件表单：新建与编辑共用。
 *
 * 调用方按 `editing?.id` 换 key 重挂载，默认值随编辑对象自然重置，
 * 省掉"实体变了要把值写回表单"的同步逻辑。
 */
function AttachmentForm({
  editing,
  busy,
  onSubmit,
  onCancel,
}: AttachmentFormProps) {
  const { register, handleSubmit, formState } = useForm<AttachmentFormValues>({
    resolver: zodResolver(attachmentFormSchema),
    defaultValues: toFormValues(editing),
  });

  return (
    <form
      className="space-y-4 rounded-lg border border-border p-4"
      noValidate
      onSubmit={handleSubmit((values) => onSubmit(toWriteInput(values)))}
    >
      <div className="flex flex-wrap items-center justify-between gap-2">
        <h2 className="text-sm font-semibold">
          {editing ? `编辑附件 #${editing.id}` : "新建附件"}
        </h2>
        {editing ? (
          <Button
            type="button"
            size="sm"
            variant="ghost"
            onClick={onCancel}
            disabled={busy}
          >
            取消编辑
          </Button>
        ) : null}
      </div>

      <div className="grid gap-4 md:grid-cols-2">
        <div className="space-y-2">
          <Label htmlFor="attachment-postId">文章 ID</Label>
          <Input
            id="attachment-postId"
            inputMode="numeric"
            placeholder="附件挂在哪篇文章下"
            aria-invalid={Boolean(formState.errors.postId)}
            aria-describedby={
              formState.errors.postId ? errorId("postId") : undefined
            }
            {...register("postId")}
          />
          <FieldError
            id={errorId("postId")}
            message={formState.errors.postId?.message}
          />
        </div>

        <div className="space-y-2">
          <Label htmlFor="attachment-title">标题</Label>
          <Input
            id="attachment-title"
            placeholder="下载页上显示的名字"
            aria-invalid={Boolean(formState.errors.title)}
            aria-describedby={
              formState.errors.title ? errorId("title") : undefined
            }
            {...register("title")}
          />
          <FieldError
            id={errorId("title")}
            message={formState.errors.title?.message}
          />
        </div>

        <div className="space-y-2 md:col-span-2">
          <Label htmlFor="attachment-shareUrl">分享链接</Label>
          <Input
            id="attachment-shareUrl"
            placeholder="https://…"
            aria-invalid={Boolean(formState.errors.shareUrl)}
            aria-describedby={
              formState.errors.shareUrl ? errorId("shareUrl") : undefined
            }
            {...register("shareUrl")}
          />
          <FieldError
            id={errorId("shareUrl")}
            message={formState.errors.shareUrl?.message}
          />
        </div>

        <div className="space-y-2">
          <Label htmlFor="attachment-fileName">文件名</Label>
          <Input
            id="attachment-fileName"
            placeholder="src.zip"
            aria-invalid={Boolean(formState.errors.fileName)}
            aria-describedby={
              formState.errors.fileName ? errorId("fileName") : undefined
            }
            {...register("fileName")}
          />
          <FieldError
            id={errorId("fileName")}
            message={formState.errors.fileName?.message}
          />
        </div>

        <div className="space-y-2">
          <Label htmlFor="attachment-bytes">大小（字节）</Label>
          <Input
            id="attachment-bytes"
            inputMode="numeric"
            placeholder="留空则不显示大小"
            aria-invalid={Boolean(formState.errors.bytes)}
            aria-describedby={
              formState.errors.bytes ? errorId("bytes") : undefined
            }
            {...register("bytes")}
          />
          <FieldError
            id={errorId("bytes")}
            message={formState.errors.bytes?.message}
          />
        </div>

        <div className="space-y-2">
          <Label htmlFor="attachment-sha256">sha256</Label>
          <Input
            id="attachment-sha256"
            placeholder="留空表示不校验"
            aria-invalid={Boolean(formState.errors.sha256)}
            aria-describedby={
              formState.errors.sha256 ? errorId("sha256") : undefined
            }
            {...register("sha256")}
          />
          <FieldError
            id={errorId("sha256")}
            message={formState.errors.sha256?.message}
          />
        </div>

        <div className="space-y-2">
          <Label htmlFor="attachment-version">版本</Label>
          <Input
            id="attachment-version"
            placeholder="v1"
            aria-invalid={Boolean(formState.errors.version)}
            aria-describedby={
              formState.errors.version ? errorId("version") : undefined
            }
            {...register("version")}
          />
          <FieldError
            id={errorId("version")}
            message={formState.errors.version?.message}
          />
        </div>

        <div className="space-y-2">
          <Label htmlFor="attachment-sort">排序</Label>
          <Input
            id="attachment-sort"
            inputMode="numeric"
            aria-invalid={Boolean(formState.errors.sort)}
            aria-describedby={
              formState.errors.sort ? errorId("sort") : undefined
            }
            {...register("sort")}
          />
          <FieldError
            id={errorId("sort")}
            message={formState.errors.sort?.message}
          />
        </div>
      </div>

      <Button type="submit" disabled={busy}>
        {busy ? "保存中…" : editing ? "保存修改" : "创建附件"}
      </Button>
    </form>
  );
}

/**
 * 附件管理（D14）：分页表格 + 新建/编辑 + 删除。
 *
 * 附件是挂在文章下的外部文件（网盘分享链接），公开访问一律经
 * `/api/attachments/{id}/go` 302 跳转并计点击，所以列表里给的是跳转地址而不是 shareUrl。
 *
 * 页码放在查询串里，为的是复用 `ui/shared/pagination.tsx` 的链接式分页；
 * 筛选条件留在组件内，查询串变化不会把它们重置。
 */
export function AttachmentManager() {
  const searchParams = useSearchParams();
  const router = useRouter();
  const pathname = usePathname();
  const page = readPage(searchParams);

  const [postIdDraft, setPostIdDraft] = useState("");
  const [postIdFilter, setPostIdFilter] = useState("");
  const [editing, setEditing] = useState<AdminAttachment | null>(null);
  const [formVersion, setFormVersion] = useState(0);
  const [error, setError] = useState<string | null>(null);
  const [notice, setNotice] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [copied, setCopied] = useState<number | null>(null);

  const {
    data,
    error: loadError,
    isLoading,
    mutate,
  } = useSWR<AdminAttachmentListResult, ApiError>(
    swrKeys.adminAttachments(postIdFilter, page, PAGE_SIZE),
    () =>
      adminAttachmentApi.list({
        postId: postIdFilter ? Number(postIdFilter) : undefined,
        page,
        pageSize: PAGE_SIZE,
      }),
  );

  const total = data?.total ?? 0;
  const totalPages = Math.max(1, Math.ceil(total / PAGE_SIZE));

  // 删除或换筛选后当前页可能越界，退到最后一页；页码在查询串里，只能 replace。
  useEffect(() => {
    if (data && page > totalPages) {
      router.replace(`${pathname}?page=${totalPages}`, { scroll: false });
    }
  }, [data, page, totalPages, pathname, router]);

  /** 页码在查询串里，换筛选条件时要清掉 page 才回得到第 1 页。 */
  function resetPage() {
    if (searchParams.has("page")) {
      router.replace(pathname, { scroll: false });
    }
  }

  function changeFilter(next: string) {
    setPostIdFilter(next);
    resetPage();
  }

  async function save(input: AttachmentCreateInput) {
    setBusy(true);
    setError(null);
    setNotice(null);
    try {
      if (editing) {
        await adminAttachmentApi.update(editing.id, input);
        setNotice(`附件 #${editing.id} 已更新。`);
      } else {
        await adminAttachmentApi.create(input);
        setNotice("附件已创建。");
      }
      setEditing(null);
      // 换 key 重挂载表单，否则新建成功后表单里还留着上一次的值。
      setFormVersion((current) => current + 1);
      await mutate();
    } catch (caught) {
      setError(caught instanceof ApiError ? caught.message : "保存失败，请稍后重试");
    } finally {
      setBusy(false);
    }
  }

  async function remove(item: AdminAttachment) {
    if (!window.confirm(`确定删除附件「${item.title}」？公开跳转地址会立即失效。`)) {
      return;
    }
    setBusy(true);
    setError(null);
    setNotice(null);
    try {
      await adminAttachmentApi.remove(item.id);
      if (editing?.id === item.id) {
        setEditing(null);
      }
      setNotice(`附件 #${item.id} 已删除。`);
      await mutate();
    } catch (caught) {
      setError(caught instanceof ApiError ? caught.message : "删除失败，请稍后重试");
    } finally {
      setBusy(false);
    }
  }

  async function copyPath(item: AdminAttachment) {
    try {
      await navigator.clipboard.writeText(attachmentGoPath(item.id));
      setCopied(item.id);
      window.setTimeout(() => setCopied(null), 1_500);
    } catch {
      setError("复制失败，请手动选中地址复制");
    }
  }

  return (
    <section className="space-y-6">
      <header className="space-y-1">
        <h1 className="text-2xl font-semibold tracking-tight">附件</h1>
        <p className="text-sm text-muted-foreground">
          {`共 ${total} 个；公开地址一律经 /api/attachments/{id}/go 302 跳转并计一次点击。`}
        </p>
      </header>

      <form
        className="flex flex-wrap items-end gap-2"
        onSubmit={(event) => {
          event.preventDefault();
          changeFilter(postIdDraft);
        }}
      >
        <div className="space-y-2">
          <Label htmlFor="attachment-post-filter">按文章筛选</Label>
          <Input
            id="attachment-post-filter"
            inputMode="numeric"
            className="w-48"
            placeholder="文章 ID，留空看全部"
            value={postIdDraft}
            // 只收数字，省掉"填了非数字要报错"的分支。
            onChange={(event) =>
              setPostIdDraft(event.target.value.replace(/\D/g, ""))
            }
          />
        </div>
        <Button type="submit" size="sm" variant="outline">
          筛选
        </Button>
        {postIdFilter ? (
          <Button
            type="button"
            size="sm"
            variant="ghost"
            onClick={() => {
              setPostIdDraft("");
              changeFilter("");
            }}
          >
            看全部
          </Button>
        ) : null}
      </form>

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

      {notice ? (
        <p className="text-sm text-muted-foreground" role="status">
          {notice}
        </p>
      ) : null}

      <AttachmentForm
        key={`${editing?.id ?? "new"}-${formVersion}`}
        editing={editing}
        busy={busy}
        onSubmit={(input) => void save(input)}
        onCancel={() => setEditing(null)}
      />

      {isLoading && !data ? <Loading label="正在加载附件…" /> : null}

      {data ? (
        data.list.length === 0 ? (
          <p className="rounded-lg border border-dashed border-border px-4 py-12 text-center text-sm text-muted-foreground">
            {postIdFilter
              ? `文章 #${postIdFilter} 还没有附件。`
              : "还没有附件，用上面的表单新建一个。"}
          </p>
        ) : (
          <div className="overflow-x-auto rounded-lg border border-border">
            <table className="w-full text-sm">
              <caption className="sr-only">附件列表</caption>
              <thead className="bg-muted/60 text-left text-xs text-muted-foreground">
                <tr>
                  <th scope="col" className="px-4 py-3 font-medium">
                    标题
                  </th>
                  <th scope="col" className="px-4 py-3 font-medium">
                    文件
                  </th>
                  <th scope="col" className="px-4 py-3 font-medium">
                    大小
                  </th>
                  <th scope="col" className="px-4 py-3 font-medium">
                    版本
                  </th>
                  <th scope="col" className="px-4 py-3 font-medium">
                    点击
                  </th>
                  <th scope="col" className="px-4 py-3 font-medium">
                    创建时间
                  </th>
                  <th scope="col" className="px-4 py-3 text-right font-medium">
                    操作
                  </th>
                </tr>
              </thead>
              <tbody>
                {data.list.map((item) => (
                  <tr key={item.id} className="border-t border-border">
                    <td className="px-4 py-3">
                      <p className="font-medium">{item.title}</p>
                      <p className="text-xs text-muted-foreground">
                        文章 #{item.postId} · sort {item.sort}
                      </p>
                    </td>
                    <td className="px-4 py-3">
                      <p
                        className="max-w-[16rem] truncate"
                        title={item.fileName || undefined}
                      >
                        {item.fileName || "—"}
                      </p>
                      <code className="text-xs text-muted-foreground">
                        {attachmentGoPath(item.id)}
                      </code>
                    </td>
                    <td className="px-4 py-3 text-muted-foreground">
                      {item.bytes ? formatBytes(item.bytes) : "—"}
                    </td>
                    <td className="px-4 py-3 text-muted-foreground">
                      {item.version || "—"}
                    </td>
                    <td className="px-4 py-3 text-muted-foreground">
                      {item.clicks}
                    </td>
                    <td className="px-4 py-3 text-muted-foreground">
                      {formatDateTime(item.createdAt)}
                    </td>
                    <td className="px-4 py-3">
                      <div className="flex flex-wrap justify-end gap-2">
                        <Button
                          size="sm"
                          variant="outline"
                          onClick={() => void copyPath(item)}
                        >
                          {copied === item.id ? "已复制" : "复制跳转地址"}
                        </Button>
                        <Button
                          size="sm"
                          variant="outline"
                          onClick={() => setEditing(item)}
                          disabled={busy}
                        >
                          编辑
                        </Button>
                        <Button
                          size="sm"
                          variant="destructive"
                          onClick={() => void remove(item)}
                          disabled={busy}
                        >
                          删除
                        </Button>
                      </div>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )
      ) : null}

      {data ? (
        <Pagination
          page={page}
          pageSize={PAGE_SIZE}
          total={total}
          unit="条"
          hrefFor={(next) => `${pathname}?page=${next}`}
        />
      ) : null}
    </section>
  );
}
