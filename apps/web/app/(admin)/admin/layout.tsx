"use client";

import type { ReactNode } from "react";

import { SWRProvider } from "@/lib/swr/provider";

/**
 * 管理端根布局：整树客户端（agent.md §8），只装 SWR Provider。
 *
 * 登录页在 /admin/login，不经过会话守卫；守卫在 (protected)/layout.tsx。
 * 这也是 §2 路由表放行 /admin* 的由来。
 */
export default function AdminLayout({ children }: { children: ReactNode }) {
  return <SWRProvider>{children}</SWRProvider>;
}
