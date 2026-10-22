import type { Metadata, Viewport } from "next";
import type { ReactNode } from "react";

import { SITE } from "@/lib/constants/site";
import { buildOpenGraph } from "@/lib/seo";
import "@/styles/globals.css";
import "@/styles/prose.css";
import "@/styles/chroma.css";

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
  // Next 对 openGraph / twitter 是整体替换而非深合并，所以站点级默认在这里给一份，
  // 自己声明 openGraph 的页面（见 lib/seo.ts 的 buildOpenGraph）必须把 siteName 带上。
  openGraph: buildOpenGraph({
    title: SITE.title,
    description: SITE.description,
    url: SITE.url,
  }),
  twitter: { card: "summary_large_image" },
  // 不使用 next/font/google：字体自托管，构建期不依赖外网（agent.md §12）。
  // 品牌图标与个人主页（www.zjut.si）同源，素材放在 public/ 下。
  //
  // 注意 favicon.ico **不能**放 `app/`：Next 会把它当图片资源处理，而这份 ico 里嵌的
  // PNG 不是 RGBA，`next build` 会直接报 "Processing image failed / The PNG is not in
  // RGBA format" 而失败。放 public/ 就是静态直出，浏览器照样认。
  icons: {
    icon: [
      { url: "/favicon.ico", sizes: "any" },
      { url: "/brand/favicon-32.png", type: "image/png", sizes: "32x32" },
      { url: "/brand/icon-192.png", type: "image/png", sizes: "192x192" },
    ],
    apple: [{ url: "/brand/apple-touch-icon.png" }],
  },
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
