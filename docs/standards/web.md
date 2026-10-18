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

## 3. 管理端会话与 CSRF

- `app/(admin)/admin/(protected)/layout.tsx`：`useAuth()` → `isLoading` 时只渲染加载态；`401` 时 `router.replace("/admin/login")`，避免闪烁。
- `useAuth()` 内部会调 `GET /auth/me` 并把 `csrfToken` 存入内存（`lib/api/csrf.ts`）。
- 写请求自动带 `X-CSRF-Token`；403 CSRF 失败时提示用户刷新页面，不自动重试。

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
