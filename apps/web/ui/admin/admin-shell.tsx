"use client";

import { LogOut } from "lucide-react";
import { useRouter } from "next/navigation";
import type { ReactNode } from "react";
import { useState } from "react";
import { useSWRConfig } from "swr";

import type { AdminProfile } from "@/lib/api/admin";
import { adminApi } from "@/lib/api/admin";
import { swrKeys } from "@/lib/swr/keys";
import { Button } from "@/ui/shadcn/button";

/** 管理端外壳：侧栏 + 顶栏 + 内容区（仅桌面宽度，agent.md §20）。 */
export function AdminShell({
  admin,
  children,
}: {
  admin: AdminProfile;
  children: ReactNode;
}) {
  const router = useRouter();
  const { mutate } = useSWRConfig();
  const [loggingOut, setLoggingOut] = useState(false);

  async function handleLogout() {
    setLoggingOut(true);
    try {
      await adminApi.logout();
    } finally {
      // 清掉会话缓存，避免下一次进入 /admin 时先用旧数据渲染外壳。
      await mutate(swrKeys.adminMe, undefined, { revalidate: false });
      router.replace("/admin/login");
      router.refresh();
    }
  }

  return (
    <div className="flex min-h-screen flex-col">
      <header className="border-b border-border">
        <div className="container flex h-14 items-center justify-between">
          <span className="text-sm font-semibold">zjut.si 管理台</span>
          <div className="flex items-center gap-4">
            <span className="text-sm text-muted-foreground">{admin.username}</span>
            <Button
              variant="outline"
              size="sm"
              onClick={handleLogout}
              disabled={loggingOut}
            >
              <LogOut className="h-4 w-4" aria-hidden="true" />
              登出
            </Button>
          </div>
        </div>
      </header>
      <main className="container flex-1 py-8">{children}</main>
    </div>
  );
}
