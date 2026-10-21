# 前端编码细则

> 上位契约：[`../agent.md`](../agent.md) §8 / §9 / §11 / §12。本文只补充"怎么写"。

## 1. 目录

```
apps/web/
├─ app/(public)/          公开页：SSR，服务端取数
├─ app/(admin)/admin/
│  ├─ (protected)/        需要会话的管理页（layout 做 401 守卫）
│  └─ login/              登录页，不被守卫包裹
├─ app/(preview)/preview/[token]/   草稿预览
├─ ui/{shadcn,layout,post,admin,shared}
├─ lib/{api,hooks,zod,constants,swr}
├─ styles/{globals.css,prose.css}
└─ tests/ e2e/
```

- `ui/shadcn/*` 是生成物，不手改；定制写在 `ui/` 下的 wrapper。
- 别名：`@/ui/*`、`@/lib/*`、`@/styles/*`、`@/app/*`（`@/*` 兜底到根）。

## 2. 数据访问

**只有 `lib/api/` 可以 fetch。**

| 场景 | 调用 | 基址 |
| --- | --- | --- |
| 公开页（服务端组件） | `publicApi.*` | `http://127.0.0.1:8640` |
| 管理页（浏览器） | `adminApi.*` / `apiFetch` | 同源 `/api/admin/*` |

```ts
// 好：组件只依赖域名函数
const { data } = await publicApi.ping();

// 坏：组件里拼路径 / 直接 fetch
fetch("http://127.0.0.1:8640/api/posts");
```

`apiFetch` 负责：拼基址、`credentials: same-origin`、注入 `X-CSRF-Token`、解 `{code,data,msg}` 信封、抛 `ApiError{code,status,message,details}`。组件只捕获 `ApiError`。

**业务错误一律 HTTP 200，只按 `code` 分支**（v1.14 起）：`!response.ok` 只在"响应不是信封"时兜底
（nginx 502 HTML 等），`status` 字段只作诊断，不用来判成功/失败。

## 3. 管理端会话与 CSRF

- `app/(admin)/admin/(protected)/layout.tsx`：`useAuth()` → `isLoading` 时只渲染加载态；`code 1000`（未登录，HTTP 200）时 `router.replace("/admin/login")`，避免闪烁。
- `useAuth()` 内部会调 `GET /auth/me` 并把 `csrfToken` 存入内存（`lib/api/csrf.ts`）。
- 写请求自动带 `X-CSRF-Token`；CSRF 失败（`code 1001`）时提示用户刷新页面，不自动重试。

## 4. 组件约定

- 文件 kebab-case，组件 PascalCase，props 类型 `XxxProps`，不用 `React.FC`。
- 只有 Next 约定文件（`page/layout/error/not-found/route/loading`）用 default export，其余具名导出。
- 颜色与间距只用 token（`bg-background`、`text-muted-foreground`、`gap-6`），禁止写死色值。
- 图片不用 `next/image`，统一 `<PostImage>` 输出 480/960/1600/2400 + blurhash 占位（D7 落地）。

## 5. 表单

```tsx
const schema = z.object({ username: z.string().trim().min(1, "请输入用户名") });
type LoginInput = z.infer<typeof schema>;

const { register, handleSubmit, formState: { errors, isSubmitting } } = useForm<LoginInput>({
  resolver: zodResolver(schema),
  defaultValues: { username: "" },
});
```

- schema 放 `lib/zod/`，约束与服务端 `binding` 一致。
- 用 `react-hook-form` 的非受控写法（`register`），不要每个字段一个 `useState`。
- 错误提示与字段用 `aria-describedby` / `aria-invalid` 关联。
- **密钥字段**（服务端不回显的 token / 地址）在表单里留空表示"保持不变"，要清空得单独一个
  `clearXxx` 复选框；提交时由 `secretValue(input, clear)` 归一成 `null`（不变）/ `""`（清空）/ 新值。
  设置页的告警卡片就是这么做的（`ui/admin/settings-form.tsx`）。

## 6. 可访问性

- 语义标签优先：`header/nav/main/section/footer`；一页只有一个 `main`。
- 焦点可见（`focus-visible:ring-*`）；图片必须有 `alt`。
- 对比度 AA；装饰性图标 `aria-hidden="true"`。
- `eslint-plugin-jsx-a11y` 规则全开，`pnpm lint` 必须干净。

## 7. 渲染与缓存

| 类型 | 规则 |
| --- | --- |
| 公开页 | `force-dynamic`（根 layout 已声明），服务端取数，HTML `no-cache` + ETag |
| 管理页 | 整树 `'use client'`，数据用 SWR，响应 `no-store` |
| 构建期 | 禁止访问 API；`next build` 不依赖后端在线 |

## 8. CSP

- `middleware.ts` 每请求生成 nonce，写入 `Content-Security-Policy` 的请求头与响应头。
- 禁止 `unsafe-inline` 的 `script-src`；`style-src` 保留 `unsafe-inline`（shadcn 需要）。
- 不要在组件里塞 `<script>`；确需第三方脚本时必须加 `nonce` 并先更新契约。

## 9. 质量门

```bash
pnpm lint && pnpm typecheck && pnpm test && pnpm build
```

提交前 `make check`。CI 与本地必须一致；`pnpm build` 通过即证明没有构建期取数。

两个本机特例：

- **standalone 与软链**：`output: 'standalone'` 在 Windows 非开发者模式下会因 `EPERM: symlink`
  构建失败。只在本机验证页面时用 `NEXT_DISABLE_STANDALONE=1 pnpm build`；Docker 与 CI 都在
  Linux 上构建，保持 standalone。
- **pnpm 安装脚本白名单**：pnpm 12 默认阻止依赖的 postinstall，白名单写在
  `apps/web/pnpm-workspace.yaml` 的 `allowBuilds`（当前放行 `esbuild`、`unrs-resolver`）。
  新增需要构建的依赖时必须在这里登记，否则 `pnpm install` 会以 `ERR_PNPM_IGNORED_BUILDS` 退出。

## 10. 公开端访客标识

公开端没有用户系统，公开 HTML 与公开读接口也不 `Set-Cookie`（`agent.md` §17.1），
所以"同一个访客"由 `lib/visitor.ts` 在 `localStorage`（键 `zjut:vid`）里生成一次随机串，
随写请求以 `X-Visitor-Id` 头带上：

```ts
// 好：lib/api/ 里的域名函数统一带访客头
return apiFetch<PostStats>(`/api/posts/${slug}/stats`, { headers: visitorHeaders() });
```

- 读到的值必须是 8–64 位 `[A-Za-z0-9_-]`，否则重新生成（形状不对服务端会退回 `ip|ua`）。
- SSR 或隐私模式抛错时返回空串：宁可这次请求不带标识，也不能让阅读页因为打点而崩。
- 计数（阅读数 / 点赞数）只允许客户端异步取，**不得进 SSR HTML**；`liked` 这类访客态
  读接口是 `no-store`，不参与 ETag。

## 11. 编辑器（正文）

- 工具栏（`ui/admin/editor-toolbar.tsx`）只做纯字符串计算：把"插入 / 包裹选区"翻译成
  `MarkdownEdit{value,selectionStart,selectionEnd}`，写回表单与恢复光标由 `post-editor` 统一做，
  工具栏不持有表单状态。
- 预览（`ui/admin/post-preview.tsx`）复用 `ui/post/PostContent`，因此公式（行内 `$x$` 与
  块级 `$$…$$`）与代码块复制按钮与服务端发布结果一致。
- 图片：拖拽文件或粘贴截图到正文框即上传并插入 `![alt](url)`；预览里的图片可点选成图片框，
  拖右下角手柄改宽度，松开时写回 `{width=N}`（`agent.md` §11「图片尺寸」）。
  拖动过程只改内联样式，写回 Markdown 只发生在 `pointerup`，否则会每个 pointermove 都触发
  一次 600ms 防抖预览。

## 12. SEO 与元数据（D15）

- 每个公开页都要有 `generateMetadata`：`title`、`description`、`alternates.canonical` 三者缺一不可，
  且**同一站点内 canonical 不重复**；分页列表的 canonical 是它自己（`/posts?page=2`），不要指回第 1 页。
- 绝对地址一律用 `SITE.url` 拼（`lib/seo.ts` 的 `absoluteUrl()`），不要在页面里手写域名。
- OG/Twitter：站点级默认写在布局，文章页覆写成 `type: "article"` 并带 `publishedTime`/`tags`/封面图
  （四档里的 `1600`）。**不做**动态 OG 图片（§17.2 白名单里没有生成库）。
- JSON-LD：纯数据构造放 `lib/seo.ts`（可单测），渲染放 `ui/seo/json-ld.tsx`；
  `<script type="application/ld+json">` **必须带 CSP nonce**（`(await headers()).get("x-nonce")`），
  `JSON.stringify` 结果里的 `<` 要转义成 `\u003c`。
- 不该进索引的页面显式声明 `robots: { index: false }`：`/search`、`/preview/*`、全部 `/admin*`。
- 爬虫入口：`app/sitemap.xml/route.ts` 与 `app/rss.xml/route.ts` 是 route handler，必须
  `export const dynamic = "force-dynamic"`（构建期绝不取数）；`app/robots.ts` 只依赖常量，
  构建期生成为静态文件即可。三者的 `Cache-Control: public, max-age=600` 写在
  `next.config.ts` 的 headers 里（必须排在 catch-all 之后，同名头后者覆盖前者）。
- 新增 `generateMetadata` 后要确认它没有被 `(public)/[slug]` 这类动态段抢走路由
  （Next 里静态路由优先，但必须在 `next build` 的路由表里核对一遍）。

