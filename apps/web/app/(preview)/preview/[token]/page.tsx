import type { Metadata } from "next";
import { notFound } from "next/navigation";

export const metadata: Metadata = {
  title: "预览",
  robots: { index: false, follow: false, nocache: true },
};

/**
 * 草稿预览页（/preview/{token}）。
 *
 * 真实取数在 D13：GET /api/preview/{token}（no-store），一次性令牌，
 * 过期或已用返回 410/2006。本轮先占位，确保路由与 8640 的转发规则成立。
 */
export default async function PreviewPage({
  params,
}: {
  params: Promise<{ token: string }>;
}) {
  const { token } = await params;
  if (!token || token.length > 128) {
    notFound();
  }

  return (
    <main className="container max-w-2xl space-y-4 py-16">
      <h1 className="text-2xl font-semibold tracking-tight">草稿预览</h1>
      <p className="text-sm leading-6 text-muted-foreground">
        预览链路将在 D13 接通（一次性令牌、24 小时过期、返回 410/2006）。
        当前令牌片段：<code className="font-mono">{token.slice(0, 8)}…</code>
      </p>
    </main>
  );
}
