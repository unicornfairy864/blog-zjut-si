"use client";

import type { ComponentProps } from "react";

import { cn } from "@/lib/utils";

/**
 * 原生 `<select>` 的 shadcn 风格包装。
 *
 * `ui/shadcn` 里没有 select：引入 radix select 会多一层交互与体积，而管理表单只需要
 * 「几个固定选项」。原生控件保留键盘与读屏语义，也不用新增依赖（agent.md §17.2）。
 *
 * React 19 允许函数组件直接接收 `ref`，所以这里可以配合 `react-hook-form` 的
 * `register` 一起用（`register` 会传 `name/onChange/onBlur/ref`）。
 */
export function FieldSelect({ className, ...props }: ComponentProps<"select">) {
  return (
    <select
      data-slot="field-select"
      className={cn(
        "flex h-10 w-full rounded-md border border-input bg-background px-3 py-2 text-sm shadow-sm transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2 disabled:cursor-not-allowed disabled:opacity-50",
        className,
      )}
      {...props}
    />
  );
}
