import { SITE } from "@/lib/constants/site";

/**
 * 公开首页占位。
 *
 * D9 起替换为：置顶文章 + 最新文章列表 + 分页（全部服务端取数，走 publicApi）。
 */
export default function HomePage() {
  return (
    <section className="mx-auto max-w-2xl space-y-6 py-16">
      <p className="text-sm font-medium text-muted-foreground">{SITE.name}</p>
      <h1 className="text-3xl font-semibold tracking-tight">站点正在搭建</h1>
      <p className="text-base leading-7 text-muted-foreground">
        {SITE.description}
      </p>
      <p className="text-sm text-muted-foreground">
        当前进度：服务端骨架、数据库迁移、双 listener 边界与鉴权链路（D1–D3）。
      </p>
    </section>
  );
}
