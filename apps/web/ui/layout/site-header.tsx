import Link from "next/link";

import { SITE } from "@/lib/constants/site";

/** 公开页导航；375px 下允许换行，不产生横向滚动（agent.md §16 G4）。 */
const NAV_ITEMS = [
  { href: "/posts", label: "文章" },
  { href: "/archive", label: "归档" },
  { href: "/tags", label: "标签" },
  { href: "/series", label: "系列" },
  { href: "/links", label: "友链" },
  { href: "/about", label: "关于" },
  { href: "/search", label: "搜索" },
] as const;

/** 公开页页头：品牌（头像 → 个人主页，文字 → 本站首页）+ 导航。 */
export function SiteHeader() {
  return (
    <header className="border-b border-border">
      <div className="container flex flex-wrap items-center gap-x-6 gap-y-1 py-3">
        <div className="flex items-center gap-2">
          {/*
            头像指向个人主页（www.zjut.si），文字指向本站首页：两个目标不同，
            所以是两个并列的链接，而不是一个大链接里套小链接。
            头像用同标签跳转（不加 target="_blank"）：它是导航的一部分，不是外链引用。
          */}
          <a
            href={SITE.homeUrl}
            aria-label={`个人主页 ${SITE.homeUrl}`}
            className="shrink-0 rounded-md focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring focus-visible:ring-offset-2"
          >
            {/* 图标是方形的，圆角由 class 给；alt 留空，语义由外层链接的 aria-label 承担。 */}
            <img
              src="/brand/icon-192.png"
              alt=""
              width={28}
              height={28}
              className="h-7 w-7 rounded-md"
            />
          </a>
          <Link
            href="/"
            className="font-semibold tracking-tight underline-offset-4 hover:underline"
          >
            {SITE.navLabel}
          </Link>
        </div>
        <nav
          aria-label="主导航"
          className="flex flex-wrap items-center gap-x-4 gap-y-1 text-sm text-muted-foreground"
        >
          {NAV_ITEMS.map((item) => (
            <Link
              key={item.href}
              href={item.href}
              className="underline-offset-4 hover:text-foreground hover:underline"
            >
              {item.label}
            </Link>
          ))}
        </nav>
      </div>
    </header>
  );
}
