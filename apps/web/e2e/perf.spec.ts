import { expect, test, type Page } from "@playwright/test";

import { publicRoutes } from "./helpers";

/**
 * 性能断言（agent.md §15 / §16 G8）。
 *
 * 阈值：文章页 TTFB <300ms（**无缓存回源**，即服务端自己那段）、LCP <2.5s、
 * 单篇 JS <150KB gzip。
 *
 * 为什么要拆开算：`PerformanceNavigationTiming.responseStart` 从导航开始计时，
 * 把 DNS、TCP 握手、TLS 握手全算进去了。审计线上站点时这几项可能有几百毫秒
 * （跨洲链路尤其明显），直接拿它比 300ms 会把"网络慢"误判成"服务端慢"。
 * 契约要的是回源耗时，所以这里用
 *
 *     服务端段 ≈ (responseStart - requestStart) - (connectEnd - connectStart)
 *
 * 近似：`requestStart → responseStart` 是"请求发出到首字节"，含一个网络往返 +
 * 服务端处理；`connectEnd - connectStart` 是 TCP 握手，也约等于一个往返，减掉即得。
 * LCP 是用户感知指标，**不**做这个扣除（网络慢就该体现出来）。
 */
const TTFB_BUDGET_MS = 300;
const LCP_BUDGET_MS = 2500;
const ARTICLE_JS_BUDGET_BYTES = 150 * 1024;

/**
 * 链路质量闸门：LCP 是端到端感知指标，网络往返超过这个值时就无法区分
 * "站点慢"和"链路慢"（审计线上时见过 TCP 握手 4.2s 的情况），此时跳过 LCP 断言
 * 并在报告里注明，而不是报一个假失败。
 */
const MAX_MEANINGFUL_RTT_MS = 500;

interface Timing {
  /** 从导航开始到首字节（含 DNS/TCP/TLS）。 */
  rawTtfb: number;
  /** 请求发出到首字节。 */
  wait: number;
  /** TCP 握手耗时，作为单次网络往返的估计。 */
  rtt: number;
  /** 服务端段估计值。 */
  server: number;
  lcp: number;
}

/** 装 LCP 观察器：必须在页面脚本之前注入，否则拿不到早期候选。 */
async function installLcpObserver(page: Page): Promise<void> {
  await page.addInitScript(() => {
    (window as unknown as { __lcp: number }).__lcp = 0;
    try {
      new PerformanceObserver((list) => {
        for (const entry of list.getEntries()) {
          (window as unknown as { __lcp: number }).__lcp = entry.startTime;
        }
      }).observe({ type: "largest-contentful-paint", buffered: true });
    } catch {
      // 不支持就不记录，断言会跳过 LCP 那一段
    }
  });
}

async function readTiming(page: Page): Promise<Timing> {
  return page.evaluate(() => {
    const navigation = performance.getEntriesByType(
      "navigation",
    )[0] as PerformanceNavigationTiming | undefined;
    const lcp = (window as unknown as { __lcp?: number }).__lcp ?? 0;
    if (!navigation) {
      return { rawTtfb: 0, wait: 0, rtt: 0, server: 0, lcp };
    }
    const wait = navigation.responseStart - navigation.requestStart;
    const rtt = navigation.connectEnd - navigation.connectStart;
    return {
      rawTtfb: navigation.responseStart,
      wait,
      rtt,
      server: Math.max(0, wait - rtt),
      lcp,
    };
  });
}

/** 失败信息里把各段都摊开，省得下次还要猜是网络还是服务端。 */
function describe(timing: Timing): string {
  return (
    `服务端段≈${timing.server.toFixed(1)}ms（请求→首字节 ${timing.wait.toFixed(1)}ms ` +
    `− 网络往返 ${timing.rtt.toFixed(1)}ms）；含网络的原始 TTFB ${timing.rawTtfb.toFixed(1)}ms；` +
    `LCP ${timing.lcp.toFixed(1)}ms`
  );
}

test.describe("性能预算", () => {
  test("文章页 TTFB 与 LCP 都在预算内", async ({ page, request }) => {
    const routes = await publicRoutes(request);
    const articleRoute = routes.find((route) => route.startsWith("/posts/"));
    test.skip(!articleRoute, "站点里还没有已发布文章，跳过文章页性能断言");

    await installLcpObserver(page);
    await page.goto(articleRoute as string, { waitUntil: "load" });
    // 等一拍，让 LCP 候选稳定下来（load 之后仍可能更新）
    await page.waitForTimeout(1200);
    const timing = await readTiming(page);

    expect(timing.server, `文章页 TTFB 超预算 —— ${describe(timing)}`).toBeLessThan(
      TTFB_BUDGET_MS,
    );
    if (timing.lcp > 0 && timing.rtt <= MAX_MEANINGFUL_RTT_MS) {
      expect(timing.lcp, `文章页 LCP 超预算 —— ${describe(timing)}`).toBeLessThan(
        LCP_BUDGET_MS,
      );
    } else {
      test.info().annotations.push({
        type: "lcp-skipped",
        description: `网络往返 ${timing.rtt.toFixed(0)}ms（> ${MAX_MEANINGFUL_RTT_MS}ms），LCP 反映的是链路而非站点，已跳过`,
      });
    }
  });

  test("首页 TTFB 在预算内", async ({ page }) => {
    await page.goto("/", { waitUntil: "load" });
    const timing = await readTiming(page);
    expect(timing.rawTtfb).toBeGreaterThan(0);
    expect(timing.server, `首页 TTFB 超预算 —— ${describe(timing)}`).toBeLessThan(
      TTFB_BUDGET_MS,
    );
  });

  test("文章页首屏 JS 在 150KB gzip 预算内（不含公式）", async ({ page, request }) => {
    const routes = await publicRoutes(request);
    const articleRoute = routes.find((route) => route.startsWith("/posts/"));
    test.skip(!articleRoute, "站点里还没有已发布文章，跳过 JS 预算断言");

    const transferred: Array<{ url: string; bytes: number }> = [];
    page.on("response", async (response) => {
      const url = response.url();
      if (!url.includes("/_next/static/") || !url.endsWith(".js")) {
        return;
      }
      const length = Number(response.headers()["content-length"] ?? "0");
      transferred.push({ url, bytes: length });
    });

    await page.goto(articleRoute as string, { waitUntil: "load" });
    await page.waitForTimeout(500);

    // content-length 是**压缩后**的线上字节数，正对契约口径。
    const total = transferred.reduce((sum, item) => sum + item.bytes, 0);
    expect(total, `首屏 JS 共 ${(total / 1024).toFixed(1)}KB gzip，超预算`).toBeLessThan(
      ARTICLE_JS_BUDGET_BYTES,
    );
  });
});
