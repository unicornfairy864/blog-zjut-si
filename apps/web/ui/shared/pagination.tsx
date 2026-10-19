import Link from "next/link";

export interface PaginationProps {
  page: number;
  pageSize: number;
  total: number;
  /** 计数单位；公开文章列表用「篇」，管理端列表用「条」。 */
  unit?: string;
  /** 生成某一页的链接；由调用方决定要不要带 tag / q 之类的查询参数。 */
  hrefFor: (page: number) => string;
}

/** 分页导航（agent.md §5：page ≥1、pageSize 1–50）。 */
export function Pagination({
  page,
  pageSize,
  total,
  unit = "篇",
  hrefFor,
}: PaginationProps) {
  const totalPages = Math.max(1, Math.ceil(total / Math.max(1, pageSize)));
  if (totalPages <= 1) {
    return null;
  }

  const previous = page > 1 ? hrefFor(page - 1) : null;
  const next = page < totalPages ? hrefFor(page + 1) : null;

  return (
    <nav
      aria-label="分页"
      className="flex items-center justify-between gap-4 pt-2 text-sm"
    >
      {previous ? (
        <Link
          href={previous}
          rel="prev"
          className="underline-offset-4 hover:underline"
        >
          ← 上一页
        </Link>
      ) : (
        <span className="text-muted-foreground">← 上一页</span>
      )}

      <span className="text-xs text-muted-foreground">
        第 {page} / {totalPages} 页 · 共 {total} {unit}
      </span>

      {next ? (
        <Link
          href={next}
          rel="next"
          className="underline-offset-4 hover:underline"
        >
          下一页 →
        </Link>
      ) : (
        <span className="text-muted-foreground">下一页 →</span>
      )}
    </nav>
  );
}
