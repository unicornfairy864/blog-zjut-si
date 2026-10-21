import { headers } from "next/headers";

import { serializeJsonLd } from "@/lib/seo";

export interface JsonLdProps {
  /** 结构化数据对象；由 `lib/seo.ts` 的构造器生成。 */
  data: Record<string, unknown>;
}

/**
 * JSON-LD 注入（agent.md §11 / §9）。
 *
 * CSP 是 `script-src 'self' 'nonce-…' 'strict-dynamic'` 且禁止 `unsafe-inline`，
 * 所以这段脚本必须带上 `middleware.ts` 每请求注入的 nonce；`<` 统一转义成
 * `\u003c`，防止正文里的 `</script>` 提前闭合标签。
 *
 * `headers()` 在 Next 15 是异步的，因此本组件是 async server component。
 */
export async function JsonLd({ data }: JsonLdProps) {
  const nonce = (await headers()).get("x-nonce") ?? undefined;
  return (
    <script
      type="application/ld+json"
      nonce={nonce}
      dangerouslySetInnerHTML={{ __html: serializeJsonLd(data) }}
    />
  );
}
