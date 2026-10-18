# 接口清单

> 契约以 [`agent.md`](agent.md) §5 为准；本文件随实现更新，D15 起改由 OpenAPI 生成。
> 所有响应都是 `{code,data,msg}` 信封；时间一律 Unix 秒（整数）。

## 通用约定

- 前缀 `/api`；成功 `HTTP 200` + `{"code":0,"data":<数据>,"msg":"操作成功"}`
- 失败：HTTP 状态由 `code` 查表；`data` 放字段错误或引用清单
- 分页入参 `page`(≥1) / `pageSize`(1–50, 默认 20) / `keyword`；出参 `data:{list,total,page,pageSize}`
- 公开读接口带 `Cache-Control: no-cache` 与强 `ETag`，支持 `If-None-Match → 304`
- 管理端写操作必须带 `X-CSRF-Token`（值来自 `GET /auth/me` 的 `csrfToken`）
- 会话 cookie：`sid`，`HttpOnly` `SameSite=Strict` `Path=/`，7 天滑动过期

## 公开组（8640）

### `GET /api/system/ping`

骨架打通端点。限流 20 r/s/IP。

```json
{ "code": 0, "data": { "pong": true, "time": 1760000000 }, "msg": "操作成功" }
```

### `GET /api/posts`

已发布文章列表（置顶优先、发布时间倒序）；草稿、定时、归档、回收站均不可见。

```json
{
  "code": 0,
  "data": {
    "list": [
      {
        "id": 1, "slug": "hello", "title": "你好", "excerpt": "……",
        "coverMediaId": null, "publishedAt": 1760000000, "updatedAt": 1760000000,
        "wordCount": 1024, "readingTime": 4, "isPinned": false
      }
    ],
    "total": 1, "page": 1, "pageSize": 20
  },
  "msg": "操作成功"
}
```

### `GET /api/posts/{slug}`

已发布文章详情，返回服务端渲染好的 `contentHtml`（不含 Markdown 源码）。

| HTTP | code | 场景 |
| --- | --- | --- |
| 200 | 0 | 已发布 |
| 404 | 1002 | 不存在 / 草稿 / 定时（不泄露存在性） |
| 410 | 1007 | 已归档 / 已删除 |

### `GET /api/search?q=关键词`

≥3 字走 FTS5（trigram，`bm25` 排序），1–2 字走 `LIKE` 兜底；只搜已发布文章。
`q` 长度 1–64 字，超限 `400 / 1003`。

## 管理组（8641，只经 SSH 隧道）

### 鉴权

| 端点 | 说明 |
| --- | --- |
| `POST /api/admin/auth/login` | 无鉴权、无 CSRF；5 次/分钟；10 分钟 5 次失败锁定 30 分钟 |
| `GET /api/admin/auth/me` | 需要登录；返回 `admin`、`csrfToken`、`expiresAt` |
| `POST /api/admin/auth/logout` | 需要登录 + CSRF；删除会话并清 cookie |

登录失败码：`1000`(401) 口令错误、`1003`(400) 参数非法、`1005`(429) 限流、`2005`(423) 已锁定。

### 文章

| 端点 | 说明 |
| --- | --- |
| `GET /api/admin/posts` | 列表；`status` 可筛 `draft/scheduled/published/archived`，`keyword` 匹配标题与 slug |
| `POST /api/admin/posts` | 新建草稿；`slug` 留空由服务端生成 |
| `GET /api/admin/posts/{id}` | 详情（含 `contentMd` 与 `contentHtml`） |
| `PUT /api/admin/posts/{id}` | 覆盖式保存，必须带 `title` 与 `contentMd` |
| `PATCH /api/admin/posts/{id}` | 局部更新；`nil` 字段不改 |
| `DELETE /api/admin/posts/{id}` | 软删（回收站，30 天后物理删除） |
| `POST /api/admin/posts/{id}/publish` | 发布；写版本快照并 `revision+1` |
| `POST /api/admin/posts/{id}/unpublish` | 取消发布 / 取消归档，回到草稿 |
| `GET /api/admin/posts/{id}/revisions` | 版本列表，最多 50 条 |
| `POST /api/admin/posts/preview` | 渲染未保存正文，不落库；与发布结果同源 |

写入字段：`title`、`slug`、`excerpt`、`contentMd`、`seoTitle`、`seoDescription`、`canonicalUrl`、
`coverMediaId`、`ogMediaId`、`seriesId`、`seriesOrder`、`isPinned`、`allowComments`、`lang`；
`PATCH` 另支持 `status`（仅 `archived` 归档、`draft` 取消发布）与 `createRedirect`（改 slug 时写 301）。

文章错误码：

| code | HTTP | 场景 |
| --- | --- | --- |
| 1002 | 404 | 文章不存在 |
| 1003 | 400 | 参数校验失败（`data` 为字段错误数组） |
| 1004 | 409 | 非法状态迁移（如重复发布、草稿归档） |
| 1005 | 429 | 公开接口限流 |
| 1007 | 410 | 公开端访问已归档/已删除文章 |
| 2001 | 409 | slug 冲突 |
| 2004 | 409 | 已发布文章改 slug 未写跳转 |

### 预览

```json
// POST /api/admin/posts/preview  →  data
{ "html": "<h2 id=\"h-1a2b3c4d\">标题</h2>", "text": "标题", "wordCount": 2, "readingTime": 1, "excerpt": "标题" }
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

`/site`、`/tags*`、`/archive`、`/series*`、`/links`、`/pages/*`、`/events/view`、
`/attachments/*`、`/preview/{token}`、`/posts/{slug}/related`、`/posts/{slug}/comments`、
`/posts/{slug}/like`、媒体与评论管理、`/stats/*`、`/jobs*`、`/audit-logs`、`POST /api/internal/revalidate`。
