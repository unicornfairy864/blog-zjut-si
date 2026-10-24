import { Divider } from "@/ui/layout/divider";
import { Skeleton } from "@/ui/shared/skeleton";

/**
 * 公开页的等待骨架（列表 / 归档 / 标签 / 系列 / 友链 / 搜索等共用）。
 *
 * 公开页是 `force-dynamic`，点击链接后要等服务端返回 HTML——没有这个文件时
 * 界面上**什么都不变**，用户会以为没点中。骨架块按页面节奏（标题 → 分割线 → 卡片）
 * 摆位，跳转时视觉上不跳。
 *
 * 不写 `h1`：骨架不是内容，占位标题用块元素即可，免得读屏把占位当成真标题。
 */
export default function PublicLoading() {
  return (
    <div
      role="status"
      aria-live="polite"
      className="mx-auto w-full max-w-3xl space-y-10"
    >
      <span className="sr-only">正在加载…</span>

      <div className="space-y-3">
        <Skeleton className="h-8 w-40" />
        <Skeleton className="h-4 w-72 max-w-full" />
        <Skeleton className="h-3 w-56 max-w-full" />
      </div>

      {/* 与真实页面同一条分割线，跳转时位置不跳。 */}
      <Divider />

      <div className="space-y-8">
        {[0, 1, 2].map((index) => (
          <div key={index} className="space-y-3 border-b border-border pb-8 last:border-b-0">
            <Skeleton className="h-6 w-2/3" />
            <Skeleton className="h-3 w-44" />
            <Skeleton className="h-4 w-full" />
            <Skeleton className="h-4 w-11/12" />
            <Skeleton className="h-5 w-20 rounded-full" />
          </div>
        ))}
      </div>
    </div>
  );
}
