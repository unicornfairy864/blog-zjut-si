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
        "wordCount": 1024, "readingTime": 4, "isPinned": false,
        "cover": {
          "sha256": "786a8d…", "ext": "jpg", "width": 2400, "height": 1200,
          "alt": "示例图", "blurhash": "L6Pj0^…",
          "url": "/uploads/78/786a8d…/1600.webp",
          "origUrl": "https://pan.example.com/public/blog/786a8d….jpg",
          "variants": { "480": "…/480.webp", "960": "…/960.webp", "1600": "…/1600.webp", "2400": "…/2400.webp" }
        },
        "tags": [{ "name": "Go", "slug": "go" }]
      }
    ],
    "total": 1, "page": 1, "pageSize": 20
  },
  "msg": "操作成功"
}
```

`cover` 无封面时为 `null`；`variants` 与 `url` 的规则同媒体库（`img_base_url` 为空时用相对路径）。
列表、搜索、标签、系列、相关文章返回的都是这个列表项，`prev` / `next` 只在详情里出现。

### `GET /api/posts/{slug}`

已发布文章详情，返回服务端渲染好的 `contentHtml`（不含 Markdown 源码）。
在列表项字段之外，详情还带 `contentHtml`、`seoTitle`、`seoDescription`、`canonicalUrl`、`lang`、
`allowComments`（false 时前端不显示评论表单）、`prev` / `next`（按 `published_at` 相邻，仅 published，边界为 `null`）。

配了 `cloudreve.public_base_url` 时，`contentHtml` 里的本站图片会在返回时被包上一层
「查看原图」链接（`<a class="media-orig" href="{public_base_url}/{sha256}.{ext}" target="_blank"
rel="noopener noreferrer">`）；图床未启用、图片未归档时原样返回，所以 `contentHtml` 的内容会随
归档进度变化（ETag 按最终响应体计算，不会出现脏缓存）。

同一处还会给正文里的本站图片补 `srcset`（该图的四档变体，不足两档不输出）、`sizes`、
`width`/`height` 与 `loading="lazy" decoding="async"`；`media` 表里查不到的图片原样保留。

| HTTP | code | 场景 |
| --- | --- | --- |
| 200 | 0 | 已发布 |
| 404 | 1002 | 不存在 / 草稿 / 定时（不泄露存在性） |
| 410 | 1007 | 已归档 / 已删除 |

### `GET /api/posts/{slug}/related`

相关文章（同标签或同系列，排除自身，最多 6 篇，按发布时间倒序）。

```json
{ "code": 0, "data": { "list": [ /* 公开列表项 */ ] }, "msg": "操作成功" }
```

### `GET /api/posts/{slug}/comments`

已通过（`approved`）的评论：顶层评论带一层 `replies`（嵌套固定两层，回复的回复会扁平到同一层）。

```json
{
  "code": 0,
  "data": {
    "total": 3,
    "list": [
      { "id": 1, "parentId": null, "authorName": "甲", "authorUrl": "",
        "contentHtml": "<p>你好</p>", "createdAt": 1760000000,
        "replies": [
          { "id": 2, "parentId": 1, "authorName": "乙", "authorUrl": "",
            "contentHtml": "<p>+1</p>", "createdAt": 1760000100 }
        ] }
    ]
  },
  "msg": "操作成功"
}
```

不返回邮箱、`ip_hash`、`ua`。文章不存在 / 草稿 → `404 / 1002`，归档 / 已删除 → `410 / 1007`。

顶层评论被标为 `spam` / `deleted` 后，它下面**已过审**的回复不会被藏起来，而是降级成顶层输出
（避免评论凭空消失）；所以 `total`（该文 `approved` 总数）可能大于 `list` 里实际可见的条数。

### `POST /api/posts/{slug}/comments`

请求体：

```json
{ "authorName": "甲", "authorEmail": "a@b.c", "authorUrl": "https://example.com",
  "contentMd": "你好", "parentId": null }
```

| 字段 | 规则 |
| --- | --- |
| `authorName` | 必填，1–50 字符 |
| `authorEmail` | 可空；非空需像邮箱且 ≤200 字符，**只入库不公开** |
| `authorUrl` | 可空；非空需 `http(s)://` 且 ≤200 字符 |
| `contentMd` | 必填，1–2000 字符，服务端渲染为 `contentHtml`（同一套 goldmark + bluemonday） |
| `parentId` | 可空；必须存在且属于同一篇文章 |

```json
{ "code": 0, "data": { "id": 5, "status": "pending", "createdAt": 1760000200 }, "msg": "操作成功" }
```

新评论一律 `pending`，审核通过后才出现在公开列表。限流 **3 次 / 10 分钟 / IP**，超限 `429 / 1005`；
文章 `allow_comments = 0` 时 `403 / 1001`。

### `GET /api/site`

站点级信息：`settings` 派生的时区 / 图片前缀 / 内容版本，加上公开计数。

```json
{
  "code": 0,
  "data": {
    "timezone": "Asia/Shanghai", "imgBaseUrl": "/uploads", "contentVersion": 1,
    "counts": { "posts": 12, "tags": 3, "series": 2, "links": 5 }
  },
  "msg": "操作成功"
}
```

### `GET /api/tags`

标签云；只含有已发布文章的标签，按文章数降序、名称升序。

```json
{
  "code": 0,
  "data": { "list": [{ "id": 1, "name": "Go", "slug": "go", "description": "", "count": 7 }] },
  "msg": "操作成功"
}
```

### `GET /api/tags/{slug}`

标签详情：`tag` + 该标签下的分页文章。标签不存在 → `404 / 1002`。

```json
{
  "code": 0,
  "data": {
    "tag": { "id": 1, "name": "Go", "slug": "go", "description": "" },
    "list": [ /* 公开列表项 */ ], "total": 7, "page": 1, "pageSize": 20
  },
  "msg": "操作成功"
}
```

### `GET /api/archive`

按年 / 月分组的已发布文章；年、月均倒序。

```json
{
  "code": 0,
  "data": {
    "total": 12,
    "years": [
      {
        "year": 2026, "count": 8,
        "months": [
          { "month": 10, "count": 3,
            "posts": [{ "id": 1, "slug": "x", "title": "X", "publishedAt": 1760000000 }] }
        ]
      }
    ]
  },
  "msg": "操作成功"
}
```

### `GET /api/series`

系列列表（带已发布文章数）。

```json
{
  "code": 0,
  "data": { "list": [{ "id": 1, "name": "从零写博客", "slug": "blog-from-scratch", "description": "", "count": 6 }] },
  "msg": "操作成功"
}
```

### `GET /api/series/{slug}`

系列详情；文章按 `series_order` 升序、其次发布时间升序。系列不存在 → `404 / 1002`。

```json
{
  "code": 0,
  "data": {
    "series": { "id": 1, "name": "从零写博客", "slug": "blog-from-scratch", "description": "" },
    "list": [ /* 公开列表项 */ ], "total": 6
  },
  "msg": "操作成功"
}
```

### `GET /api/links`

友链；只出 `status='active'` 的记录（取值 `active` / `hidden`），按 `sort` 升序、`id` 升序。

```json
{
  "code": 0,
  "data": { "list": [{ "id": 1, "name": "Cloudreve", "url": "https://pan.zjut.si", "avatar": "", "description": "" }] },
  "msg": "操作成功"
}
```

### `GET /api/pages/{slug}`

静态页（`/about` 等）。不存在 → `404 / 1002`；软删 → `410 / 1007`。

```json
{
  "code": 0,
  "data": {
    "slug": "about", "title": "关于", "contentHtml": "<p>…</p>",
    "seoTitle": "", "seoDescription": "", "updatedAt": 1760000000
  },
  "msg": "操作成功"
}
```

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

### 媒体

| 端点 | 说明 |
| --- | --- |
| `POST /api/admin/media` | `multipart/form-data`：`alt`（可选，必须排在 `file` 前）+ `file`；≤30MB、≤5000 万像素；内容 sha256 去重 |
| `GET /api/admin/media` | 媒体库分页，按创建时间倒序 |
| `DELETE /api/admin/media/{id}` | 未被引用才删除；被引用返回 `409 / 2002` 与引用清单 |
| `POST /api/admin/media/{id}/retry` | 从本地原图重新生成变体 |

上传成功（`data`）：

```json
{
  "id": 1, "sha256": "786a8d…", "originalName": "photo.jpg", "mime": "image/jpeg", "ext": "jpg",
  "bytes": 177841, "width": 2400, "height": 1200, "pixels": 2880000,
  "alt": "示例图", "blurhash": "L6Pj0^…", "status": "ready", "error": "",
  "url": "/uploads/78/786a8d…/1600.webp",
  "origUrl": "https://pan.example.com/public/blog/786a8d….jpg",
  "variants": {
    "480": "/uploads/78/786a8d…/480.webp",
    "960": "/uploads/78/786a8d…/960.webp",
    "1600": "/uploads/78/786a8d…/1600.webp",
    "2400": "/uploads/78/786a8d…/2400.webp"
  },
  "createdAt": 1760000000, "updatedAt": 1760000000
}
```

上传立即返回 `status: "processing"`，后台任务生成四档 WebP 与 blurhash 后变 `ready`；
客户端轮询 `GET /api/admin/media` 即可。`failed` 时 `error` 有原因、可 `retry`。

`origUrl` 是图床上的原图直链，只有图床启用、配置了 `cloudreve.public_base_url`、且该图
归档完成时才非空；否则是空串。它要求图床存储策略的「文件名规则」为 `{originname}`
（见 `docs/runbook.md` §11.5）。

`img_base_url` 为空时 URL 是相对路径 `/uploads/…`（公开站由 nginx 直出；
管理端 8641 直出 `/uploads/*` 供媒体库与编辑器预览）。

### 评论

| 端点 | 说明 |
| --- | --- |
| `GET /api/admin/comments` | 审核队列；`status` 缺省 `pending`，传 `all` 看全部，按创建时间倒序 |
| `PATCH /api/admin/comments` | 单条改状态：`{"id":1,"status":"approved"}`，只接受 `approved`/`spam`/`deleted` |
| `POST /api/admin/comments/bulk` | 批量改状态：`{"ids":[1,2],"status":"spam"}` → `{"updated":2}`（1–200 个 id） |

```json
// GET /api/admin/comments?status=pending  →  data
{
  "list": [
    { "id": 1, "postId": 3, "postSlug": "hello", "postTitle": "你好", "parentId": null,
      "authorName": "甲", "authorEmail": "a@b.c", "authorUrl": "",
      "contentMd": "你好", "contentHtml": "<p>你好</p>", "status": "pending", "createdAt": 1760000000 }
  ],
  "total": 1, "page": 1, "pageSize": 20
}
```

- 邮箱只在这里出现；`ip_hash` / `ua` 任何接口都不返回（§9）。
- 状态变更会重算 `posts.comment_count`（approved 计数）并写 `audit_logs`。
- `deleted` 是软删：记录保留，用 `status=deleted` 仍能查到。

## 内部组（8641，仅容器内或隧道）

| 端点 | 说明 |
| --- | --- |
| `GET /healthz` | 进程存活，`{"status":"ok","time":…}` |
| `GET /readyz` | DB / 磁盘 <85% / uploads 可写 / 队列 <100；任一失败 503 |
| `GET /api/internal/health` | 走统一信封的内部健康检查 |

## 错误码

见 [`agent.md`](agent.md) §5。代码中只能引用 `model/common/errs` 的常量，禁止裸数字。

## 尚未实现（后续模块）

`/events/view`、`/attachments/*`、`/preview/{token}`、`/posts/{slug}/like`、`/stats/*`、`/jobs*`、
`/audit-logs`、`/settings`、`/tags*`、`/series*`、`/links`、`/pages/*`、`/redirects*` 的管理端写接口、
`POST /api/internal/revalidate`。
