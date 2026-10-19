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

/** 公开页页头。 */
export function SiteHeader() {
  return (
    <header className="border-b border-border">
      <div className="container flex flex-wrap items-center gap-x-6 gap-y-1 py-3">
        <Link
          href="/"
          className="font-semibold tracking-tight underline-offset-4 hover:underline"
        >
          {SITE.name}
        </Link>
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
