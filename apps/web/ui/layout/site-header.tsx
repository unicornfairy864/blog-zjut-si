import Link from "next/link";

import { SITE } from "@/lib/constants/site";

/** 公开页页头；导航项随 D9/D10 逐页接通。 */
export function SiteHeader() {
  return (
    <header className="border-b border-border">
      <div className="container flex h-14 items-center justify-between">
        <Link href="/" className="font-semibold tracking-tight">
          {SITE.name}
        </Link>
        <nav aria-label="主导航" className="flex items-center gap-6 text-sm text-muted-foreground">
          <Link href="/" className="hover:text-foreground">
            首页
          </Link>
        </nav>
      </div>
    </header>
  );
}
