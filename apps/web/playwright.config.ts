import { defineConfig, devices } from "@playwright/test";

/**
 * E2E 与可访问性/性能审计（agent.md §16 G4 / G8、§17.2 的 playwright）。
 *
 * 被测站点是**已经在跑**的那一套（Go 8640 + Next 3000），本配置不负责拉起服务：
 * 先用 runbook §22 的步骤把栈跑起来，再 `pnpm test:e2e`。默认打 8640（真实边缘路径），
 * 也可以用 `E2E_BASE_URL=https://blog.zjut.si` 直接审计线上。
 *
 * 浏览器用系统已装的 Edge（`channel: "msedge"`）：这样不必下载 Playwright 自带的
 * Chromium，本地与 CI 都省一步，也更贴近用户实际环境。
 */
const baseURL = process.env.E2E_BASE_URL ?? "http://127.0.0.1:8640";

export default defineConfig({
  testDir: "./e2e",
  timeout: 30_000,
  expect: { timeout: 5_000 },
  fullyParallel: false,
  workers: 1,
  reporter: [["list"]],
  use: {
    baseURL,
    channel: "msedge",
    locale: "zh-CN",
    timezoneId: "Asia/Shanghai",
    trace: "off",
    screenshot: "off",
  },
  projects: [
    {
      name: "desktop",
      use: { ...devices["Desktop Chrome"], viewport: { width: 1280, height: 900 } },
    },
    {
      name: "mobile-375",
      use: { ...devices["Desktop Chrome"], viewport: { width: 375, height: 720 } },
    },
  ],
});
