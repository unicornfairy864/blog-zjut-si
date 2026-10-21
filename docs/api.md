# 接口清单

> 契约以 [`agent.md`](agent.md) §5 为准；本文件随实现更新，D15 起改由 OpenAPI 生成。
> 所有响应都是 `{code,data,msg}` 信封；时间一律 Unix 秒（整数）。

## 通用约定

- 前缀 `/api`；成功 `HTTP 200` + `{"code":0,"data":<数据>,"msg":"操作成功"}`
- 失败：**HTTP 200** + `{"code":<数字码>,"data":<字段错误\|引用清单\|null>,"msg":"<人话>"}`；
  成功/失败只由 `code` 区分（v1.14 起）。**本文档只按 code 断言，不按 HTTP 状态断言**
- 例外（仍保留真实状态码）：`/healthz`、`/readyz` 200/503；静态直出与本地附件文件流；`redirects` 301/302
  与外链附件 302；公开读接口的 `ETag` 304；路由不存在的 404（`code 1002`）
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
          "origUrl": "/uploads/78/786a8d…/original.jpg",
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
`url` 的选档顺序固定为 `1600` → `2400` → `960` → `480`（缺档时退到更大的档），所以同一份数据每次请求拿到的是同一个地址。
列表、搜索、标签、系列、相关文章返回的都是这个列表项，`prev` / `next` 只在详情里出现。

### `GET /api/posts/{slug}`

已发布文章详情，返回服务端渲染好的 `contentHtml`（不含 Markdown 源码）。
在列表项字段之外，详情还带 `contentHtml`、`seoTitle`、`seoDescription`、`canonicalUrl`、`lang`、
`allowComments`（false 时前端不显示评论表单）、`prev` / `next`（按 `published_at` 相邻，仅 published，边界为 `null`）。

`contentHtml` 里的本站图片会在返回时被包上一层「查看原图」链接（`<a class="media-orig"
href="{img_base_url}/{storage_key}/original.{ext}" target="_blank" rel="noopener noreferrer">`）；
该图的原图被管理端手动删掉（`orig_archived`）时原样返回、不给链接。

同一处还会给正文里的本站图片补 `srcset`（该图的四档变体，不足两档不输出）、`sizes`、
`width`/`height` 与 `loading="lazy" decoding="async"`；`media` 表里查不到的图片原样保留。

| code | 场景 |
| --- | --- |
| 0 | 已发布 |
| 1002 | 不存在 / 草稿 / 定时（不泄露存在性） |
| 1007 | 已归档 / 已删除 |

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

不返回邮箱、`ip_hash`、`ua`。文章不存在 / 草稿 → `200 + code 1002`，归档 / 已删除 → `200 + code 1007`。

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

新评论一律 `pending`，审核通过后才出现在公开列表。限流 **3 次 / 10 分钟 / IP**，超限 `200 + code 1005`；
文章 `allow_comments = 0` 时 `200 + code 1001`。

### `POST /api/posts/{slug}/like` · `DELETE /api/posts/{slug}/like`

点赞 / 取消点赞，两者都**幂等**：重复 POST 不会重复计数，没点过就 DELETE 也返回 200。
限流 **10 次 / 分钟 / visitor**，超限 `200 + code 1005`。

```json
{ "code": 0, "data": { "liked": true, "likeCount": 3 }, "msg": "操作成功" }
```

`liked` 是该 visitor 当前的状态；`likeCount` 每次都按 `likes` 表重算后写回 `posts.like_count`。
文章不存在 → `200 + code 1002`；已归档或已删除 → `200 + code 1007`。

### `GET /api/posts/{slug}/stats`

访客态的阅读数与点赞数，供公开页**客户端异步**渲染（计数不进 SSR HTML）。

```json
{ "code": 0, "data": { "viewCount": 12, "likeCount": 3, "liked": false }, "msg": "操作成功" }
```

响应头是 `Cache-Control: no-store`（`liked` 依赖访客，不参与 ETag 协商）。

### `POST /api/events/view`

PV 打点。请求体：

```json
{ "path": "/posts/hello", "slug": "hello", "referrer": "https://example.com/x" }
```

| 字段 | 规则 |
| --- | --- |
| `path` | 必填，≤200 字符，以 `/` 开头 |
| `slug` | 可空；命中已发布文章时计到该文，否则只计全站行 |
| `referrer` | 可空；只取 host 落库（`page_views.referrer_host`） |

```json
{ "code": 0, "data": { "recorded": true, "viewCount": 13 }, "msg": "操作成功" }
```

- 同 visitor 同文章 **10 分钟内只 +1**：重复打点返回 `recorded: false`，计数不变。
- `viewCount` 仅在 `slug` 命中已发布文章时返回，否则为 `null`。
- 访客标识取请求头 `X-Visitor-Id`（前端 `localStorage` 生成，8–64 位 `[A-Za-z0-9_-]`）；
  缺失或非法时按 `ip|ua` 兜底。公开接口**不 `Set-Cookie`**。

### `GET /api/preview/{token}`

一次性预览链接，看草稿 / 定时 / 归档文章用；响应 `Cache-Control: no-store`，
不参与 ETag，也不进 sitemap / RSS / 搜索。

```json
{ "code": 0, "data": {
    "slug": "hello", "title": "你好", "status": "draft",
    "excerpt": "…", "contentHtml": "<p>…</p>",
    "wordCount": 120, "readingTime": 1,
    "publishedAt": null, "updatedAt": 1760000000, "expiresAt": 1760000000
  }, "msg": "操作成功" }
```

- 首次访问写 `used_at` 并返回内容；**再次访问或超过 24h → `200 + code 2006`**。
- token 不存在 → `200 + code 1002`；token 是 64 位 hex（32 字节随机），库里只存 `token_hash`。

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

标签详情：`tag` + 该标签下的分页文章。标签不存在 → `200 + code 1002`。

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

系列详情；文章按 `series_order` 升序、其次发布时间升序。系列不存在 → `200 + code 1002`。

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
  "data": { "list": [{ "id": 1, "name": "示例友链", "url": "https://example.com", "avatar": "", "description": "" }] },
  "msg": "操作成功"
}
```

### `GET /api/pages`

全部未删除静态页的清单（`sitemap.xml` 用；不出正文）。

```json
{
  "code": 0,
  "data": { "list": [{ "slug": "about", "title": "关于", "updatedAt": 1760000000 }] },
  "msg": "操作成功"
}
```

### `GET /api/pages/{slug}`

静态页（`/about` 等）。不存在 → `200 + code 1002`；软删 → `200 + code 1007`。

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
`q` 长度 1–64 字，超限 `200 + code 1003`。

## 管理组（8641，只经 SSH 隧道）

### 鉴权

| 端点 | 说明 |
| --- | --- |
| `POST /api/admin/auth/login` | 无鉴权、无 CSRF；5 次/分钟；10 分钟 5 次失败锁定 30 分钟 |
| `GET /api/admin/auth/me` | 需要登录；返回 `admin`、`csrfToken`、`expiresAt` |
| `POST /api/admin/auth/logout` | 需要登录 + CSRF；删除会话并清 cookie |

登录失败码（全部 HTTP 200）：`1000` 口令错误、`1003` 参数非法、`1005` 限流、`2005` 已锁定。

### 文章

| 端点 | 说明 |
| --- | --- |
| `GET /api/admin/posts` | 列表；`status` 可筛 `draft/scheduled/published/archived`，`keyword` 匹配标题与 slug |
| `POST /api/admin/posts` | 新建草稿；`slug` 留空由服务端生成 |
| `GET /api/admin/posts/{id}` | 详情（含 `contentMd` 与 `contentHtml`） |
| `PUT /api/admin/posts/{id}` | 覆盖式保存，必须带 `title` 与 `contentMd` |
| `PATCH /api/admin/posts/{id}` | 局部更新；`nil` 字段不改 |
| `DELETE /api/admin/posts/{id}` | 软删（回收站，30 天后物理删除） |
| `POST /api/admin/posts/{id}/publish` | 发布；写版本快照并 `revision+1`。已发布文章内容有变化时再发布 = 为改动记新版本（保留原 `published_at`）；内容没变 → `200 + code 1004` |
| `POST /api/admin/posts/{id}/unpublish` | 取消发布 / 取消归档，回到草稿 |
| `GET /api/admin/posts/{id}/revisions` | 版本列表，最多 50 条 |
| `POST /api/admin/posts/preview` | 渲染未保存正文，不落库；与发布结果同源 |

写入字段：`title`、`slug`、`excerpt`、`contentMd`、`seoTitle`、`seoDescription`、`canonicalUrl`、
`coverMediaId`、`ogMediaId`、`seriesId`、`seriesOrder`、`isPinned`、`allowComments`、`lang`、`tagIds`；
`PATCH` 另支持 `status`（仅 `archived` 归档、`draft` 取消发布）与 `createRedirect`（改 slug 时写 301）。

分类字段的口径（D20 补齐，agent.md §5「内容管理口径」）：

- `tagIds` 是**整数数组**，语义是**替换**：传 `[1,2]` 就把该文标签整体改成这两个；传 `[]` 清空；不传字段 = 不改。
  服务端自动去重、忽略 `≤0`，最多 20 个（超限 → `200 + code 1003`）。**id 必须真实存在**，
  否则 `200 + code 1003`，`data` 为 `[{field:"tagIds",message:…}]`。
- `seriesId` 传 `0` 表示清空（置 NULL）；指向不存在的系列 → `200 + code 1003`（`data` 带 `seriesId`）。
  `seriesOrder` 是系列内排序，`≥0`。
- 详情（`GET /posts/{id}`）回显 `tagIds`（`[]` 表示没有标签）与 `cover` / `ogImage`
  （`{id,url,alt,width,height}`，编辑器显示已选封面用；外键指向的图已被删除时为 `null`）；
  列表项回显 `tags[{name,slug}]` 与 `seriesName`。

```json
// PATCH /api/admin/posts/3  →  只提交要改的项
{ "tagIds": [1, 4], "seriesId": 2, "seriesOrder": 1, "isPinned": true, "allowComments": false }
```

文章错误码：

| code | 场景 |
| --- | --- |
| 1002 | 文章不存在 |
| 1003 | 参数校验失败（`data` 为字段错误数组） |
| 1004 | 非法状态迁移（如重复发布、草稿归档） |
| 1005 | 公开接口限流 |
| 1007 | 公开端访问已归档/已删除文章 |
| 2001 | slug 冲突 |
| 2004 | 已发布文章改 slug 未写跳转 |

### 预览

```json
// POST /api/admin/posts/preview  →  data
{ "html": "<h2 id=\"h-1a2b3c4d\">标题</h2>", "text": "标题", "wordCount": 2, "readingTime": 1, "excerpt": "标题" }
```

### 定时发布 / 版本回滚 / 预览链接

| 端点 | 说明 |
| --- | --- |
| `POST /api/admin/posts/{id}/schedule` | `{"publishedAt": 1760000000, "note": "定时发布"}`；`publishedAt` 必须是**未来**时间（否则 `200 + code 1003`，`data` 带 `field=publishedAt`），落 `scheduled` 并投递 `publish_scheduled` 任务。只有 `draft` 能定时：`published` / `archived` → `200 + code 1004`（要重新定时先 `unpublish`） |
| `POST /api/admin/posts/{id}/unpublish` | 退回草稿，同时撤掉该文待执行的 `publish_scheduled` 任务 |
| `POST /api/admin/posts/{id}/revisions/{rid}/restore` | `{"note": "回滚"}` 可选；用旧版本覆盖正文并**再写一条新 revision**（`revision+1`），历史不被改写 |
| `GET /api/admin/posts/{id}/revisions` | `data: {"list":[…]}`，最多 50 条、按 `revisionNo` 倒序，**不含正文** |
| `POST /api/admin/posts/{id}/preview-link` | 生成一次性预览链接 → `{"token":"…","url":"/preview/…","expiresAt":1760000000}`；**同一篇文章旧的未使用链接会被作废** |
| `DELETE /api/admin/preview-links/{token}` | 立即作废该链接 → `{"deleted":true}`；不存在 → `200 + code 1002` |

```json
// POST /api/admin/posts/{id}/schedule  →  data（与 GET /posts/{id} 同形状）
{ "id": 3, "slug": "hello", "title": "你好", "status": "scheduled",
  "publishedAt": 1760000000, "revision": 2, … }
```

定时发布到点由 `publish_scheduled` 任务完成（调度器每 60s 扫一次，误差 <90s）；
任务执行时会再确认一次「仍是 `scheduled` 且 `published_at <= now`」，用户中途取消就不会误发。

```json
// POST /api/admin/posts/{id}/preview-link  →  data
{ "token": "9f2c…（32 字节 hex，明文只出现这一次）", "url": "/preview/9f2c…", "expiresAt": 1760000000 }
```

库里只存 `token_hash`；公开端 `GET /api/preview/{token}` 首次访问写 `used_at` 并返回内容，
之后或超过 24h → `200 + code 2006`，token 不存在 → `200 + code 1002`。

隧道里（`http://localhost:8641`）点开 `/preview/…` 也能直接看：8641 边缘同样把 `/preview*`
转给 Next，不必把域名换成公网站点。

### 任务与审计

| 端点 | 说明 |
| --- | --- |
| `GET /api/admin/jobs?status=&page=&pageSize=` | 任务队列分页，按 `run_at` 倒序；`status` 空表示全部 |
| `POST /api/admin/jobs` | 手动投递：`{"type":"cleanup"}`，只接受 `cleanup` / `backup` / `link_check` / `integrity_check`，立即入队 |
| `POST /api/admin/jobs/{id}/retry` | 把 `failed` 任务放回 `pending` 并立刻到期；非 `failed` → `200 + code 1004` |
| `GET /api/admin/audit-logs?action=&targetType=&targetId=&page=&pageSize=` | 审计日志分页，按创建时间倒序；`adminId` / `ipHash` 只在这里出现 |
| `POST /api/admin/notify/test` | 按**已保存**的告警配置立刻投递一条测试告警；不看 `enabled`、不受 24h 静默期影响。通道没配齐 → `200 + code 1003`；投递失败 → `200 + code 1006`（`msg` 只说"超时 / HTTP 4xx / 连不上"，细节在容器日志）。成功写审计 `notify.test` |

```json
// GET /api/admin/jobs  →  data.list[]
{ "id": 12, "type": "cleanup", "payload": "{}", "runAt": 1760000000, "status": "done",
  "attempts": 1, "maxAttempts": 3, "lastError": "", "lockedAt": null, "createdAt": 1760000000 }
```

```json
// GET /api/admin/audit-logs  →  data.list[]
{ "id": 88, "adminId": 1, "action": "post.publish", "targetType": "post", "targetId": "3",
  "meta": { "revision": 2 }, "ipHash": "…", "createdAt": 1760000000 }
```

### 分类与友链（管理端）

| 端点 | 说明 |
| --- | --- |
| `GET /api/admin/tags` · `GET /api/admin/series` | 全量列表（不分页），带 `count`（未删除文章数，含草稿） |
| `POST /api/admin/tags` · `POST /api/admin/series` | `{"name":"Go","slug":"go","description":""}`；`slug` 留空时由 ASCII 名生成，中文名必须手填 |
| `PUT /api/admin/tags/{id}` · `PUT /api/admin/series/{id}` | 同上字段，均可选 |
| `DELETE /api/admin/tags/{id}` · `DELETE /api/admin/series/{id}` | 连带清 `post_tags` / 把 `posts.series_id` 置 NULL，**不删文章** |
| `GET /api/admin/links` | 全量（含 `hidden`），按 `sort`、`id` 升序 |
| `POST /api/admin/links` | `{"name":"…","url":"https://…","avatar":"","description":"","sort":1,"status":"active"}` |
| `PUT /api/admin/links/{id}` · `DELETE /api/admin/links/{id}` | 同上 |

```json
// GET /api/admin/tags  →  data
{ "list": [ { "id": 1, "name": "Go", "slug": "go", "description": "", "count": 3, "createdAt": 1760000000 } ] }
```

冲突与校验：`slug` 重复 → `200 + code 2001`；`slug` 非法或中文名留空 → `200 + code 1003`（`data` 带 `field`）；
`name` 重复 → `200 + code 1004`。

### 跳转与附件

| 端点 | 说明 |
| --- | --- |
| `GET /api/admin/redirects` | 全量，按 `id` 倒序 |
| `POST /api/admin/redirects` | `{"fromPath":"/posts/old","toPath":"/posts/new","status":301}`；`fromPath` 唯一、以 `/` 开头且 ≤200；`toPath` 以 `/` 开头或 `http(s)://` 开头、≤500 |
| `PUT /api/admin/redirects/{id}` · `DELETE /api/admin/redirects/{id}` | 同上 |
| `GET /api/admin/attachments?postId=&page=&pageSize=` | 附件分页，按 `sort`、`id` 升序 |
| `POST /api/admin/attachments` | `{"postId":3,"title":"源码","shareUrl":"https://…","fileName":"src.zip","bytes":1024,"sha256":"","version":"v1","sort":0}` |
| `PUT /api/admin/attachments/{id}` · `DELETE /api/admin/attachments/{id}` | 同上字段，均可选。外链附件（没有 `storage_key`）把 `shareUrl` 清空 → `200 + code 1003`（`data` 带 `field=shareUrl`）：不允许留下"既没有外链地址也没有本地文件"的空壳；本地附件不受影响 |
| `GET /api/attachments/{id}/go` | **公开**：`302` 跳 `share_url` 并 `clicks+1`；不存在 → `200 + code 1002`；响应 `no-store` |

```json
// GET /api/admin/attachments  →  data.list[]
{ "id": 5, "postId": 3, "title": "源码", "shareUrl": "", "fileName": "src.zip",
  "bytes": 1048576, "sha256": "3f9a…", "version": "v1", "sort": 0, "clicks": 12,
  "storageKey": "3f/3f9a…zip", "mime": "application/zip", "createdAt": 1760000000 }
```

`storageKey` 非空表示**本地上传**（物理文件 `{file.dir}/{storageKey}`），为空表示**外链**（按 `shareUrl` 302）。
`POST /api/admin/attachments`（JSON）只用于外链，`shareUrl` 留空 → `200 + code 1003`；本地文件走下面的上传端点。

### 附件上传与下载

| 端点 | 说明 |
| --- | --- |
| `POST /api/admin/attachments/upload` | `multipart/form-data`：`file`（必填）、`postId`（必填）、`title`（可选，默认取去掉扩展名的文件名）、`version`、`sort`；按内容 sha256 去重 |
| `GET /api/attachments/{id}/go` | 公开下载出口：本地的直接下发，外链的 `302` 到 `shareUrl`；两种情况都 `clicks+1`、`no-store` |

上传失败：

| code | 场景 |
| --- | --- |
| 1003 | 缺 `file` / `postId` 非法 / `sort` 非法 / `title` 超长 |
| 2008 | 超过 `file.max_bytes`（默认 20MB） |
| 2007 | 扩展名或文件名命中安全黑名单（`php`/`jsp`/`exe`/`html`/`svg`/`js`… 与隐藏文件） |

本地下发的响应头固定为 `Content-Type: application/octet-stream`、
`Content-Disposition: attachment; filename="…"; filename*=UTF-8''…`、`X-Content-Type-Options: nosniff`，
并支持 `Range`（断点续传）；文件丢失 → `200 + code 1007`，记录不存在 → `200 + code 1002`。

### 存储管理

| 端点 | 说明 |
| --- | --- |
| `GET /api/admin/storage` | 磁盘占用统计 + 孤儿文件（磁盘有、库里无）+ 缺失文件（库里有、磁盘无） |
| `POST /api/admin/storage/cleanup` | 删除孤儿文件，返回 `{removedFiles,removedBytes,failedFiles}` |
| `DELETE /api/admin/media/{id}/original` | 只删原图、保留四档变体，置 `origArchived=1`（此后不能 `retry`） |

```json
// GET /api/admin/storage  →  data
{
  "uploadDir": "/data/blog/uploads", "fileDir": "/data/blog/files",
  "mediaCount": 12, "originalBytes": 10485760, "variantBytes": 3145728, "originalsRemoved": 2,
  "attachmentCount": 5, "attachmentBytes": 2097152, "linkedAttachmentCount": 4, "totalBytes": 15728640,
  "orphanCount": 3, "orphanBytes": 81920, "missingCount": 1,
  "orphans": [ { "path": "ab/abcd…/960.webp", "bytes": 40960, "kind": "variant" } ],
  "missing": [ { "kind": "original", "storageKey": "ab/abcd…", "mediaId": 3 } ]
}
```

`kind` ∈ `original` / `variant` / `attachment`；清单最多 200（孤儿）/ 100（缺失）条，计数是全量。
清理只删孤儿，**不碰**缺失文件与任何在用的原图、变体、附件。

公开边缘还会按 `redirects.from_path` 精确匹配：命中就 `301/302` 到 `to_path` 并 `hits+1`，
不落 Next（所以改 slug 后老链接不会 404）。

### 设置

| 端点 | 说明 |
| --- | --- |
| `GET /api/admin/settings` | 当前可写项 + 只读回显（**不含任何口令与告警密钥**） |
| `PUT /api/admin/settings` | `{"imgBaseUrl":"","siteTimezone":"Asia/Shanghai","notify":{…}}`；写内容类字段会 `content_version+1`，**只改 `notify` 不会** |

```json
// GET /api/admin/settings  →  data
{
  "imgBaseUrl": "", "siteTimezone": "Asia/Shanghai", "contentVersion": 7,
  "upload": { "dir": "/data/blog/uploads", "maxBytes": 31457280 },
  "file": { "dir": "/data/blog/files", "maxBytes": 20971520 },
  "task": { "enabled": true, "intervalSeconds": 60 },
  "db": { "path": "/data/blog/db/blog.db" },
  "notify": {
    "enabled": true, "channel": "webhook",
    "webhookUrlSet": true, "telegramBotTokenSet": false, "telegramChatId": "",
    "events": { "jobFailed": true, "diskHigh": true, "certExpiring": true },
    "diskThresholdPercent": 85, "certExpireDays": 14,
    "certFile": "/etc/letsencrypt/live/blog.zjut.si/fullchain.pem",
    "lastAlerts": [ { "event": "job_failed", "at": 1760000000, "ok": true } ]
  }
}
```

```json
// PUT /api/admin/settings  →  只提交要改的项
{ "notify": { "enabled": true, "channel": "webhook",
              "webhookUrl": "https://example.com/hook",
              "diskThresholdPercent": 85, "certExpireDays": 14,
              "events": { "jobFailed": true, "diskHigh": true, "certExpiring": true } } }
```

- `siteTimezone` 必须是 `time.LoadLocation` 认得的 IANA 名（如 `Asia/Shanghai`），否则 `200 + code 1003`。
- `imgBaseUrl` 留空表示用相对路径 `/uploads/…`（由 nginx 直出）；填了必须是 `http(s)://` 且不带结尾斜杠。
- `notify` 的密钥（`webhookUrl`、`telegramBotToken`）**从不回显**：`null` = 保持不变，`""` = 清空，非空 = 覆盖；
  字段级错误（渠道非法、启用却没填必填项、阈值越界）→ `200 + code 1003`，`data` 是 `[{field,message}]`。
- `visitor_salt` 与告警密钥属于内部密钥，**任何接口都不返回**（§9）。

### 媒体

| 端点 | 说明 |
| --- | --- |
| `POST /api/admin/media` | `multipart/form-data`：`alt`（可选，必须排在 `file` 前）+ `file`；≤30MB、≤5000 万像素；内容 sha256 去重 |
| `GET /api/admin/media` | 媒体库分页，按创建时间倒序 |
| `DELETE /api/admin/media/{id}` | 未被引用才删除；被引用返回 `200 + code 2002` 与引用清单 |
| `POST /api/admin/media/{id}/retry` | 从本地原图重新生成变体；原图已被删（`origArchived`）时 `200 + code 1002` |
| `DELETE /api/admin/media/{id}/original` | 只删原图、保留四档变体（见「存储管理」） |

上传成功（`data`）：

```json
{
  "id": 1, "sha256": "786a8d…", "originalName": "photo.jpg", "mime": "image/jpeg", "ext": "jpg",
  "bytes": 177841, "width": 2400, "height": 1200, "pixels": 2880000,
  "alt": "示例图", "blurhash": "L6Pj0^…", "status": "ready", "error": "",
  "url": "/uploads/78/786a8d…/1600.webp",
  "origUrl": "/uploads/78/786a8d…/original.jpg",
  "origArchived": false, "origArchivedAt": null,
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

`origUrl` 是同源原图直链（`{img_base_url}/{storage_key}/original.{ext}`，原图永久保留）；
管理端删掉原图后 `origArchived=true`、`origUrl` 为空串，`retry` 也会失败（原图是重新转码的唯一来源）。

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

### 统计

| 端点 | 说明 |
| --- | --- |
| `GET /api/admin/stats/overview` | 首页看板：状态计数 + 今日/累计 PV·UV + 点赞 + Top 5 文章 |
| `GET /api/admin/stats/daily?from=2026-09-20&to=2026-10-19` | 全站日粒度（`stats_daily.post_id=0`），缺省最近 30 天，范围 ≤366 天 |

```json
// GET /api/admin/stats/overview  →  data
{
  "posts": { "published": 3, "draft": 2, "scheduled": 0, "archived": 1, "deleted": 0 },
  "comments": { "pending": 1, "approved": 4, "spam": 0 },
  "media": { "ready": 5, "processing": 0, "failed": 0 },
  "views": { "total": 120, "today": 8, "uvToday": 5 },
  "likes": { "total": 9, "today": 2 },
  "topPosts": [
    { "id": 1, "slug": "hello", "title": "你好", "viewCount": 40, "likeCount": 3 }
  ]
}
```

```json
// GET /api/admin/stats/daily?from=2026-10-01&to=2026-10-19  →  data
{
  "from": "2026-10-01", "to": "2026-10-19", "totalPv": 120, "totalUv": 60,
  "list": [ { "date": "2026-10-19", "pv": 8, "uv": 5 } ]
}
```

日期都是 `Asia/Shanghai` 的 `YYYY-MM-DD`；没有数据的日期不补零行（前端按区间补空点）。

## 内部组（8641，仅容器内或隧道）

| 端点 | 说明 |
| --- | --- |
| `GET /healthz` | 进程存活，`{"status":"ok","time":…}` |
| `GET /readyz` | DB / 磁盘 <85% / uploads 可写 / 队列 <100；任一失败 **503**（§5 例外清单） |
| `GET /api/internal/health` | 走统一信封的内部健康检查 |

## Next 自身端点（不走 Go，D15）

这些由 Next 的 route handler 每请求渲染（构建期不取数），内容来自上面这些公开 API：

| 端点 | 说明 |
| --- | --- |
| `GET /sitemap.xml` | 只含已发布文章、未删除静态页与非空标签/系列；`Cache-Control: public, max-age=600` |
| `GET /rss.xml` | RSS 2.0，最近 20 篇已发布文章，`<content:encoded>` 全文 |
| `GET /robots.txt` | 禁 `/admin`、`/preview`、`/api/`，带 `Sitemap:` |

## 错误码

见 [`agent.md`](agent.md) §5。代码中只能引用 `model/common/errs` 的常量，禁止裸数字。

## 尚未实现（后续模块）

管理端静态页写接口 `GET|PUT /api/admin/pages/{slug}`，以及 `POST /api/internal/revalidate`（Next 主动失效）。
两者在 §5 的接口清单里出现过，但路由未注册，**别当契约用**（agent.md §5 有同样的标注）。
