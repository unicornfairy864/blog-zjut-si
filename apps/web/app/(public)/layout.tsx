import type { ReactNode } from "react";

import { SiteFooter } from "@/ui/layout/site-footer";
import { SiteHeader } from "@/ui/layout/site-header";
import { PageViewTracker } from "@/ui/post/page-view-tracker";

/** 公开页布局：SSR + 语义化结构（agent.md §8 / §11）。 */
export default function PublicLayout({ children }: { children: ReactNode }) {
  return (
    <div className="flex min-h-screen flex-col">
      <SiteHeader />
      {/* 非文章页的 PV 打点（文章详情页自己打，带 slug）——「全站 PV/UV」要真的是全站。 */}
      <PageViewTracker />
      <main className="container flex-1 py-10">{children}</main>
      <SiteFooter />
    </div>
  );
}
