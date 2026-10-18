# 接口清单

> 本文件在 D15 由 OpenAPI 生成（`docs/api.md` 为产物）。当前为 D1–D3 已实现端点的手写清单。
> 契约以 [`agent.md`](agent.md) §5 为准。

## 通用约定

- 前缀 `/api`；成功 `HTTP 200` + `{"code":0,"data":<数据>,"msg":"操作成功"}`
- 失败：HTTP 状态由 `code` 查表；`data` 放字段错误或引用清单
- 分页入参 `page`(≥1) / `pageSize`(1–50, 默认 20) / `keyword`；出参 `data:{list,total,page,pageSize}`
- 时间一律 Unix 秒（整数）
- 管理端写操作必须带 `X-CSRF-Token`（值来自 `GET /auth/me` 的 `csrfToken`）
- 会话 cookie：`sid`，`HttpOnly` `SameSite=Strict` `Path=/`，7 天滑动过期

## 公开组（8640）

### `GET /api/system/ping`

骨架打通端点，不承载业务数据。限流 20 r/s/IP。

```json
{ "code": 0, "data": { "pong": true, "time": 1760000000 }, "msg": "操作成功" }
```

## 管理组（8641，只经 SSH 隧道）

### `POST /api/admin/auth/login`

无鉴权、无 CSRF。同一用户名 5 次/分钟；10 分钟内 5 次失败锁定 30 分钟。

请求：

```json
{ "username": "admin", "password": "……" }
```

成功（同时下发 `Set-Cookie: sid=…`）：

```json
{ "code": 0, "data": { "admin": { "id": 1, "username": "admin", "email": "", "lastLoginAt": 1760000000, "createdAt": 1760000000, "updatedAt": 1760000000 }, "expiresAt": 1760604800 }, "msg": "操作成功" }
```

失败：

| code | HTTP | 场景 |
| --- | --- | --- |
| 1000 | 401 | 用户名或密码错误 |
| 1003 | 400 | 参数缺失/超长（`data` 为字段错误数组） |
| 1005 | 429 | 超过 5 次/分钟 |
| 2005 | 423 | 已锁定，`data.lockedUntil` 为解锁时间戳 |

### `GET /api/admin/auth/me`

需要登录。返回管理员信息与**明文** `csrfToken`（服务端只存它的哈希，可复算校验）。

```json
{ "code": 0, "data": { "admin": { "id": 1, "username": "admin", "email": "", "lastLoginAt": 1760000000, "createdAt": 1760000000, "updatedAt": 1760000000 }, "csrfToken": "…", "expiresAt": 1760604800 }, "msg": "操作成功" }
```

未登录或会话过期：`401` + `code 1000`，同时清 cookie。

### `POST /api/admin/auth/logout`

需要登录 + `X-CSRF-Token`。删除会话并清 cookie。

```json
{ "code": 0, "data": null, "msg": "已登出" }
```

## 内部组（8641，仅容器内或隧道）

| 端点 | 说明 |
| --- | --- |
| `GET /healthz` | 进程存活，`{"status":"ok","time":…}` |
| `GET /readyz` | DB / 磁盘 <85% / uploads 可写 / 队列 <100；任一失败 503 |
| `GET /api/internal/health` | 走统一信封的内部健康检查 |

## 错误码

见 [`agent.md`](agent.md) §5。代码中只能引用 `model/common/errs` 的常量，禁止裸数字。

## 尚未实现（后续模块）

`/site`、`/posts*`、`/tags*`、`/archive`、`/series*`、`/search`、`/links`、`/pages/*`、`/events/view`、
`/attachments/*`、`/preview/{token}`、管理端 CRUD、`/stats/*`、`/jobs*`、`/audit-logs`、`POST /api/internal/revalidate`。
