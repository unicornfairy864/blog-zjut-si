import { expect, test } from "@playwright/test";

import { apiData } from "./helpers";

/**
 * 等待反馈（agent.md §8）。
 *
 * 公开页是 `force-dynamic`，点链接要等服务端把 HTML 渲染完；没有 `loading.tsx`
 * 时界面上**什么都不变**，用户会以为没点中。这里把 RSC 导航请求按住 2.5 秒，
 * 断言骨架屏（`role="status"`）确实出现在屏幕上，随后又被真实内容替换掉。
 */
const HOLD_MS = 2500;

/** 把客户端导航的 RSC 请求拖慢；只在导航期间生效。 */
async function holdRscRequests(page: import("@playwright/test").Page): Promise<void> {
  await page.route(/[?&]_rsc=/, async (route) => {
    await new Promise((resolve) => setTimeout(resolve, HOLD_MS));
    await route.continue();
  });
}

test.describe("等待反馈", () => {
  test("跳转到列表页时先出现骨架屏", async ({ page }) => {
    await page.goto("/", { waitUntil: "load" });
    await holdRscRequests(page);

    await page.getByRole("link", { name: "文章", exact: true }).click();

    const skeleton = page.locator('[role="status"]').first();
    await expect(skeleton).toBeVisible({ timeout: 5000 });
    await expect(skeleton).toContainText("正在加载");

    // 真实内容到达后骨架必须消失，不能卡住。
    await expect(page.getByRole("heading", { name: "全部文章" })).toBeVisible({
      timeout: 15000,
    });
    await expect(page.locator('[role="status"]')).toHaveCount(0);
  });

  test("跳转到文章页时先出现文章骨架屏", async ({ page, request }) => {
    const posts = await apiData<{ list: Array<{ slug: string; title: string }> }>(
      request,
      "/api/posts?page=1&pageSize=1",
    );
    const first = posts?.list?.[0];
    test.skip(!first, "站点里还没有已发布文章，跳过文章页骨架断言");

    await page.goto("/posts", { waitUntil: "load" });
    await holdRscRequests(page);

    await page.getByRole("link", { name: first!.title }).first().click();

    const skeleton = page.locator('[role="status"]').first();
    await expect(skeleton).toBeVisible({ timeout: 5000 });
    await expect(skeleton).toContainText("正在加载文章");

    await expect(page.getByRole("heading", { name: first!.title })).toBeVisible({
      timeout: 15000,
    });
    await expect(page.locator('[role="status"]')).toHaveCount(0);
  });
});
