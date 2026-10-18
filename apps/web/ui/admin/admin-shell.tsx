"use client";

import { LogOut } from "lucide-react";
import Link from "next/link";
import { usePathname, useRouter } from "next/navigation";
import type { ReactNode } from "react";
import { useState } from "react";
import { useSWRConfig } from "swr";

import type { AdminProfile } from "@/lib/api/admin";
import { adminApi } from "@/lib/api/admin";
import { swrKeys } from "@/lib/swr/keys";
import { cn } from "@/lib/utils";
import { Button } from "@/ui/shadcn/button";

const NAV_ITEMS = [
  { href: "/admin", label: "仪表盘" },
  { href: "/admin/posts", label: "文章" },
] as const;

/** 管理端外壳：顶栏 + 导航 + 内容区（仅桌面宽度，agent.md §20）。 */
export function AdminShell({
  admin,
  children,
}: {
  admin: AdminProfile;
  children: ReactNode;
}) {
  const router = useRouter();
  const pathname = usePathname();
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
        <div className="container flex h-14 items-center justify-between gap-6">
          <div className="flex items-center gap-6">
            <span className="text-sm font-semibold">zjut.si 管理台</span>
            <nav aria-label="管理导航" className="flex items-center gap-1">
              {NAV_ITEMS.map((item) => {
                const active =
                  item.href === "/admin"
                    ? pathname === "/admin"
                    : pathname.startsWith(item.href);
                return (
                  <Link
                    key={item.href}
                    href={item.href}
                    aria-current={active ? "page" : undefined}
                    className={cn(
                      "rounded-md px-3 py-1.5 text-sm transition-colors",
                      active
                        ? "bg-muted font-medium text-foreground"
                        : "text-muted-foreground hover:text-foreground",
                    )}
                  >
                    {item.label}
                  </Link>
                );
              })}
            </nav>
          </div>
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
