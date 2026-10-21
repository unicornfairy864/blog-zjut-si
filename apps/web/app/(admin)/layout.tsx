import type { Metadata } from "next";
import type { ReactNode } from "react";

/**
 * 管理端整树不进索引（agent.md §11「SEO」）。
 *
 * `admin/layout.tsx` 是 `"use client"`，客户端组件不能导出 `metadata`，
 * 所以 robots 声明放在这层服务端布局里；它只透传 children，不改变渲染结构。
 */
export const metadata: Metadata = {
  robots: { index: false, follow: false },
};

export default function AdminGroupLayout({
  children,
}: {
  children: ReactNode;
}) {
  return children;
}
