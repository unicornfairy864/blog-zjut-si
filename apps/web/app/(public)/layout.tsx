import type { ReactNode } from "react";

import { SiteFooter } from "@/ui/layout/site-footer";
import { SiteHeader } from "@/ui/layout/site-header";

/** 公开页布局：SSR + 语义化结构（agent.md §8 / §11）。 */
export default function PublicLayout({ children }: { children: ReactNode }) {
  return (
    <div className="flex min-h-screen flex-col">
      <SiteHeader />
      <main className="container flex-1 py-10">{children}</main>
      <SiteFooter />
    </div>
  );
}
