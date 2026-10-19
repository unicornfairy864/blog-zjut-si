import type { Metadata } from "next";

import { publicApi } from "@/lib/api/public";

export const dynamic = "force-dynamic";

export const metadata: Metadata = {
  title: "友链",
  description: "常看的朋友们的站点。",
};

/** 友链页（agent.md §5 `GET /links`）。 */
export default async function LinksPage() {
  const { list } = await publicApi.links();

  return (
    <div className="mx-auto w-full max-w-3xl space-y-6">
      <header className="space-y-2">
        <h1 className="text-2xl font-semibold tracking-tight">友链</h1>
        <p className="text-xs text-muted-foreground">共 {list.length} 条</p>
      </header>

      {list.length === 0 ? (
        <p className="rounded-lg border border-dashed border-border px-4 py-12 text-center text-sm text-muted-foreground">
          还没有添加友链。
        </p>
      ) : (
        <ul className="grid gap-4 sm:grid-cols-2">
          {list.map((link) => (
            <li
              key={link.id}
              className="flex min-w-0 items-start gap-3 rounded-lg border border-border p-4"
            >
              {link.avatar ? (
                <img
                  src={link.avatar}
                  alt=""
                  width={40}
                  height={40}
                  loading="lazy"
                  className="h-10 w-10 shrink-0 rounded-full object-cover"
                />
              ) : null}
              <div className="min-w-0 space-y-1">
                <a
                  href={link.url}
                  target="_blank"
                  rel="noopener noreferrer"
                  className="break-words font-medium underline-offset-4 hover:underline"
                >
                  {link.name}
                </a>
                {link.description ? (
                  <p className="break-words text-xs leading-5 text-muted-foreground">
                    {link.description}
                  </p>
                ) : null}
              </div>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
