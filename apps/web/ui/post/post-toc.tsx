/** 目录项：只取 h2 / h3（agent.md §11）。 */
export interface TocItem {
  id: string;
  text: string;
  level: 2 | 3;
}

/** 把 HTML 里的实体还原成纯文本标签（标题里可能有 &amp; 或代码）。 */
function toPlainText(html: string): string {
  return html
    .replace(/<[^>]*>/g, "")
    .replace(/&lt;/g, "<")
    .replace(/&gt;/g, ">")
    .replace(/&quot;/g, '"')
    .replace(/&#39;/g, "'")
    .replace(/&amp;/g, "&")
    .trim();
}

/**
 * 从服务端渲染好的正文里抽目录。
 *
 * 标题的 `id` 由渲染管线生成（heading.go），所以这里只做正则提取，不重新算锚点。
 */
export function extractToc(html: string): TocItem[] {
  const items: TocItem[] = [];
  const pattern = /<h([23])\b([^>]*)>([\s\S]*?)<\/h\1>/gi;
  let match: RegExpExecArray | null;
  while ((match = pattern.exec(html)) !== null) {
    const idMatch = /\bid="([^"]+)"/i.exec(match[2]);
    if (!idMatch) {
      continue;
    }
    const text = toPlainText(match[3]);
    if (!text) {
      continue;
    }
    items.push({
      id: idMatch[1],
      text,
      level: match[1] === "2" ? 2 : 3,
    });
  }
  return items;
}

export interface PostTocProps {
  items: TocItem[];
  className?: string;
}

/** 目录列表；锚点跳转由 CSS 的 scroll-mt 保证不贴顶。 */
export function PostToc({ items, className }: PostTocProps) {
  if (items.length === 0) {
    return null;
  }

  return (
    <nav aria-label="目录" className={className}>
      <p className="mb-3 text-xs font-medium uppercase tracking-wide text-muted-foreground">
        目录
      </p>
      <ul className="space-y-2 text-sm">
        {items.map((item) => (
          <li
            key={item.id}
            className={item.level === 3 ? "pl-4 text-muted-foreground" : ""}
          >
            <a
              href={`#${item.id}`}
              className="underline-offset-4 hover:underline"
            >
              {item.text}
            </a>
          </li>
        ))}
      </ul>
    </nav>
  );
}
