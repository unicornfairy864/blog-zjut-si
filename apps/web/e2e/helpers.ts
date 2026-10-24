import type { APIRequestContext, Page } from "@playwright/test";

/** 审计发现的单条问题。 */
export interface AuditIssue {
  rule: string;
  detail: string;
  selector: string;
}

/** 读一个公开 API 的信封，拿 `data`；失败返回 null（审计不该因为缺数据而挂掉）。 */
export async function apiData<T>(
  request: APIRequestContext,
  path: string,
): Promise<T | null> {
  try {
    const response = await request.get(path);
    if (!response.ok()) {
      return null;
    }
    const body = (await response.json()) as { code: number; data: T };
    return body.code === 0 ? body.data : null;
  } catch {
    return null;
  }
}

/**
 * 公开页路由清单。
 *
 * 详情类页面（文章 / 标签 / 系列 / 静态页）的 slug 从 API 现取：站点数据是作者自己
 * 灌的，硬编码 slug 会让审计在换库之后立刻失效。
 */
export async function publicRoutes(request: APIRequestContext): Promise<string[]> {
  const routes = ["/", "/posts", "/archive", "/tags", "/series", "/links", "/search"];

  const posts = await apiData<{ list: Array<{ slug: string }> }>(
    request,
    "/api/posts?page=1&pageSize=1",
  );
  const postSlug = posts?.list?.[0]?.slug;
  if (postSlug) {
    routes.push(`/posts/${postSlug}`);
  }

  const tags = await apiData<{ list: Array<{ slug: string }> }>(request, "/api/tags");
  const tagSlug = tags?.list?.[0]?.slug;
  if (tagSlug) {
    routes.push(`/tags/${tagSlug}`);
  }

  const series = await apiData<{ list: Array<{ slug: string }> }>(request, "/api/series");
  const seriesSlug = series?.list?.[0]?.slug;
  if (seriesSlug) {
    routes.push(`/series/${seriesSlug}`);
  }

  const pages = await apiData<{ list: Array<{ slug: string }> }>(request, "/api/pages");
  const pageSlug = pages?.list?.[0]?.slug;
  if (pageSlug) {
    routes.push(`/${pageSlug}`);
  }

  return routes;
}

/**
 * 在页面里跑一遍可访问性检查。
 *
 * 为什么自己写而不是装 axe-core：§17.2 的依赖白名单里只有 playwright，
 * 加 axe 需要先登记；这里要查的几条（标题层级、alt、可访问名、标签、对比度、
 * 地标）用计算样式就能判，不需要额外依赖。
 */
export async function scanA11y(page: Page): Promise<AuditIssue[]> {
  return page.evaluate(() => {
    const issues: AuditIssue[] = [];

    const describe = (el: Element): string => {
      const id = el.id ? `#${el.id}` : "";
      const cls =
        typeof el.className === "string" && el.className.trim()
          ? "." + el.className.trim().split(/\s+/).slice(0, 2).join(".")
          : "";
      return `${el.tagName.toLowerCase()}${id}${cls}`;
    };
    const text = (el: Element): string => (el.textContent ?? "").trim();
    // 显式 aria-hidden 的子树不在可访问性树里：不该再要求它有可访问名或标签。
    const hiddenFromAT = (el: Element): boolean => el.closest('[aria-hidden="true"]') !== null;

    // 1. 标题：页面骨架恰好一个 h1，且不跳级
    //
    // 正文（`.prose-blog`）里的标题由作者控制：作者写 `#` 就是 h1，这是合法的
    // Markdown 用法，所以这里只保证**页面自身**（标题区）的 h1 唯一。
    const headings = Array.from(document.querySelectorAll("h1,h2,h3,h4,h5,h6"));
    const h1Count = headings.filter(
      (h) => h.tagName === "H1" && !h.closest(".prose-blog"),
    ).length;
    if (h1Count !== 1) {
      issues.push({
        rule: "single-h1",
        detail: `页面骨架（正文之外）有 ${h1Count} 个 h1`,
        selector: "h1",
      });
    }
    let previous = 0;
    for (const heading of headings) {
      const level = Number(heading.tagName[1]);
      if (previous > 0 && level > previous + 1) {
        issues.push({
          rule: "heading-order",
          detail: `h${previous} 之后直接跳到 h${level}：「${text(heading).slice(0, 30)}」`,
          selector: describe(heading),
        });
      }
      previous = level;
    }

    // 2. 图片必须有 alt（装饰图允许 alt=""，但不能没有这个属性）
    for (const image of Array.from(document.querySelectorAll("img"))) {
      if (hiddenFromAT(image)) {
        continue;
      }
      if (!image.hasAttribute("alt")) {
        issues.push({
          rule: "img-alt",
          detail: `缺少 alt：${image.getAttribute("src") ?? "(无 src)"}`,
          selector: describe(image),
        });
      }
    }

    // 3. 链接与按钮必须有可访问名
    const accessibleName = (el: Element): string => {
      const aria = el.getAttribute("aria-label");
      if (aria?.trim()) {
        return aria.trim();
      }
      const labelledBy = el.getAttribute("aria-labelledby");
      if (labelledBy) {
        const target = document.getElementById(labelledBy);
        if (target && text(target)) {
          return text(target);
        }
      }
      if (text(el)) {
        return text(el);
      }
      const image = el.querySelector("img[alt]");
      if (image) {
        return (image.getAttribute("alt") ?? "").trim();
      }
      return (el.getAttribute("title") ?? "").trim();
    };
    for (const el of Array.from(document.querySelectorAll("a[href], button"))) {
      if (hiddenFromAT(el)) {
        continue;
      }
      if (!accessibleName(el)) {
        issues.push({
          rule: "accessible-name",
          detail: `${el.tagName.toLowerCase()} 没有可访问名`,
          selector: describe(el),
        });
      }
    }

    // 4. 表单控件必须有标签
    for (const el of Array.from(document.querySelectorAll("input, textarea, select"))) {
      const type = (el.getAttribute("type") ?? "").toLowerCase();
      if (["hidden", "submit", "button", "reset"].includes(type) || hiddenFromAT(el)) {
        continue;
      }
      // 注意不能用 `??` 串 `&&`：`el.id && …` 在 id 为空时返回的是 `""`，
      // 而 `"" ?? x` 仍然是 `""`（`??` 只兜 null/undefined），会误报缺标签。
      const idLabel = el.id
        ? document.querySelector(`label[for="${CSS.escape(el.id)}"]`)
        : null;
      const labelled =
        idLabel !== null ||
        el.closest("label") !== null ||
        (el.getAttribute("aria-label") ?? "").trim() !== "" ||
        (el.getAttribute("aria-labelledby") ?? "").trim() !== "";
      if (!labelled) {
        issues.push({
          rule: "control-label",
          detail: `${el.tagName.toLowerCase()}[type=${type || "text"}] 没有 label`,
          selector: describe(el),
        });
      }
    }

    // 5. 语言与地标
    if (!document.documentElement.getAttribute("lang")) {
      issues.push({ rule: "html-lang", detail: "<html> 缺 lang", selector: "html" });
    }
    if (!document.querySelector("main")) {
      issues.push({ rule: "landmark-main", detail: "缺少 <main> 地标", selector: "body" });
    }

    // 6. 对比度（AA：正文 4.5，大字号 3.0）
    type Rgba = { r: number; g: number; b: number; a: number };
    const parseColor = (value: string): Rgba | null => {
      const match = value.match(/rgba?\(([^)]+)\)/);
      if (!match) {
        return null;
      }
      const parts = match[1].split(",").map((piece) => Number.parseFloat(piece));
      return { r: parts[0], g: parts[1], b: parts[2], a: parts[3] ?? 1 };
    };
    const luminance = (color: Rgba): number => {
      const channel = (raw: number): number => {
        const v = raw / 255;
        return v <= 0.03928 ? v / 12.92 : Math.pow((v + 0.055) / 1.055, 2.4);
      };
      return 0.2126 * channel(color.r) + 0.7152 * channel(color.g) + 0.0722 * channel(color.b);
    };
    const backgroundOf = (el: Element): Rgba | null => {
      let node: Element | null = el;
      while (node) {
        const style = getComputedStyle(node);
        const color = parseColor(style.backgroundColor);
        if (color && color.a > 0.95) {
          return color;
        }
        if (style.backgroundImage !== "none") {
          return null; // 图片背景上算对比度没有意义
        }
        node = node.parentElement;
      }
      return { r: 255, g: 255, b: 255, a: 1 };
    };

    for (const el of Array.from(document.querySelectorAll("body *"))) {
      const hasOwnText = Array.from(el.childNodes).some(
        (node) => node.nodeType === 3 && (node.textContent ?? "").trim(),
      );
      if (!hasOwnText) {
        continue;
      }
      const style = getComputedStyle(el);
      if (
        style.visibility === "hidden" ||
        style.display === "none" ||
        Number.parseFloat(style.opacity) < 0.5
      ) {
        continue;
      }
      const foreground = parseColor(style.color);
      const background = backgroundOf(el);
      if (!foreground || !background) {
        continue;
      }
      const first = luminance(foreground);
      const second = luminance(background);
      const ratio = (Math.max(first, second) + 0.05) / (Math.min(first, second) + 0.05);
      const size = Number.parseFloat(style.fontSize);
      const weight = Number(style.fontWeight) || 400;
      const isLarge = size >= 24 || (size >= 18.66 && weight >= 700);
      const required = isLarge ? 3 : 4.5;
      if (ratio < required) {
        issues.push({
          rule: "contrast",
          detail: `对比度 ${ratio.toFixed(2)} < ${required}（${size}px/${weight}）：「${text(el).slice(0, 24)}」`,
          selector: describe(el),
        });
      }
    }

    return issues;
  });
}
