import type { Metadata, Viewport } from "next";
import type { ReactNode } from "react";

import { SITE } from "@/lib/constants/site";
import "@/styles/globals.css";

/**
 * 全部路由按请求渲染：CSP 的 nonce 每请求都不同，静态 HTML 会和策略对不上
 * （agent.md §8 要求公开页 force-dynamic，§9 要求 nonce）。
 */
export const dynamic = "force-dynamic";

export const metadata: Metadata = {
  metadataBase: new URL(SITE.url),
  title: {
    default: SITE.title,
    template: `%s · ${SITE.name}`,
  },
  description: SITE.description,
  authors: [{ name: SITE.author }],
  robots: { index: true, follow: true },
  // 不使用 next/font/google：字体自托管，构建期不依赖外网（agent.md §12）。
  icons: { icon: "/favicon.ico" },
};

export const viewport: Viewport = {
  width: "device-width",
  initialScale: 1,
  themeColor: "#ffffff",
};

export default function RootLayout({ children }: { children: ReactNode }) {
  return (
    <html lang={SITE.locale}>
      <body className="min-h-screen">{children}</body>
    </html>
  );
}
