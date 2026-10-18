"use client";

import { useRouter } from "next/navigation";
import { useEffect, type ReactNode } from "react";

import { useAuth } from "@/lib/hooks/use-auth";
import { AdminShell } from "@/ui/admin/admin-shell";
import { Alert, AlertDescription } from "@/ui/shadcn/alert";
import { Button } from "@/ui/shadcn/button";
import { Loading } from "@/ui/shared/loading";

/**
 * 受保护的管理页外壳：先 GET /auth/me，401 直接跳 /admin/login。
 *
 * 在会话确认前只渲染加载态，避免先闪一下内容再跳走（agent.md §8）。
 */
export default function ProtectedAdminLayout({
  children,
}: {
  children: ReactNode;
}) {
  const router = useRouter();
  const { admin, isLoading, isUnauthorized, error, mutate } = useAuth();

  useEffect(() => {
    if (isUnauthorized) {
      router.replace("/admin/login");
    }
  }, [isUnauthorized, router]);

  if (isUnauthorized) {
    return (
      <main className="container py-16">
        <Loading label="正在跳转到登录页…" />
      </main>
    );
  }

  // 网络/5xx 等非 401 失败：给一次重试，不要无限转圈。
  if (!isLoading && !admin && error) {
    return (
      <main className="container max-w-md space-y-4 py-16">
        <Alert variant="destructive">
          <AlertDescription>无法确认会话状态，请检查服务是否在线。</AlertDescription>
        </Alert>
        <Button onClick={() => void mutate()}>重试</Button>
      </main>
    );
  }

  if (isLoading || !admin) {
    return (
      <main className="container py-16">
        <Loading label="正在校验会话…" />
      </main>
    );
  }

  return <AdminShell admin={admin}>{children}</AdminShell>;
}
