# 0001 · 双 listener 边缘与 8641 内部路由分发

日期：2026-10-18
状态：已采纳

## 背景

契约要求三个 gin engine（public / admin / internal）各自成文件（§3.2.7），同时
§2 又规定 `/api/internal/*`、`/healthz`、`/readyz` 挂在 8641 这一个端口上。
一个 `http.Server` 只能有一个 Handler，两个 engine 不能同时直接监听同一端口。

此外 §2 的 8641 路由表只放行 `/admin*` 与 `/_next/*`，而 §8 要求未登录跳转登录页。
如果登录页放在 `/login`，它会被 `其余 → 404` 规则挡住。

## 选项

1. **第三个端口（:8642）给 internal**：违反"只发布 8640/8641"的端口契约。
2. **把 internal 路由直接注册进 admin engine**：engine 文件仍在，但"三个 engine"名存实亡。
3. **纯路径分发的 gateway**：admin 与 internal 各自是完整 engine，8641 的 Handler 是一个
   按前缀分流的 `http.Handler`（`router.NewAdminGateway`）。
4. **登录页放 `/login` 并给 8641 路由表加白名单**：需要改 §2，且与"默认 404"的收敛原则冲突。
5. **登录页放 `/admin/login`**（本决定）：不动路由表，只改 §8 的跳转路径。

## 决定

- 采用 3：`initialize.Run` 里 `router.NewAdminGateway(adminEngine, internalEngine)`；分流规则是
  `/healthz`、`/readyz`、`/api/internal/*` 归 internal，其余归 admin。
- 采用 5：登录页为 `app/(admin)/admin/login/page.tsx`，未登录一律跳 `/admin/login`；
  `(protected)` 路由组承载会话守卫，登录页不在其中。

## 后果

- 8640 的 `/admin*`、`/api/admin/*`、`/api/internal/*`、`/uploads/*`、健康检查由单一的
  `edge.IsPrivatePath` 判定为 404，C4 只有一个真相来源。
- 8641 的放行清单收敛为 `/admin*`、`/_next/*`、`/favicon.ico`、`/robots.txt`，其余 404。
- `edge.IsAdminPagePath` 与 `edge.IsPrivatePath` 必须成对维护：新增页面路径时两处都要看。
- Next 的深链（如 `/admin/posts/1`）只要前缀是 `/admin` 就能过，未来若加 `/login` 之外的非
  `/admin` 管理页（例如 `/preview`），需要在 8641 放行清单里显式添加。
