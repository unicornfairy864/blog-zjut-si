import { NextResponse, type NextRequest } from "next/server";

import { apiBaseUrl } from "@/lib/constants/env";
import { SITE } from "@/lib/constants/site";

/**
 * CSP、nonce 与 410（agent.md §9 / §11）。
 *
 * 禁止 unsafe-inline：Next 的 RSC 水合脚本通过请求头里的 nonce 自动带上，
 * 因此这里同时写请求头与响应头。style 仍保留 unsafe-inline（shadcn 组件有内联样式）。
 * public 页与管理页都声明 force-dynamic，所以每份 HTML 的 nonce 都是当次的。
 *
 * 另外：归档 / 已删除的文章要返回 **410**，而 App Router 的页面无法自定义状态码
 * （`notFound()` 只会给 404），所以详情路径在这里先问一次 API，是 410 就直接回。
 * 客户端导航（RSC 请求）交给页面按 404 处理，避免把 HTML 塞进 RSC 响应。
 */
const isDev = process.env.NODE_ENV !== "production";

/** 只拦 `/posts/<slug>` 这一层，且不带子路径。 */
const POST_DETAIL_PATH = /^\/posts\/[^/]+$/;

/** 探测超时：本地回环，正常在毫秒级。 */
const PROBE_TIMEOUT_MS = 3000;

function buildCsp(nonce: string): string {
  return [
    "default-src 'self'",
    `script-src 'self' 'nonce-${nonce}' 'strict-dynamic'${isDev ? " 'unsafe-eval'" : ""}`,
    "style-src 'self' 'unsafe-inline'",
    "img-src 'self' data: blob:",
    "font-src 'self' data:",
    `connect-src 'self'${isDev ? " ws: wss:" : ""}`,
    "object-src 'none'",
    "base-uri 'self'",
    "form-action 'self'",
    "frame-ancestors 'none'",
  ].join("; ");
}

/** 是不是 Next 的客户端导航 / 预取请求。 */
function isClientNavigation(request: NextRequest): boolean {
  return (
    request.headers.get("rsc") === "1" ||
    request.headers.has("next-router-state-tree") ||
    request.headers.get("next-router-prefetch") === "1" ||
    request.headers.get("purpose") === "prefetch"
  );
}

/** 问一次 API：这篇文章是不是已下线（410）。失败一律当作"不是"，交给页面处理。 */
async function isGone(pathname: string): Promise<boolean> {
  const slug = pathname.slice("/posts/".length);
  try {
    const response = await fetch(
      `${apiBaseUrl()}/api/posts/${encodeURIComponent(slug)}`,
      {
        headers: { Accept: "application/json" },
        cache: "no-store",
        signal: AbortSignal.timeout(PROBE_TIMEOUT_MS),
      },
    );
    return response.status === 410;
  } catch {
    return false;
  }
}

/** 410 的独立页面；不加载任何脚本，也不需要水合。 */
function goneHtml(): string {
  return [
    "<!doctype html>",
    `<html lang="zh-CN"><head><meta charset="utf-8">`,
    '<meta name="viewport" content="width=device-width, initial-scale=1">',
    '<meta name="robots" content="noindex">',
    `<title>内容已下线 · ${SITE.name}</title></head>`,
    '<body style="margin:0;min-height:100vh;display:flex;flex-direction:column;align-items:center;justify-content:center;gap:12px;font-family:system-ui,-apple-system,sans-serif;color:#111827">',
    '<p style="margin:0;font-size:14px;color:#6b7280">410</p>',
    '<h1 style="margin:0;font-size:20px">这篇内容已下线</h1>',
    '<a href="/" style="font-size:14px;color:#111827">回到首页</a>',
    "</body></html>",
  ].join("");
}

export async function middleware(request: NextRequest) {
  const nonce = Buffer.from(crypto.randomUUID()).toString("base64");
  const csp = buildCsp(nonce);

  if (
    request.method === "GET" &&
    POST_DETAIL_PATH.test(request.nextUrl.pathname) &&
    !isClientNavigation(request) &&
    (await isGone(request.nextUrl.pathname))
  ) {
    return new NextResponse(goneHtml(), {
      status: 410,
      headers: {
        "Content-Type": "text/html; charset=utf-8",
        "Cache-Control": "no-cache",
        "Content-Security-Policy": csp,
        "X-Content-Type-Options": "nosniff",
        "Referrer-Policy": "strict-origin-when-cross-origin",
      },
    });
  }

  const requestHeaders = new Headers(request.headers);
  requestHeaders.set("x-nonce", nonce);
  requestHeaders.set("Content-Security-Policy", csp);

  const response = NextResponse.next({ request: { headers: requestHeaders } });
  response.headers.set("Content-Security-Policy", csp);
  return response;
}

export const config = {
  matcher: [
    {
      source: "/((?!_next/static|_next/image|favicon.ico).*)",
      missing: [
        { type: "header", key: "next-router-prefetch" },
        { type: "header", key: "purpose", value: "prefetch" },
      ],
    },
  ],
};
