import { expect, test } from "@playwright/test";

import { publicRoutes, scanA11y } from "./helpers";

/**
 * 可访问性审计（agent.md §8「可访问性」/ §16 G4）。
 *
 * 每个公开页都要求：恰好一个 h1、标题不跳级、图片有 alt、链接与按钮有可访问名、
 * 表单控件有标签、有 <main> 地标、正文对比度达到 AA、375px 下无横向滚动。
 */
test.describe("公开页可访问性", () => {
  test("站点可达（拿不到就别把后面的一堆失败当成缺陷）", async ({ request }) => {
    const response = await request.get("/api/site");
    expect(
      response.ok(),
      "站点不可达：先用 runbook §22 把 Go(8640) + Next(3000) 跑起来，或设置 E2E_BASE_URL",
    ).toBe(true);
  });

  test("每个公开页都没有可访问性问题", async ({ page, request }) => {
    const routes = await publicRoutes(request);
    expect(routes.length).toBeGreaterThanOrEqual(7);

    const report: string[] = [];
    for (const route of routes) {
      const response = await page.goto(route, { waitUntil: "load" });
      expect(response?.status(), `${route} 应返回 200`).toBe(200);

      const issues = await scanA11y(page);
      if (issues.length > 0) {
        report.push(
          `${route}\n` +
            issues
              .map((issue) => `    [${issue.rule}] ${issue.selector} — ${issue.detail}`)
              .join("\n"),
        );
      }
    }
    expect(report.join("\n"), "存在可访问性问题").toBe("");
  });

  test("375px 下不出现横向滚动（§16 G4）", async ({ page, request }) => {
    const routes = await publicRoutes(request);

    const overflowing: string[] = [];
    for (const route of routes) {
      await page.setViewportSize({ width: 375, height: 720 });
      await page.goto(route, { waitUntil: "load" });
      const overflow = await page.evaluate(() => {
        const doc = document.documentElement;
        return {
          scrollWidth: doc.scrollWidth,
          innerWidth: window.innerWidth,
          // 找出真正超宽的元素，便于定位
          widest: Array.from(document.querySelectorAll("body *"))
            .filter((el) => el.getBoundingClientRect().right > window.innerWidth + 1)
            .slice(0, 3)
            .map(
              (el) =>
                `${el.tagName.toLowerCase()}.${String(el.className).split(/\s+/)[0]} right=${Math.round(el.getBoundingClientRect().right)}`,
            ),
        };
      });
      if (overflow.scrollWidth > overflow.innerWidth + 1) {
        overflowing.push(
          `${route}: scrollWidth=${overflow.scrollWidth} > ${overflow.innerWidth} ${overflow.widest.join(" | ")}`,
        );
      }
    }
    expect(overflowing.join("\n"), "存在横向滚动").toBe("");
  });

  test("键盘焦点可见（focus-visible 有非 none 的轮廓）", async ({ page, request }) => {
    const routes = await publicRoutes(request);
    const missing: string[] = [];

    for (const route of routes.slice(0, 4)) {
      await page.goto(route, { waitUntil: "load" });
      const firstLink = page.locator("a[href]").first();
      if ((await firstLink.count()) === 0) {
        continue;
      }
      await firstLink.focus();
      const outline = await firstLink.evaluate((el) => {
        const style = getComputedStyle(el);
        return {
          outlineStyle: style.outlineStyle,
          outlineWidth: style.outlineWidth,
          boxShadow: style.boxShadow,
        };
      });
      const visible =
        (outline.outlineStyle !== "none" && Number.parseFloat(outline.outlineWidth) > 0) ||
        (outline.boxShadow !== "none" && outline.boxShadow !== "");
      if (!visible) {
        missing.push(`${route}: a[href] 聚焦后没有可见轮廓`);
      }
    }
    expect(missing.join("\n")).toBe("");
  });
});
