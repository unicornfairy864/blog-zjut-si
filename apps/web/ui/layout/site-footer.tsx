import { SITE } from "@/lib/constants/site";

/** 公开页页脚。 */
export function SiteFooter() {
  return (
    <footer className="border-t border-border">
      <div className="container flex h-16 items-center justify-between text-xs text-muted-foreground">
        <span>
          © {new Date().getFullYear()} {SITE.name}
        </span>
        <span>{SITE.description}</span>
      </div>
    </footer>
  );
}
