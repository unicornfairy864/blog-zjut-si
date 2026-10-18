import path from "node:path";
import type { NextConfig } from "next";

// 契约要点（agent.md §8 / §12）：
// - standalone 产物，Node 堆由 --max-old-space-size=384 限制；
// - 构建期不访问 API（所有取数在运行时）；
// - 不使用 next/image，图片统一由 <PostImage> 输出 <img srcset>；
// - 公开页 HTML 不缓存（no-cache + ETag），管理端全部 no-store。
//
// Windows 本地验证：非开发者模式下无法创建 standalone 需要的软链，会报 EPERM。
// 只在本机跑类型/页面验证时用 `NEXT_DISABLE_STANDALONE=1 pnpm build`；
// Docker 与 CI 都在 Linux 上构建，默认始终输出 standalone。
const disableStandalone = process.env.NEXT_DISABLE_STANDALONE === "1";

const nextConfig: NextConfig = {
  output: disableStandalone ? undefined : "standalone",
  // standalone 以 apps/web 为根，容器里直接 node server.js 即可。
  outputFileTracingRoot: path.resolve(process.cwd()),
  reactStrictMode: true,
  poweredByHeader: false,
  images: { unoptimized: true },

  async headers() {
    return [
      {
        // 文件名含哈希，永久缓存。
        source: "/_next/static/:path*",
        headers: [
          { key: "Cache-Control", value: "public, max-age=31536000, immutable" },
        ],
      },
      {
        source: "/admin/:path*",
        headers: [{ key: "Cache-Control", value: "no-store" }],
      },
      {
        source: "/api/:path*",
        headers: [{ key: "Cache-Control", value: "no-store" }],
      },
      {
        source: "/:path*",
        headers: [
          { key: "X-Content-Type-Options", value: "nosniff" },
          { key: "Referrer-Policy", value: "strict-origin-when-cross-origin" },
          { key: "Cache-Control", value: "no-cache" },
        ],
      },
    ];
  },
};

export default nextConfig;
