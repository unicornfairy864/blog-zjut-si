import { Divider } from "@/ui/layout/divider";
import { Skeleton } from "@/ui/shared/skeleton";

/**
 * 文章详情页的等待骨架。
 *
 * 与列表页分开写是因为版面不同：文章页是 `max-w-5xl` + 右侧目录栏，
 * 用列表页那套骨架跳转时会明显错位。
 */
export default function PostDetailLoading() {
  return (
    <div role="status" aria-live="polite" className="mx-auto w-full max-w-5xl">
      <span className="sr-only">正在加载文章…</span>

      <div className="lg:grid lg:grid-cols-[minmax(0,1fr)_14rem] lg:gap-10">
        <div className="min-w-0 space-y-6">
          <div className="space-y-3">
            <Skeleton className="h-9 w-4/5" />
            <Skeleton className="h-3 w-52 max-w-full" />
            <Skeleton className="h-8 w-28" />
            <Skeleton className="h-5 w-24 rounded-full" />
          </div>

          <Divider />

          <div className="space-y-4">
            <Skeleton className="h-7 w-1/3" />
            <Skeleton className="h-4 w-full" />
            <Skeleton className="h-4 w-11/12" />
            <Skeleton className="h-4 w-full" />
            <Skeleton className="h-7 w-1/4" />
            <Skeleton className="h-4 w-10/12" />
            <Skeleton className="h-32 w-full" />
          </div>
        </div>

        <aside className="hidden lg:block">
          <div className="space-y-2">
            <Skeleton className="h-3 w-16" />
            <Skeleton className="h-3 w-32" />
            <Skeleton className="h-3 w-28" />
            <Skeleton className="h-3 w-36" />
          </div>
        </aside>
      </div>
    </div>
  );
}
