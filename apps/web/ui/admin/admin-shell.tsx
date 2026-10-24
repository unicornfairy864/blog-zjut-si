"use client";

import {
  BarChart3,
  CornerUpRight,
  FileText,
  HardDrive,
  Images,
  Layers,
  LayoutDashboard,
  Link2,
  ListChecks,
  LogOut,
  MessageSquare,
  Paperclip,
  ScrollText,
  Settings,
  Tags,
} from "lucide-react";
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

/** 导航项；图标与文案一一对应，新增模块时在这里加一行即可。 */
const NAV_ITEMS = [
  { href: "/admin", label: "仪表盘", icon: LayoutDashboard },
  { href: "/admin/posts", label: "文章", icon: FileText },
  { href: "/admin/media", label: "媒体", icon: Images },
  { href: "/admin/comments", label: "评论", icon: MessageSquare },
  { href: "/admin/tags", label: "标签", icon: Tags },
  { href: "/admin/series", label: "系列", icon: Layers },
  { href: "/admin/links", label: "友链", icon: Link2 },
  { href: "/admin/redirects", label: "跳转", icon: CornerUpRight },
  { href: "/admin/attachments", label: "附件", icon: Paperclip },
  { href: "/admin/storage", label: "存储", icon: HardDrive },
  { href: "/admin/jobs", label: "任务", icon: ListChecks },
  { href: "/admin/audit-logs", label: "审计", icon: ScrollText },
  { href: "/admin/stats", label: "统计", icon: BarChart3 },
  { href: "/admin/settings", label: "设置", icon: Settings },
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
        {/* 导航项变多后必须允许换行，否则窄屏会把右侧的登出按钮挤出可视区。 */}
        <div className="container flex min-h-14 flex-wrap items-center justify-between gap-x-6 gap-y-2 py-2">
          <div className="flex flex-wrap items-center gap-x-6 gap-y-2">
            <span className="text-sm font-semibold">zjut.si 管理端</span>
            <nav aria-label="管理导航" className="flex flex-wrap items-center gap-1">
              {NAV_ITEMS.map((item) => {
                const active =
                  item.href === "/admin"
                    ? pathname === "/admin"
                    : pathname.startsWith(item.href);
                const Icon = item.icon;
                return (
                  <Link
                    key={item.href}
                    href={item.href}
                    aria-current={active ? "page" : undefined}
                    className={cn(
                      "flex items-center gap-1.5 rounded-md px-3 py-1.5 text-sm transition-colors",
                      active
                        ? "bg-muted font-medium text-foreground"
                        : "text-muted-foreground hover:text-foreground",
                    )}
                  >
                    <Icon className="h-4 w-4" aria-hidden="true" />
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
