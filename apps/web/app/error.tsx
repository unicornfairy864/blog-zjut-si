"use client";

import { useEffect } from "react";

import { Button } from "@/ui/shadcn/button";

/**
 * 全局错误边界：只显示人话提示，绝不暴露堆栈（agent.md §8）。
 */
export default function GlobalError({
  error,
  reset,
}: {
  error: Error & { digest?: string };
  reset: () => void;
}) {
  useEffect(() => {
    // 客户端只记录摘要，详细堆栈在服务端日志里。
    console.error("页面渲染失败", error.digest ?? error.name);
  }, [error]);

  return (
    <main className="container flex min-h-[60vh] flex-col items-center justify-center gap-4 text-center">
      <h1 className="text-2xl font-semibold">页面出了点问题</h1>
      <p className="text-sm text-muted-foreground">
        请稍后重试；如果持续出现，请查看服务端日志中的请求 id。
      </p>
      <Button onClick={reset}>重试</Button>
    </main>
  );
}
