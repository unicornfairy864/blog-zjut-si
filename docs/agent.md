# AGENTS.md

> 项目：`zjut.si` 个人博客（Go + Next.js + SQLite 单容器）
> 路径：`docs/agent.md`（仓库根若需要 `AGENTS.md`，放一个只含链接的 3 行索引，不要复制正文；Windows 下不要用软链）
> 版本：**v1.15**（2026-10-21 D15 SEO：sitemap / RSS / JSON-LD / OG，见 §11 与 §22）
> 状态：**设计契约**。任何偏离本文档的实现都必须先更新本文档
> 读者：执行编码的 AI Agent 与项目所有者（验收方）
> 阅读顺序：§1 硬红线 → §2 拓扑 → §3 结构 → §4 数据模型 → §5 接口 → §7/§8 规范 → §16 验收

---

## 0. 一句话定义

一台 3C/4G 海外 VPS 上的**单人技术博客**：Next.js 负责所有页面渲染，Go 负责全部业务逻辑与 API，SQLite 存数据，浏览器与宿主 nginx 承担全部缓存职责，管理端经 SSH 隧道访问、永不上公网。

---

## 1. 不可协商的硬约束

违反以下任一条即视为**实现错误**，不接受"更好"的替代方案：

| # | 约束 | 说明 |
| --- | --- | --- |
| C1 | **单容器**：Next + Go 两个进程，由 s6-overlay 托管 | 不拆分服务，不引入 Caddy、Redis、Nginx（容器内） |
| C2 | **数据库只有 SQLite** | 禁止 MySQL/Postgres/Redis；禁止任何内存数据库 |
| C3 | **宿主端口只绑 `127.0.0.1`** | compose 只发布 `127.0.0.1:8640` 与 `127.0.0.1:8641`；**容器内进程监听 `:8640`/`:8641`（即 `0.0.0.0`）**，否则 docker 端口映射不可达。宿主上禁止 `0.0.0.0`，禁用 `network_mode: host` |
| C4 | **管理端永不出现在 8640** | `8640` 上访问 `/admin/*`、`/api/admin/*` 必须返回 404 |
| C5 | **8641 永不上公网** | 仅 SSH 隧道可达，浏览器地址为 `http://localhost:8641` |
| C6 | **不引入缓存层** | 无 Redis、无 ISR、无 proxy_cache、无 CDN。只允许浏览器缓存头与 nginx `open_file_cache` |
| C7 | **不做用户系统** | 无注册、无登录用户、无个人中心；只有单一 `admins` 记录 |
| C8 | **不做 TOTP / 2FA** | 仅用户名 + 密码 |
| C9 | **公开页必须 SSR，管理页必须 CSR** | 见 §8 渲染契约 |
| C10 | **图片与附件都不入库、不上 CDN / 对象存储** | 磁盘存文件（`upload.dir` / `file.dir`），DB 只存 key 与元数据 |

---

## 2. 系统拓扑

```
公网 :80 ──▶ 宿主 nginx ──▶ 127.0.0.1:8640 ──▶ Go 公开边缘
                                                 ├ /api/*        → Go 本地
                                                 ├ /admin/*      → code 1002（HTTP 200，不泄露存在性）
                                                 ├ /uploads/*    → code 1002（HTTP 200；公网由 nginx 直出）
                                                 └ 其余          → Next :3000

本地 :8641 ─▶ ssh -L 8641:127.0.0.1:8641 ─▶ Go 管理边缘
                                              ├ /api/admin/*   → Go 本地
                                              ├ /api/internal/*→ Go 本地
                                              ├ /healthz|/readyz→ Go 本地
                                              ├ /admin*        → Next :3000（含 /admin/login）
                                              ├ /preview*      → Next :3000（隧道里也能直接开预览链接）
                                              ├ /_next/*       → Next :3000（+ /favicon.ico）
                                              └ 其余           → code 1002（HTTP 200）
```

管理端 8641 额外直出 `/uploads/*`（仅本地媒体库与编辑器预览用）；8640 上 `/uploads` 不落 Go（直接命中 nginx 直出）。
**连接建立了就一定是 HTTP 200**：路由不存在、未登录、CSRF 失败、限流、锁定、Next 挂掉（`code 1006`）都是 200 + `code`。

| 组件 | 职责 | 明确不做 |
| --- | --- | --- |
| 宿主 nginx | TLS/80 入口、静态图片直出、限流、防盗链 | 不代理缓存 HTML，不做鉴权 |
| Go 进程 | 双 listener 边缘路由、全部 API、静态文件校验、任务调度 | 不渲染 HTML 页面 |
| Next 进程 | 全部页面渲染（公开 SSR + 管理 CSR + 预览页） | 不直连数据库、不写业务规则 |
| SQLite | 唯一持久化存储 | 不存图片二进制 |
| 本地磁盘 | 图片与附件的唯一副本（`/data/blog/uploads`、`/data/blog/files`） | 不接外部图床/对象存储 |

**附件不经任何静态直出**：图片走宿主 nginx 的 `/uploads/`，附件只从 `GET /api/attachments/{id}/go` 出口（见 §10）。

**端口 3065 属于既有服务，任何代码、配置、文档都不得改动它。**
**健康检查端点只注册在 8641**，8640 上不暴露磁盘、队列、DB 状态。

---

## 3. 仓库结构与模块边界

### 3.1 顶层

```
/
├─ apps/
│  ├─ server/                 # Go：gin-vue-admin 骨架
│  │  ├─ main.go              # 仅 initialize → run
│  │  ├─ go.mod
│  │  ├─ config.yaml  config/ # viper + yaml，敏感项由环境变量覆盖
│  │  ├─ global/              # 仅 db.go / config.go / log.go
│  │  ├─ core/                # log db edge storage render
│  │  ├─ api/v1/              # public/ admin/ internal/ + enter.go
│  │  ├─ service/             # 业务逻辑（唯一写业务的地方）
│  │  ├─ model/               # 实体 + DTO + 数据访问 + common/{response,request,errs}
│  │  ├─ router/ middleware/ initialize/ task/ utils/
│  │  ├─ migrations/ resource/ testdata/
│  │  └─ ...
│  └─ web/                    # Next.js：dub 组织
│     ├─ app/(public)/        # 公开页
│     ├─ app/(admin)/admin/   # 管理页（全 client）：(protected)/ 带会话守卫，login/ 不带
│     ├─ app/(preview)/preview/[token]/
│     ├─ app/robots.ts        # /robots.txt（禁 /admin、/preview、/api）
│     ├─ app/sitemap.xml/route.ts   # /sitemap.xml（每请求取数，只含已发布）
│     ├─ app/rss.xml/route.ts       # /rss.xml（RSS 2.0 全文）
│     ├─ ui/{shadcn,layout,post,admin,shared,seo}
│     ├─ lib/{api,hooks,zod,constants,swr,seo.ts}
│     ├─ styles/ public/ tests/ e2e/
│     ├─ next.config.ts  tailwind.config.ts  tsconfig.json  components.json
│     ├─ middleware.ts       # 每请求 nonce 的 CSP（§9）
│     ├─ vitest.config.ts
│     └─ package.json
├─ deploy/
│  ├─ Dockerfile             # 多阶段：web build → go build → runtime
│  ├─ s6/                    # 进程托管脚本
│  ├─ nginx/blog.conf        # 宿主 nginx 片段（人工部署）
│  └─ compose.yaml
├─ docs/
│  ├─ agent.md               # 本文档（唯一契约）
│  ├─ runbook.md             # 部署 / 备份 / 恢复 / 排查
│  ├─ api.md                 # 接口清单（由 OpenAPI 生成）
│  ├─ standards/{go,web}.md  # 编码规范细则
│  ├─ adr/                   # 架构决策记录
│  └─ review-draft.md        # v1.1 评审记录（归档用）
├─ .github/workflows/        # CI
├─ Makefile                  # dev / check / build / migrate
├─ .dockerignore
└─ .gitignore
```

### 3.2 Go 分层规则（强约束）

1. 依赖方向 `api/v1 → service → model`；`initialize` 可依赖全部；`core/*`、`utils` 不得反向依赖业务包。
2. `api/v1/*` 只做：绑定 → `binding` 校验 → 调 service → `response.*` 输出。禁止 SQL、事务、状态机判断。
3. `service/*` 写全部业务规则；事务由 service 发起（`global.DB.Transaction`），tx 以参数透传；**不接收 `*gin.Context`**。
4. `model/*` 是唯一出现 `gorm` 与 SQL 的地方；只做数据访问，不做权限与状态机判断。
5. `api` 不得直接调用 `model` 的查询函数。
6. `global` 只有 `DB`、`Config`、`Log` 三个变量；新增全局必须先改本文档。
7. `router/*` 只注册路由与中间件链，三个 engine（public / admin / internal）各自成文件。
8. 每个业务包一个 `enter.go` 聚合（`ServiceGroup` / `ApiGroup`），跨包只走聚合入口。
9. Next 不得直接访问 SQLite；所有数据必须经 Go API。
10. `apps/web/lib/api/` 之外，禁止任何组件直接 `fetch`。

---

## 4. 数据模型契约（23 张表）

**全局约定**

| 项 | 规则 |
| --- | --- |
| 时间 | `INTEGER`，Unix 秒，UTC；展示层转 `Asia/Shanghai` |
| 日粒度 | `stats_daily.date` 是 **`Asia/Shanghai` 的 `YYYY-MM-DD`**（不是 UTC 日期） |
| 布尔 | `INTEGER` 0/1 |
| JSON | `TEXT` 存合法 JSON 字符串 |
| 主键 | `INTEGER PRIMARY KEY`（rowid 别名），`sessions`/`posts_fts` 等例外见行内 |
| 文本 | UTF-8；`slug` 仅 `[a-z0-9-]`，长度 1–120 |
| 软删 | `deleted_at IS NULL` 为有效；回收站 30 天后物理删除 |
| 图片 | 绝不写入 BLOB 字段 |

| 表 | 字段（类型） | 索引 / 约束 |
| --- | --- | --- |
| `admins` | id, username TEXT, password_hash TEXT, email TEXT, last_login_at INT, created_at INT, updated_at INT | UNIQUE(username) |
| `sessions` | token_hash TEXT PK, admin_id INT, csrf_hash TEXT, ip_hash TEXT, ua TEXT, created_at INT, last_seen_at INT, expires_at INT | INDEX(admin_id), INDEX(expires_at) |
| `login_attempts` | id, key TEXT, kind TEXT, count INT, window_start INT, locked_until INT | UNIQUE(key,kind) |
| `posts` | id, slug, title, excerpt, content_md, content_html, content_text, cover_media_id, status, published_at, created_at, updated_at, deleted_at, word_count, reading_time, is_pinned, allow_comments, series_id, series_order, seo_title, seo_description, canonical_url, og_media_id, view_count, like_count, comment_count, revision, lang | UNIQUE(slug); INDEX(status,published_at DESC) WHERE deleted_at IS NULL; INDEX(series_id,series_order) |
| `post_revisions` | id, post_id, revision_no, title, content_md, content_html, note, created_at | UNIQUE(post_id,revision_no)；仅保留最近 50 条 |
| `tags` | id, name, slug, description, created_at | UNIQUE(name), UNIQUE(slug) |
| `post_tags` | post_id, tag_id | PK(post_id,tag_id); INDEX(tag_id) |
| `series` | id, name, slug, description, created_at | UNIQUE(slug) |
| `preview_tokens` | token_hash PK, post_id, expires_at, used_at INT NULL, created_at | 24h 过期；`used_at` 非空即失效 |
| `redirects` | id, from_path, to_path, status INT, hits INT, created_at | UNIQUE(from_path); CHECK(status IN (301,302)) |
| `media` | id, sha256, storage_driver, storage_key, original_name, mime, ext, bytes, width, height, pixels, variants_json, blurhash, alt, status, error, orig_archived, orig_archived_at INT NULL, created_at, updated_at | UNIQUE(sha256); INDEX(status)；`orig_archived=1` 表示本地原图已被管理端手动删除（变体仍在），原图不再自动清理 |
| `post_attachments` | id, post_id, title, share_url, file_name, bytes, sha256, version, sort, clicks, storage_key, mime, created_at | INDEX(post_id); INDEX(storage_key)；`storage_key` 非空 = 本地上传（物理文件 `{file.dir}/{storage_key}`），空 = 外链（按 `share_url` 302） |
| `comments` | id, post_id, parent_id, root_id, author_name, author_email, email_hash, author_url, content_md, content_html, status, ip_hash, ua, created_at, updated_at | INDEX(post_id,status,created_at); INDEX(status,created_at) |
| `likes` | post_id, visitor_hash, created_at | PK(post_id,visitor_hash) |
| `page_views` | id, post_id NULL, path, visitor_hash, referrer_host, created_at | INDEX(post_id,created_at)；明细保留 90 天 |
| `stats_daily` | date TEXT, post_id INT, pv INT, uv INT | PK(date,post_id)；date 为上海日期；`post_id=0` 表示全站聚合行（PK 不允许 NULL） |
| `jobs` | id, type, payload, run_at, status, attempts, max_attempts, last_error, locked_at, created_at | INDEX(status,run_at) |
| `settings` | key TEXT PK, value_json, updated_at | 必含 `content_version`、`img_base_url`、`site_timezone`、`visitor_salt`；`notify`（告警配置）与 `notify_state`（告警去重状态）由 D17 起使用 |
| `links` | id, name, url, avatar, description, sort, status, created_at | — |
| `audit_logs` | id, admin_id, action, target_type, target_id, meta_json, ip_hash, created_at | INDEX(created_at), INDEX(target_type,target_id) |
| `pages` | id, slug, title, content_md, content_html, content_text, seo_title, seo_description, created_at, updated_at, deleted_at | UNIQUE(slug)；承载 `/about` 等静态页 |
| `link_checks` | id, post_id, url, kind, status_code, ok, error, checked_at | INDEX(post_id); INDEX(checked_at)；`kind`: link\|image |
| `posts_fts` | FTS5(title, excerpt, content_text) | `content='posts'`, `tokenize='trigram'`，配 ai/ad/au 三触发器 |

**枚举（必须用常量，禁止魔法字符串）**

```
posts.status    : draft | scheduled | published | archived
media.status    : processing | ready | failed      # uploading 是内存态，不落库
comments.status : pending | approved | spam | deleted
links.status    : active | hidden                  # 默认 active（migrations/000001）
jobs.status     : pending | running | done | failed
jobs.type       : publish_scheduled | media_process | link_check | backup | cleanup
storage_driver  : local                             # 只有本地磁盘，不接对象存储
```

---

## 5. 接口契约

**通用（gin-vue-admin 信封）**

| 项 | 规则 |
| --- | --- |
| 前缀 | `/api`（不引入 `/api/v1` 前缀，URL 保持简洁） |
| 成功 | HTTP 200 + `{"code":0,"data":<数据>,"msg":"操作成功"}` |
| 失败 | **HTTP 200** + `{"code":<数字码>,"data":<details\|null>,"msg":"<人话>"}`；成功/失败只由 `code` 区分（v1.14 起，gin-vue-admin 风格）。`data` 语义不变：无附加数据为 `null`，字段错误为 `[{field,message}]` |
| 分页入参 | `page`(≥1) `pageSize`(1–50, 默认 20) `keyword`(可选) |
| 分页出参 | `data:{"list":[],"total":0,"page":1,"pageSize":20}` |
| 时间 | 全部出参为 Unix 秒整数 |
| 缓存头 | 公开读接口必须带强 `ETag`（响应体哈希），支持 `If-None-Match → 304` |
| CSRF | 所有 `/api/admin/*` 写操作必须校验 `X-CSRF-Token`；token 由 `GET /auth/me` 下发，值由会话令牌哈希派生（ADR 0003），会话期内稳定 |
| 访客标识 | 公开写接口（点赞、PV 打点）读请求头 `X-Visitor-Id`：前端在 `localStorage` 生成一次的随机串，8–64 位 `[A-Za-z0-9_-]`；缺失或非法时服务端按 `ip\|ua` 兜底。**公开 HTML 与公开读接口一律不 `Set-Cookie`**（§17.1） |
| 访客态读 | `liked` 这类依赖访客的读接口（`GET /posts/{slug}/stats`）用 `Cache-Control: no-store`，不参与 ETag；计数不进 SSR HTML（§17.1） |

**保留真实 HTTP 状态码的例外清单**（只有这些不是 200；判定标准是"连接建立后 HTTP 状态本身承载协议语义"，而不是应用状态）：

| # | 场景 | 真实状态码 |
| --- | --- | --- |
| 1 | `GET /healthz`、`GET /readyz`（容器健康检查依赖 `curl -f`，503 才能让容器被标记 unhealthy） | 200 / 503 |
| 2 | 静态文件直出：8641 的 `/uploads/*`（`edge.ServeUploads`，缺文件 404；它没有 `code` 信封） | 200 / 404 |
| 3 | 文件流：公开 `GET /api/attachments/{id}/go` 命中本地文件时用 `http.ServeFile`（Range 需要 206） | 200 / 206 |
| 4 | 跳转：公开边缘按 `redirects.from_path` 的 301/302、外链附件的 `c.Redirect(302)` | 301 / 302 |
| 5 | 公开读接口的 ETag 协商命中 `If-None-Match`（`response.OkWithETag`） | 304 |

其余一切（含**路由不存在**、未登录、CSRF 失败、限流、锁定、Next 转发失败）都是 **HTTP 200 + `code`**：路由不存在是 `code 1002`，Next 转发失败是 `code 1006`。

**错误码（数字码 + 常量名，禁止裸数字）**

| code | 常量 | 语义 |
| --- | --- | --- |
| 0 | `SUCCESS` | 成功 |
| 7 | `ERROR` | 未分类失败 |
| 1000 | `ERROR_AUTH` | 未登录 / 会话失效 |
| 1001 | `ERROR_FORBIDDEN` | 越权 / CSRF 失败 |
| 1002 | `ERROR_NOT_FOUND` | 资源不存在 |
| 1003 | `ERROR_VALIDATION` | 参数校验失败，`data` 带字段错误数组 |
| 1004 | `ERROR_CONFLICT` | 通用冲突 |
| 1005 | `ERROR_RATE_LIMITED` | 限流 |
| 1006 | `ERROR_INTERNAL` | 内部错误（不暴露堆栈） |
| 1007 | `ERROR_GONE` | 已归档或已删除，资源不再提供 |
| 2001 | `ERROR_SLUG_CONFLICT` | slug 重复 |
| 2002 | `ERROR_MEDIA_IN_USE` | 媒体被引用，`data` 带引用清单 |
| 2003 | `ERROR_IMAGE_TOO_LARGE` | 像素或字节超限 |
| 2004 | `ERROR_SLUG_IMMUTABLE` | 已发布文章改 slug 未写 redirect |
| 2005 | `ERROR_LOGIN_LOCKED` | 登录锁定 30 分钟 |
| 2006 | `ERROR_PREVIEW_EXPIRED` | 预览链接过期或已用 |
| 2007 | `ERROR_FILE_REJECTED` | 附件扩展名/文件名命中安全黑名单 |
| 2008 | `ERROR_FILE_TOO_LARGE` | 附件字节数超过 `file.max_bytes` |

> v1.14 起上表所有 code 的失败响应都是 **HTTP 200**，不再有"code → HTTP 状态"的映射。
> `errs.HTTPStatus` 与 `AppError.HTTPStatus` 仍然保留，只服务于 §5 例外清单里的场景
> （健康检查等），**不得**再用来决定业务失败的状态码。

**公开组（8640）**

`GET /site` · `GET /posts` · `GET /posts/{slug}` · `GET /posts/{slug}/related` · `GET /posts/{slug}/comments` · `POST /posts/{slug}/comments` · `POST /posts/{slug}/like` · `DELETE /posts/{slug}/like` · `GET /posts/{slug}/stats` · `GET /tags` · `GET /tags/{slug}` · `GET /archive` · `GET /series` · `GET /series/{slug}` · `GET /search?q` · `GET /links` · `GET /pages` · `GET /pages/{slug}` · `POST /events/view` · `GET /attachments/{id}/go` · `GET /preview/{token}`（`no-store`） · `GET /system/ping`（骨架探活）

**管理组（8641）**

`POST /auth/login` · `POST /auth/logout` · `GET /auth/me` · `GET|POST /posts` · `GET|PUT|PATCH|DELETE /posts/{id}` · `POST /posts/{id}/publish` · `POST /posts/{id}/schedule` · `POST /posts/{id}/unpublish` · `GET /posts/{id}/revisions` · `POST /posts/{id}/revisions/{rid}/restore` · `POST /posts/{id}/preview-link` · `DELETE /preview-links/{token}` · `POST /posts/preview` · `GET|POST /media` · `DELETE /media/{id}` · `DELETE /media/{id}/original` · `POST /media/{id}/retry` · `GET /storage` · `POST /storage/cleanup` · `GET|PATCH /comments` · `POST /comments/bulk` · `GET|PUT /settings` · `POST /notify/test` · `GET|POST|PUT|DELETE /{tags|series|links|redirects|attachments}` · `POST /attachments/upload` · `GET /stats/overview` · `GET /stats/daily?from&to` · `GET /jobs` · `POST /jobs` · `POST /jobs/{id}/retry` · `GET /audit-logs`

> **尚未实现**（接口清单里出现过、路由还没注册的，别当契约用）：管理端 `GET|PUT /pages/{slug}`（静态页写接口）与 `POST /api/internal/revalidate`。

**内部组（8641，仅容器内/隧道）**

`POST /api/internal/revalidate` · `GET /api/internal/health` · `GET /healthz` · `GET /readyz`（DB 可查 + 磁盘 <85% + uploads 可写 + 队列积压 <100）

**互动与统计口径（D12）**

| 项 | 规则 |
| --- | --- |
| 点赞 | `likes` 主键 `(post_id, visitor_hash)`；`POST /posts/{slug}/like` 幂等（已赞不重复计数），`DELETE` 幂等取消；`posts.like_count` 每次变更后按 `SELECT COUNT(*) FROM likes WHERE post_id=?` 重算 |
| 点赞限流 | **10 次 / 分钟 / visitor**，键为 `visitor_hash`（`utils.HashVisitor`），超限 `code 1005` |
| PV 明细 | 每次 `POST /events/view` 写一行 `page_views`（`post_id` 可为 NULL、`path`、`visitor_hash`、`referrer_host`），明细保留 90 天由 `cleanup` 裁剪 |
| PV 去重 | **同 visitor 同文章 10 分钟内只 +1**（去重键：文章页用 `post_id`，非文章页用 `path`）；重复打点返回 `recorded:false`，不写计数 |
| 计数落点 | 计入时 `posts.view_count+1`，并同时 upsert `stats_daily` 的**文章行**与 `post_id=0` **全站行** |
| UV | 不增量近似：按上海日期 `COUNT(DISTINCT visitor_hash)` 重算该日两行的 `uv` |
| 切日 | `stats_daily.date` 为 `Asia/Shanghai` 的 `YYYY-MM-DD`；日界 = 当地 00:00 |
| 展示 | 阅读数 / 点赞数一律**客户端异步**取（`GET /posts/{slug}/stats`），不进 SSR HTML（§17.1） |
| 管理端 | `GET /stats/overview`（文章/评论/媒体状态计数、PV/UV、点赞、Top 5 文章）、`GET /stats/daily?from&to`（全站行，缺省最近 30 天，范围 ≤366 天） |

**发布控制与任务口径（D13）**

| 项 | 规则 |
| --- | --- |
| 定时发布 | `POST /posts/{id}/schedule`（`publishedAt` 必须是未来时间）→ 落 `scheduled` 并投递 `publish_scheduled` 任务（`run_at = publishedAt`，payload `{postId}`）；重复调用会先撤掉该文待执行的同类任务 |
| 到点生效 | 任务执行时再判一次：`status=scheduled` 且 `published_at <= now` 才发布，否则跳过（用户可能已取消定时）；发布逻辑与手动 `publish` 完全同源（写 revision、重算渲染、revision+1） |
| 取消定时 | `POST /posts/{id}/unpublish` 与 PATCH 回 `draft` 都撤掉待执行的 `publish_scheduled` 任务 |
| 版本回滚 | `POST /posts/{id}/revisions/{rid}/restore`：用旧版本内容覆盖正文，并**再写一条新 revision**（`revision+1`，note 记来源），历史不被改写 |
| 预览链接 | `POST /posts/{id}/preview-link` 生成一次性 token（明文只回一次，库里只存 `token_hash`，24h 过期）；`GET /preview/{token}`（`no-store`）首次访问写 `used_at` 并返回内容，之后或过期 → `code 2006`；不存在 → `code 1002` |
| 预览内容 | 预览页能看草稿 / 定时 / 归档文章；`robots.txt` 禁 `/preview`，且预览不参与 sitemap 与 RSS |
| 任务投递 | `GET /jobs` 列表、`POST /jobs/{id}/retry` 重试失败任务、`POST /jobs` 手动投递（只允许 `cleanup` / `backup` / `link_check` / `integrity_check`，用于运维与自测） |
| 周期任务 | 调度器每 tick 检查一次"当天该跑还没跑"的周期任务（按站点时区）：`backup` 03:00、`cleanup` 04:00；`integrity_check` 每周一 04:30、`link_check` 每周一 05:00。判据是 `jobs` 里当天是否已有同类型记录，重启不会重复跑 |

**内容管理口径（D14）**

| 项 | 规则 |
| --- | --- |
| 分类 CRUD | `GET|POST /{tags\|series}`、`PUT|DELETE /{tags\|series}/{id}`；`name` 必填且唯一，`slug` 可留空（ASCII 名自动生成，中文名必须手填，非法/冲突 → `code 1003`、`code 2001`）；列表带 `count`（该分类下**未删除**文章数，含草稿） |
| 删除分类 | 连带清掉 `post_tags` / 把 `posts.series_id` 置 NULL，不删文章 |
| 文章标签 | `POST|PUT|PATCH /posts` 的 `tagIds`（整数数组，**替换式**：传了就整体替换该文的标签，不传 = 不改；自动去重、忽略 `≤0`、最多 20 个，超限 → `code 1003`）；**id 必须存在**，否则 `code 1003`（`data` 带 `tagIds`）。管理端详情回显 `tagIds`，列表项回显 `tags[{name,slug}]` |
| 文章系列 | `seriesId`（`0` 表示清空）+ `seriesOrder`（`≥0`）；`seriesId` 指向不存在的系列 → `code 1003`（`data` 带 `seriesId`）。一篇文章最多属于一个系列 |
| 编辑器回显 | 管理端详情另出 `cover` / `ogImage`（`{id,url,alt,width,height}`，由 service 按外键补齐），供编辑器显示已选封面；外键指向的图已被删除时为 `null`（不报错） |
| 友链 CRUD | `GET|POST /links`、`PUT|DELETE /links/{id}`；`status` 只有 `active`/`hidden`，公开端只出 `active` |
| 跳转 CRUD | `GET|POST /redirects`、`PUT|DELETE /redirects/{id}`；`status` 只有 301/302，`fromPath` 唯一且必须以 `/` 开头（≤200），`toPath` 必须以 `/` 开头或 `http(s)://` 开头（≤500）；**公开边缘按 `from_path` 精确匹配直接 301/302 并 `hits+1`**（在转 Next 之前处理） |
| 附件 | `GET|POST /attachments`、`POST /attachments/upload`、`PUT|DELETE /attachments/{id}`；JSON 创建只用于**外链**（`shareUrl` 必填且是 http(s)，留空 → `code 1003`），本地文件走 `POST /attachments/upload`（multipart：`file`、`postId`、可选 `title`/`version`/`sort`，`title` 默认取去掉扩展名的文件名） |
| 附件安全 | 用**黑名单**拦扩展名与固定文件名（服务端脚本、可执行文件、浏览器会当页面渲染的 `html/svg/xml` 等，见 §10），命中 → `code 2007`；超过 `file.max_bytes` → `code 2008`；隐藏文件（`.` 开头）一律拒绝 |
| 附件下载 | 公开 `GET /attachments/{id}/go`：本地的直接下发（`application/octet-stream` + `Content-Disposition: attachment` + `nosniff`，支持 Range），外链的 `302` 到 `share_url`；两种情况都 `clicks+1`、`no-store`；记录不存在 → `code 1002`，文件丢了 → `code 1007` |
| 存储管理 | `GET /storage`：占用统计（原图/变体/附件/合计）+ 孤儿文件（磁盘有、库里无）+ 缺失文件（库里有、磁盘无）+ 已删原图数，清单各截断到 200/100 条但计数是全量；`POST /storage/cleanup` 只删孤儿，返回 `{removedFiles,removedBytes,failedFiles}` |
| 设置 | `GET|PUT /settings`：可写 `imgBaseUrl`、`siteTimezone`（IANA 名，非法 → `code 1003`）与 `notify`（告警通道，见下）；只读回显 `contentVersion`、`upload{dir,maxBytes}`、`file{dir,maxBytes}`、`task{enabled,intervalSeconds}`、`db{path}`（**不含任何口令**）。写内容类设置必须 `content_version+1`（公开读的 ETag 依赖它）；**只改 `notify` 不动 `content_version`**（告警配置与内容无关） |
| 任务页 | `GET /jobs` 分页 + `POST /jobs` 手动投递 + `POST /jobs/{id}/retry`；`payload` 原样回显 JSON 字符串 |
| 审计页 | `GET /audit-logs` 分页，支持 `action` / `targetType` / `targetId` 过滤；`meta` 以对象回显（库里是 JSON 字符串） |

**告警口径（D17）**

| 项 | 规则 |
| --- | --- |
| 配置 | `settings.notify`：`{enabled, channel: webhook\|telegram, webhookUrl, telegramBotToken, telegramChatId, events{jobFailed,diskHigh,certExpiring}, diskThresholdPercent(1–99，默认 85), certExpireDays(1–3650，默认 14)}`；阈值与勾选都可改，越界 → `code 1003` + 字段错误数组 |
| 密钥 | `webhookUrl` 与 `telegramBotToken` **永不回显**，只回 `webhookUrlSet` / `telegramBotTokenSet`；PUT 时 `null` = 保持不变、`""` = 清空、非空 = 覆盖。审计只记 `webhookUrlSet` 之类的布尔值（§9） |
| 启用校验 | `enabled=true` 时对应渠道必填项不能为空（webhook 要地址，Telegram 要 token + chatId），否则 `code 1003` |
| 事件 | `job_failed`（任务 failed）、`disk_high`（uploads 所在文件系统占用 ≥ 阈值）、`cert_expiring`（`tls.cert_file` 的 `NotAfter` 距今 < 阈值；未配置该文件则完全跳过） |
| 去重 | 同一事件投递成功后 24h 内不再重复；失败则 15 分钟后重试（不让一次网络抖动吞掉告警）；条件恢复即清空该事件的状态 |
| 报文 | webhook：`{"source":"blog","event":"job_failed","level":"error","message":"…","at":"RFC3339","details":{…}}`；Telegram：纯文本（`[zjut.si 博客] 告警\n…`），不用 Markdown |
| 自检 | `POST /notify/test`：按**已保存**的配置立刻投递一条测试告警，不看 `enabled` 也不受静默期影响；通道没配齐 → `code 1003`，投递失败 → `code 1006`（原因写日志，`msg` 只给"超时/HTTP 4xx/连不上"这一级） |
| 回显 | `GET /settings` 的 `notify` 另含只读 `certFile`（来自 `BLOG_TLS_CERT_FILE`）与 `lastAlerts[{event,at,ok}]`（各事件最近一次投递结果） |

---

## 6. 状态机

**文章**

```
draft ──publish──▶ published ──archive──▶ archived
  │                   ▲                      │
  └─schedule─▶ scheduled ──job──▶ published   │
                 │                            │
             unpublish ──▶ draft ◀────restore─┘
任意状态 ──soft delete──▶ deleted(30d) ──▶ 物理删除
```

- `scheduled` 必须满足 `published_at > now`
- 已发布文章 `slug` 不可变；确需修改时必须同时写 `redirects`，否则 `code 2004`
- 每次 publish：`revision+1`、写 `post_revisions`、重算 `content_html/content_text/word_count/reading_time`
- 已发布文章再次 publish：**内容与最新版本一致时按「重复发布」返回 `code 1004`**；
  内容有变化（改过标题或正文）才允许，并**为改动记一个新版本**（`revision+1`，保留原 `published_at`）——
  否则线上改字永远进不了版本历史，回滚也就无从谈起。自动保存（PATCH）仍然**不产生** revision
- `archived` 与已删除文章对外返回 `code 1007`，不出现在列表、搜索、sitemap、RSS

**媒体**

```
uploading(内存态) ─落盘校验─▶ processing ─成功─▶ ready
                                └─失败(重试2次)─▶ failed ─retry─▶ processing
ready: refs==0 → 物理删除；refs>0 → code 2002
```

- worker 全局并发 **固定为 1**
- `pixels > 50_000_000` → `failed / code 2003`
- 引用检查三处：`cover_media_id`、`og_media_id`、`content_md` 含 `storage_key`

**评论**

```
新建 ──▶ pending ──管理员通过──▶ approved（公开可见）
            ├──管理员标记──▶ spam（不进公开列表）
            └──管理员删除──▶ deleted（软删，记录保留，可在管理端按状态查回）
```

- 嵌套**固定两层**：顶层评论 + 它的一层回复；回复的回复挂到同一个顶层下（按 `root_id` 聚合），不再加深
- 新评论一律 `pending`，公开列表只出 `approved`
- 限流 **3 次 / 10 分钟 / IP**（进程内令牌桶，超限 `code 1005`）
- `posts.comment_count` 只统计 `approved`，状态变更时重算
- 邮箱只入库（含 `email_hash`），任何公开接口都不返回；`ip_hash` 同理

**任务**

```
pending ─lock─▶ running ─ok─▶ done
   ▲             ├─err & attempts<max ─▶ pending(退避 1m/5m/15m)
   │             └─err & attempts=max ─▶ failed
   └── locked_at 超 10 分钟 ──┘
```

---

## 7. 后端规范（Go / gin / gorm / SQLite）

| 项 | 规定 |
| --- | --- |
| Go 版本 | ≥1.23，`go build` 静态编译，CGO disabled |
| SQLite 驱动 | `glebarez/sqlite`（纯 Go） |
| ORM | gorm 仅用于查询与写入，`AutoMigrate` 禁止出现在任何代码路径 |
| 迁移 | `golang-migrate` 核心 + `migrations/` 版本化 SQL；数据库驱动自写（`core/db/migrate.go`，ADR 0002）；必须纯 Go；上线前通过"空库全量迁移"验证 |
| PRAGMA | `journal_mode=WAL` `synchronous=NORMAL` `busy_timeout=5000` `foreign_keys=ON` `temp_store=MEMORY`。**必须写进 DSN**（`?_pragma=...`）或 `OnConnect` 钩子，否则只有第一条连接生效 |
| 缓存预算 | `cache_size=-16384`（16MB/连接），`MaxOpenConns=4` → 合计 64MB |
| 连接池 | `MaxOpenConns=4` `MaxIdleConns=4`；写事务用 immediate |
| 内存 | `GOMEMLIMIT=400MiB`，`GOGC=60`，禁止无界 goroutine 与无上限 buffer |
| 路由 | gin；`TrustedProxies=["127.0.0.1","172.16.0.0/12"]`（容器看到的来源是 docker 网关，不是回环） |
| 响应 | 只用 `model/common/response`：`OkWithData` / `FailWithDetailed`；连接建立后一律 HTTP 200，状态语义全部由 `code` 表达（含路由不存在 `1002` 与转发 Next 失败 `1006`，见 §5 例外清单） |
| 中间件顺序 | request_id → panic 恢复 → 访问日志 → 限流 → 鉴权 → CSRF → handler |
| 日志 | `slog` JSON 到 stdout，字段 `ts,level,request_id,method,path,status,dur_ms,ip_hash`；禁止打印密码、邮箱、cookie |
| 错误处理 | 业务错误用 `errs.AppError{Code,HTTPStatus,Message}`；失败响应一律 HTTP 200，前端只看 `code`（`HTTPStatus` 已不决定状态码）；禁止把 `err.Error()` 直接返回前端 |
| 上传 | `MaxMemory=1MB` 流式落盘；先 `DecodeConfig` 再 `Decode` |
| 时间 | 仅用 `time.Time` 在内存中，落库前转 Unix 秒（统一 `dbtime.Unix` 类型） |
| 并发 | 共享可变状态必须加锁；任务 worker 单实例；用 `errgroup`，禁止裸 `go func()` |
| 事务 | `global.DB.Transaction(func(tx *gorm.DB) error {...})`，tx 透传，service 发起 |
| 测试 | 表驱动 + `t.Parallel()`；service 覆盖率 ≥70%；每个 API 至少 happy path + 错误路径 |
| Lint | `gofmt` + `goimports` + `golangci-lint`（`govet,errcheck,staticcheck,revive,gosec,bodyclose,unparam`） |
| 注释 | 导出符号必须注释；标识符英文，注释中文 |

**禁止**：手写 SQL 拼接、`fmt.Sprintf` 拼 SQL、在 api 层写业务规则、在 model 层做权限判断、service 接收 `*gin.Context`。

### 7.1 D1 必验清单（任一失败就要先改契约）

| # | 验证项 | 失败后的退路 |
| --- | --- | --- |
| V1 | FTS5 可用 + `tokenize='trigram'` 可建表 + 2 字中文可搜 | `trigram` 只用于 ≥3 字，1–2 字走 `content_text LIKE` 兜底 |
| V2 | `golang-migrate` 的纯 Go SQLite 驱动可用 | 自写极简 migrator（只读 `migrations/*.sql` + `schema_migrations` 表） |
| V3 | WebP 编码在 CGO 关闭下可用（`gen2brain/webp` 的 wazero 后端等） | 降级为 JPEG 三档；或改为独立转码容器（违反 C1，必须重新评审） |
| V4 | 4 连接 × 16MB 缓存实测内存 ≤ 100MB | 连接池降到 2 |
| V5 | 客户端 KaTeX 懒加载后单页 JS 增量 ≤ 90KB gzip | 改为独立 `/math/[slug]` 渲染，或回到服务端方案 |

---

## 8. 前端规范（Next.js + Tailwind + shadcn）

**目录与别名**（dub 的组织方式）

```
apps/web/
├─ app/(public)/ (admin)/ (preview)/ api/
├─ ui/{shadcn,layout,post,admin,shared}     # shadcn 生成物在 ui/shadcn，不手改
├─ lib/{api,hooks,zod,constants,swr}
├─ styles/{globals.css,prose.css}
└─ tests/ e2e/
```

```json
{ "compilerOptions": { "baseUrl": ".", "paths": {
  "@/ui/*": ["ui/*"], "@/lib/*": ["lib/*"], "@/styles/*": ["styles/*"], "@/app/*": ["app/*"] } } }
```

```json
{ "aliases": { "components": "@/ui", "ui": "@/ui/shadcn",
  "lib": "@/lib", "hooks": "@/lib/hooks", "utils": "@/lib/utils" } }
```

| 项 | 规定 |
| --- | --- |
| Next | App Router，`output: 'standalone'`，Node 堆限 `--max-old-space-size=384` |
| 渲染 | 公开页 `export const dynamic = 'force-dynamic'`；管理页整树 `'use client'` |
| 构建 | **构建期禁止访问 API**；任何路由不得在 build 时预渲染取数 |
| 样式 | Tailwind + shadcn/ui；`ui/shadcn/*` 不手改，定制写 wrapper；颜色与间距只用 token |
| 数据 | 仅通过 `lib/api/`：公开页服务端调 `http://127.0.0.1:8640/api/*`；管理页浏览器调同源 `/api/admin/*` |
| 状态 | 管理端用 **SWR**（`lib/swr` + `lib/hooks`）；不引入 Redux |
| 表单 | `react-hook-form` + `zod`，schema 在 `lib/zod/`，与服务端校验规则一致 |
| Markdown 编辑 | 受控 textarea + 防抖自动保存（30s 或失焦）+ `POST /posts/preview` 服务端预览；工具栏插入语法（粗体/链接/行内公式/块级公式/代码块/表格/图片） |
| 编辑器预览 | 预览面板复用 `ui/post/PostContent`（与服务端同一渲染管线 + KaTeX 懒加载），**行内 `$x$` 与块级 `$$…$$` 都在预览里出公式**，不是原文 |
| 编辑器图片 | 拖拽文件或粘贴截图到正文框即上传（`POST /api/admin/media`）并插入 Markdown；预览里的图片可点选成「图片框」，拖右下角手柄改宽度，写回 Markdown 的 `{width=N}` 属性（§11） |
| 图片 | 不使用 `next/image`，统一 `<PostImage>` 输出 480/960/1600/2400 + blurhash 占位 |
| 数学 | **客户端 KaTeX**：渲染管线输出 `<span class="math" data-tex="...">`，`<MathRender>` 懒加载替换；无公式页面不加载 |
| 命名 | 文件 kebab-case、组件 PascalCase、类型 `XxxProps`；不用 `React.FC` |
| 导出 | 只有 Next 约定文件用 default export，其余具名导出 |
| 错误 | `lib/api/fetcher.ts` 统一抛 `ApiError{code,message,status}`；**按 `code` 分支**（业务失败也是 HTTP 200，`status` 只作诊断）；`error.tsx` 兜底，不暴露堆栈 |
| 登录态 | `app/(admin)/admin/(protected)/layout.tsx` 先 `GET /auth/me`，`code 1000` 直接跳 `/admin/login`，避免闪烁 |
| 可访问性 | 语义标签、焦点可见、图片 alt 必填、对比度 AA、`eslint-plugin-jsx-a11y` |
| 质量门 | `pnpm lint && pnpm typecheck && pnpm test && pnpm build` 全绿才允许提交 |
| 禁止 | Server Actions（会绕过 `lib/api`）、组件内拼 API 路径、`app/api` 透传业务数据、AntD/MUI/Redux |

**公开页路由**（D9–D10）：`/`、`/posts`、`/posts/[slug]`、`/tags`、`/tags/[slug]`、`/archive`、
`/search`、`/series`、`/series/[slug]`、`/links`，以及用 `[slug]` 承载 `/about` 等 `pages` 静态页
（静态段优先于动态段，未知单段路径落 404）。

**图片与正文**：列表与详情的封面统一走 `ui/post/PostImage`（自出 `srcset` + blurhash 占位，
不用 `next/image`）；正文走 `ui/post/PostContent`（注入服务端 HTML，客户端只做 KaTeX 懒加载与
代码块复制按钮，无 JS 时正文完整可读）。

---

## 9. 安全规范

| 面 | 规定 |
| --- | --- |
| 密码 | argon2id；密码 ≥16 位；不落日志 |
| 登录 | 5 次失败/10min 锁定 30min；失败写 `audit_logs` + `login_attempts` |
| 锁定键 | **按用户名计数**。管理端经隧道访问，来源 IP 恒为回环/网关，按 IP 计数无意义；`ip_hash` 只用于审计 |
| 会话 | `sid` cookie：`HttpOnly` `SameSite=Strict` `Path=/`，不设 `Secure`（走 http://localhost）；7 天滑动过期；改密即全端失效 |
| CSRF | 双提交：`GET /auth/me` 返回明文 `csrf_token`，服务端存 `csrf_hash`；写请求带 `X-CSRF-Token` |
| XSS | Markdown 渲染后过 bluemonday 白名单：禁 `script/iframe/on*/style` 表达式；允许 `span.math[data-tex]` |
| 上传 | 魔数校验 + 扩展名白名单（jpeg/png/webp/gif）+ 像素上限 + 大小上限 30MB；HEIC/AVIF 明确拒绝并提示前端转码；文件名不落地 |
| EXIF | 入库前按 Orientation 摆正并剥离全部 EXIF（含 GPS），再生成变体 |
| SQL | 全参数化；FTS5 查询字符串必须转义 |
| 越权 | 8640 层直接挡掉 `/admin`、`/api/admin`，不依赖登录态判断（`NoRoute` 回 `200 + code 1002`，不泄露路径是否存在） |
| 限流与锁定 | 登录 5/min（按用户名）；评论 3/10min/IP；公开 API 20 r/s；点赞 10/min/visitor。**登录时先判锁定再判限流**：账号已锁定回 `code 2005`，不能回 `code 1005`（否则用户只会看到"稍后再试"却永远进不去） |
| 响应头 | `X-Content-Type-Options: nosniff`、`Referrer-Policy: strict-origin-when-cross-origin`、`Permissions-Policy` 最小化 |
| CSP | `script-src 'self' 'nonce-{n}' 'strict-dynamic'`，nonce 由 `middleware.ts` 注入。**禁止 `unsafe-inline`**，否则 Next RSC 水合会失败；`style-src 'self' 'unsafe-inline'` 可保留（Tailwind 运行时无内联样式，但 shadcn 组件有） |
| 敏感信息 | `.env` 不入库不入镜；`admin_email` 不在任何公开接口出现 |
| 审计 | 所有管理端写操作写 `audit_logs`（action + target + diff 摘要） |

---

## 10. 媒体与附件存储规范

**图片上传流程**

1. 浏览器端预压缩（长边 2560、WebP q0.85）→ 失败则原图直传
2. Go 流式落盘临时文件 → 魔数/大小/像素校验 → 摆正并剥离 EXIF
3. `sha256` 去重：命中则直接返回已有 `media`
4. 入库 `processing` → 投递 `media_process`
5. worker（并发 1）：生成 480/960/1600/2400 四档 WebP（q82）+ 20px blurhash；**变体不超过原图尺寸，不放大**
6. `ready` → 结束。**没有归档步骤**：原图与变体都留在本机磁盘，原图**永久保留**（不再有 30 天清理）

**磁盘布局（唯一允许的形态）**

```
{BLOG_UPLOAD_DIR}/{sha[0:2]}/{sha}/original.{ext}      # 原图，永久保留
{BLOG_UPLOAD_DIR}/{sha[0:2]}/{sha}/{480|960|1600|2400}.webp
{BLOG_FILE_DIR}/{sha[0:2]}/{sha}{ext}                  # 附件，平铺单文件、无变体
```

- 两个根目录都必须落在容器内的 bind mount 上（默认 `/data/blog/uploads`、`/data/blog/files`），**不许相同**
- 数据库只存 `storage_key`（图片是 `{sha[0:2]}/{sha}`，附件是 `{sha[0:2]}/{sha}{ext}`）与元数据，**永不存 URL**
- 临时文件写在各根目录的 `.tmp/` 下，写完 `rename` 到位，读路径看不到半截文件

**URL 规则**

```
{img_base_url}/{storage_key}/{variant}.webp        例：/uploads/ab/abcdef…/1600.webp
{img_base_url}/{storage_key}/original.{ext}        # 「查看原图」，同源 nginx 直出
/api/attachments/{id}/go                           # 附件唯一的下载出口
```

- 禁止查询参数做版本（`?v=` 一律不允许）
- 禁止把附件目录直接 alias 给 nginx：附件必须经 `/go` 出口，才能保证 `Content-Disposition: attachment`
  + `Content-Type: application/octet-stream` + `nosniff`（任何扩展名都不会被浏览器当页面渲染）
- `media` 表永不存 URL，只存 `storage_key` + `variants_json`
- 「查看原图」指向**本地原图**：公开详情返回的 `contentHtml` 会给本站图片包一层
  `<a class="media-orig" href="{img_base_url}/{storage_key}/original.{ext}" target="_blank" rel="noopener noreferrer">`。
  链接在**读取时现算**；该图的原图被管理端手动删掉（`orig_archived=1`）时 `contentHtml` 原样返回、`MediaItem.origUrl` 为空串
- 正文图片的 `srcset`（四档变体）/`sizes`/`width`/`height`/`loading` 也在**读取时**由同一处补上：
  存库的 `content_html` 保持编辑器产出的原始形态，响应里才是响应式版本（§16 G4「srcset 生效」）

**附件安全（黑名单，`code 2007`）**

文件既不会被执行、也不会被直出，所以只挡三类：能在服务器上跑的脚本（`php*`/`phtml`/`phar`/`jsp*`/`asp*`/`cgi`/`pl`/`py`/`rb`/`lua`/`sh`/`bat`/`ps1`/`jar`/`class`…）、
浏览器会当页面渲染的（`html`/`htm`/`xhtml`/`shtml`/`svg`/`xml`/`mhtml`…）、以及可执行程序与装机镜像（`exe`/`msi`/`scr`/`vbs`/`js`/`apk`/`dmg`/`iso`/`deb`/`rpm`…）。
另外固定拒绝 `.htaccess`/`.htpasswd`/`.user.ini`/`web.config`/`php.ini` 与一切 `.` 开头的隐藏文件。判定只看**最后一个扩展名**，大小写不敏感。

**删除与空间回收**

- 图片：有引用返回 `code 2002` + 引用清单；无引用才删文件与记录
- 附件：删记录后，若没有别的记录引用同一 `storage_key` 才删文件（同一份文件可被多篇文章共用）
- `DELETE /media/{id}/original`：只删原图、保留四档变体，置 `orig_archived=1`；此后不能 `retry`（原图是重新转码的唯一来源）
- **原图与附件都不自动清理**：它们是唯一副本。回收空间只能在 `/admin` 的存储管理里手动操作（删原图保变体、清理孤儿文件）
- 物理删除文章（回收站满 30 天）时，其附件的文件也会在没人再引用后被删掉

---

## 11. 内容与渲染规范

| 项 | 规定 |
| --- | --- |
| 存储格式 | Markdown（GFM）+ frontmatter 语义字段存在表列，不存 YAML |
| 渲染 | 服务端 goldmark + chroma + bluemonday；`content_html` 落库，读路径不重复渲染 |
| 数学 | goldmark 阶段只把 TeX 包成 `<span class="math" data-tex="{escaped}">`，**不在服务端渲染 KaTeX**；前端 `<MathRender>` 动态 import `katex` 并替换 innerHTML；无 JS 时显示 TeX 原文 |
| 代码块 | 行号、复制按钮、文件名标签、语言高亮（diff 支持） |
| 图片尺寸 | 允许 goldmark 属性语法 `![alt](src){width=480}`：编辑器拖拽写回 `width`（`height` 按原图比例由读取时补），读取时补 `srcset` 的一处（`DecorateContent`）**不覆盖**已有 `width` |
| TOC | 从 `h2/h3` 提取，生成锚点，正文标题自动加 `id` |
| 摘要 | 未填 `excerpt` 时取正文前 160 字纯文本 |
| 阅读时长 | `ceil(word_count / 300)` 分钟；`word_count` = 中文字符数 + 英文单词数（中文按字符计） |
| slug | 手填优先；未填时取标题拼音前 40 字符，失败用 `post-{YYYYMMDD}-{HHmm}`；发布后不可变 |
| 上一篇/下一篇 | 按 `published_at` 相邻，仅同状态 |
| 时间展示 | 站点时区 `Asia/Shanghai`；存储 UTC |
| SEO | 每页唯一 `title`/`description`/`canonical`（分页列表的 canonical 是它自己，不指回第 1 页）；文章页输出 `Article` JSON-LD（首页输出 `WebSite` + `SearchAction`），`<script type="application/ld+json">` 带 CSP nonce；`sitemap.xml` 只含已发布文章 + 静态页 + 非空标签/系列；`rss.xml` 取最近 20 篇、`<content:encoded>` 全文；`robots.txt` 禁 `/admin`、`/preview`、`/api/`；`/search`、`/preview/*`、全部 `/admin*` 为 `noindex`；OG/Twitter 卡片用封面 `1600` 变体，**不做**动态 OG 图片（白名单里没有生成库） |
| 草稿隔离 | 未发布内容在任何公开接口、sitemap、RSS、搜索中均不可见 |
| 410 | `archived` 与已删除文章：API 返回 `code 1007`（HTTP 200），页面仍返回 HTTP **410**（由 `middleware.ts` 决定），不返回 404 |

**410 的落点**：App Router 的页面无法自定义状态码（`notFound()` 只会给 404），所以
`/posts/{slug}` 的 410 由 `apps/web/middleware.ts` 在进入页面前探测 `GET /api/posts/{slug}`
的**信封 `code`**（`1007`）并直接返回一份独立的 410 HTML；客户端导航（RSC 请求）不走这条路径，
由页面按 `code 1002`/`1007` 走 404 兜底。

---

## 12. 缓存与静态资源策略

| 资源 | 头 | 说明 |
| --- | --- | --- |
| `/_next/static/*` | `public, max-age=31536000, immutable` | 文件名含哈希 |
| `/uploads/*` | `public, max-age=31536000, immutable` | 路径含内容哈希，nginx 直出 |
| KaTeX 字体与 CSS | `public, max-age=31536000, immutable` | 自托管，不打 CDN |
| HTML | `Cache-Control: no-cache` + 强 `ETag` | 每次回源校验，304 省流量 |
| 公开 API | `Cache-Control: no-cache` + `ETag` | ETag 取响应体哈希 |
| `sitemap.xml` / `rss.xml` / `robots.txt` | `public, max-age=600` | sitemap 与 rss 每请求渲染（构建期不取数）；robots.txt 只依赖常量，构建期生成为静态文件 |
| 管理端全部 | `no-store` | 禁止任何中间层缓存 |

**明确不做**：Redis、ISR、`revalidate` 定时、`proxy_cache`、CDN、Service Worker。
**预留**：nginx 侧保留 `proxy_cache_path` 配置位（默认注释），后期开启不改 upstream。

---

## 13. 异步任务与定时

| 任务 | 触发 | 行为 |
| --- | --- | --- |
| `publish_scheduled` | 定时发布时按 `published_at` 投递 | `scheduled` 且 `published_at<=now` → 发布（与手动发布同源）；已取消则跳过 |
| `media_process` | 上传后 | 生成变体 + blurhash |
| `link_check` | 每周一 05:00 | HEAD 检查外链与正文图片，写 `link_checks` |
| `backup` | 每日 03:00（Asia/Shanghai） | `VACUUM INTO` + `uploads` 与 `files` 两个目录的硬链接快照（`blog-YYYYMMDD.db` / `uploads-YYYYMMDD/` / `files-YYYYMMDD/`；库留 30 天、目录留 90 天） |
| `cleanup` | 每日 04:00（Asia/Shanghai） | 物理删软删 >30 天的文章（含其附件文件）、删 `page_views` >90 天、裁剪 revision 到 50、删过期 session/preview_token/login_attempts。**不动原图与附件**（§10） |
| `integrity_check` | 每周一 04:30（Asia/Shanghai） | 库上跑 `PRAGMA integrity_check`（必须是 `ok`），并确认最近一次 `blog-*.db` 快照存在且是完整 SQLite 文件（校验文件头）；不通过即任务失败 → 触发告警 |

调度器：单 goroutine ticker，每 60s 扫描；取任务前 `UPDATE ... SET status='running', locked_at=? WHERE id=? AND status='pending'` 保证互斥。

**告警（v1.16 起）**：任一任务进入 `failed`、磁盘占用 ≥ 阈值（默认 85%）、TLS 证书剩余天数 < 阈值（默认 14 天）→ 按 `settings.notify` 发一条通知。
渠道二选一：webhook（`POST` JSON `{source,event,level,message,at,details}`）或 Telegram（Bot API `sendMessage`）。
同一事件在**投递成功后 24h 内不重复**，失败则 15 分钟后重试；条件恢复（磁盘降回、证书换新）即清空静默状态。
投递状态存 `settings.notify_state`，`POST /notify/test` 可随时自检通道；磁盘与证书由一个**独立的巡检 goroutine 每 60s** 看一眼（与调度器同生命周期、由 `errgroup` 托管，不新增进程与表）。
**应用自身挂掉时它发不出告警**，宿主侧另配外部探活（runbook §20.4）。

---

## 14. 运维与部署

| 项 | 规定 |
| --- | --- |
| 基础镜像 | Ubuntu 24.04 宿主；容器内 debian-slim + 静态二进制 + Node 运行时 |
| 进程托管 | s6-overlay：`blogd`（Go）与 `web`（Next）两个 longrun。**任一进程退出由 s6-supervise 原地拉起**（容器不重启）；整个容器退出（含 `docker kill`）由 `restart: always` 拉起 |
| 健康检查 | `HEALTHCHECK` 打 `127.0.0.1:8641/readyz`（间隔 30s / 超时 5s / 3 次失败）。失败只把容器标成 `unhealthy`（`docker inspect .State.Health` 可见、`readyz` 同时 503），docker 本身**不会**因不健康重启容器；要自动重启就在宿主加 `autoheal`（runbook §19 第 9 步，可选） |
| 资源 | `mem_limit: 1.5g`、`restart: always`、`logging: json-file, max-size=10m, max-file=3` |
| 卷 | `/data/blog/db`（SQLite）、`/data/blog/uploads`（图片）、`/data/blog/files`（附件）、`/data/blog/backup`（备份落点，必须与前三者同一个文件系统） |
| nginx | 80 → `127.0.0.1:8640`；`client_max_body_size 32m`；`proxy_request_buffering off`；`/uploads/` 直出磁盘；`valid_referers` 防盗链 |
| TLS | certbot 自动签发 + 自动续期（systemd timer），`renew --dry-run` 每月跑一次；443 强制跳转。容器只**只读**看 `tls.cert_file`（宿主 `/etc/letsencrypt` 挂载）判断剩余天数，签发/续期仍在宿主 |
| 告警 | 设置项 `notify`：任务进入 `failed`、磁盘 ≥ 阈值（默认 85%）、证书剩余天数不足（默认 14 天）→ webhook 或 Telegram（§13）；同一条告警 24h 内只发一次，条件恢复即清零，可用 `POST /notify/test` 自检 |
| 防火墙 | `ufw` 仅开 22/80/443；8640/8641 不对公网开放（由 compose 的 127.0.0.1 绑定兜底） |
| SSH | 禁 root 密码登录，仅密钥；可选 fail2ban |
| 隧道 | `ssh -L 8641:127.0.0.1:8641 user@host`，浏览器访问 `http://localhost:8641` |
| 备份 | `VACUUM INTO /backup/blog-YYYYMMDD.db` + `uploads`/`files` 两个目录的硬链接快照（含原图，目录保留 90 天）；库保留 30 天；每周 `PRAGMA integrity_check` |
| 恢复 | 书面步骤在 `docs/runbook.md`，每季度演练一次"空机恢复" |
| 日志留存 | 容器日志 3×10MB；审计日志永久 |

---

## 15. 性能与内存预算

| 组件 | 稳态 | 峰值 | 上限设置 |
| --- | --- | --- | --- |
| Next | 350M | 400M | `--max-old-space-size=384` |
| Go | 120M | 380M | `GOMEMLIMIT=400MiB` |
| SQLite | 64M | 64M | `cache_size=-16384` × 4 连接 |
| OS + Docker | 320M | 320M | — |
| **合计** | **~860M** | **~1.2G** | **容器 1.5G** |

性能目标：文章页 TTFB <300ms（无缓存回源）、LCP <2.5s（4G）、单篇 JS <150KB gzip（**不含公式的文章**；含公式文章允许 +90KB gzip，KaTeX 懒加载）、单次搜索 <100ms。
磁盘：博客配额 15GB；>85% 告警；单篇图片 10–20MB 为常态，变体总量约原图 30%。

---

## 16. 测试与验收门禁

**每个门禁必须全部通过，不通过不得进入下一阶段。**

| 门禁 | 对应天 | 必过断言 |
| --- | --- | --- |
| G1 骨架与鉴权 | D1–D3 | 空库全量迁移成功（23 表 + FTS 触发器）；`curl 127.0.0.1:8640/api/admin/posts` = 200 + `code 1002`（连接成功即 200，路径不存在由 code 表达）；`curl 127.0.0.1:8640/healthz` = 200 + `code 1002`；`curl 公网IP:8641` 连接拒绝；5 次错误口令后第 6 次 = 200 + `code 2005`（锁定优先于限流）；会话 7 天过期；登出即失效；未登录 = 200 + `code 1000`；无 CSRF = 200 + `code 1001`；响应体为 `{code,data,msg}` |
| G2 文章与渲染 | D4–D6 | 草稿不可见；slug 冲突 = 200 + `code 2001`；`<script>` 被剥离；自动保存不产生多余 revision；发布后立即可见；编辑器预览 = 发布结果；2 字中文词可搜到；归档 = 200 + `code 1007`（页面 410） |
| G3 媒体与附件 | D7–D8 / v1.13 | 31MB 拒绝 = 200 + `code 2003`；50MP 拒绝 = 200 + `code 2003`；同图去重；引用删除 = 200 + `code 2002` + 清单；四档变体可访问；转码峰值 <300MB；GPS EXIF 不出现在输出图；HEIC 被拒并有提示；原图永久保留；附件上传落 `file.dir`、`.php`/`.html`/`.exe` = 200 + `code 2007`、超限 = 200 + `code 2008`；`/go` 响应带 `Content-Disposition: attachment` + `octet-stream` + `nosniff`；`GET /storage` 的孤儿与缺失清单与磁盘一致 |
| G4 公开端 | D9–D10 | 375px 无横向滚动；分页 + 标签组合正确；404 无堆栈；`srcset` 生效；系列/友链/关于页可访问 |
| G5 互动 | D11–D12 | 评论 3/10min 限流 = 200 + `code 1005`；嵌套 ≤2 层；spam 不进公开列表；点赞幂等；同 visitor 10 分钟内 PV 只 +1；`stats_daily` 按上海时区切日 |
| G6 发布控制 | D13 | 定时发布到点生效（误差 <90s）；回滚生成新 revision；预览链接 24h 后 = 200 + `code 2006`；`cleanup` 能删掉构造的过期数据 |
| G7 SEO 与部署 | D15–D16 | `sitemap.xml` 只含已发布（草稿/定时/归档/回收站一律不出现）且 XML 合法；`rss.xml` 可被解析、条目 ≤20 且带 `content:encoded` 全文；每页 JSON-LD 可 `JSON.parse` 且 `@type` 正确；每页恰好一个 `canonical` 且互不相同；`robots.txt` 含 `Sitemap:` 且禁 `/admin`、`/preview`、`/api/`；`docker kill` 与 `pkill -9 blogd` 两种杀法都在 30s 内自愈；隧道可登录（`/admin/login` 出登录页）；nginx 直出静态图片 200 且带 `immutable`、`/admin` 与 `/api/admin` 在公网 404；`certbot renew --dry-run` 通过。逐条命令见 runbook §19 |
| G8 备份、测试与性能 | D17–D19 | `VACUUM INTO` 可还原；空机恢复演练成功（runbook §21）；`POST /notify/test` 能真实送达（webhook / Telegram 至少一种）；`integrity_check` 任务通过；单测/接口/E2E 全绿；service 覆盖率 ≥70%；TTFB <300ms；LCP <2.5s；内存不超 §15 |

**测试要求**：状态机与任务调度必须有单元测试；每个 API 至少一条 happy path + 一条错误路径；禁止提交无测试的状态迁移代码。

---

## 17. 明确禁止与已批准依赖

### 17.1 反模式

| 禁止项 | 原因 |
| --- | --- |
| 引入 Redis / MySQL / Meilisearch / Elasticsearch | 已裁定；单机单作者用不上 |
| 使用 `AutoMigrate` | SQLite 改列会静默失败 |
| 把端口绑到宿主机的 `0.0.0.0` | 绕过 nginx 与隧道，安全事故 |
| 在 8640 返回任何管理页面或管理 API | 破坏唯一安全边界 |
| 接外部图床 / 对象存储（Cloudreve、S3、OSS 等）托管图片或附件 | 外部服务一挂全站破图；图床已下线（v1.13） |
| 把附件目录直接 alias 给 nginx 直出 | 失去 `Content-Disposition`/`nosniff` 保护，可下载的 HTML/SVG 会变成同源 XSS |
| 自动清理本地原图或附件 | 它们已是唯一副本，只能由管理端显式删除（§10） |
| 图片 URL 加 `?v=时间戳` | 摧毁浏览器缓存 |
| 公开 HTML 与公开读接口 `Set-Cookie` | 无意义且污染缓存层；访客态写接口用 `X-Visitor-Id` 请求头（§5） |
| 阅读数/点赞数 SSR 进 HTML | 无缓存层后依然要客户端异步，避免整页重渲染 |
| 引入用户注册/登录/收藏 | 产品明确不要 |
| 引入 TOTP / OAuth / SSO | 已裁定仅密码登录 |
| 引入 Redux / AntD / MUI / 组件库替换 shadcn | 风格与体积双重失控 |
| 在 Next 中直连数据库或写业务规则 | 双写逻辑必然漂移 |
| 手写 SQL 拼接字符串 | 注入风险 |
| `service` 接收 `*gin.Context` | 不可测、不可被 task 复用 |
| 打印密码、邮箱、cookie 到日志 | 泄露 |
| 改动 3065 端口相关配置 | 影响既有服务 |
| 未经记录就修改 §4 表结构或 §5 接口 | 契约漂移，验收失效 |

### 17.2 已批准依赖（白名单）

| 端 | 依赖 |
| --- | --- |
| Go | gin, gorm, glebarez/sqlite, golang-migrate, viper, goldmark(+GFM/attributes), chroma/v2, bluemonday, google/uuid, golang.org/x/crypto(argon2), golang.org/x/sync(errgroup), gen2brain/webp（wazero，构建加 `-tags nodynamic`）, golang.org/x/image(draw), buckket/go-blurhash, golang.org/x/time |
| Web | next(15.x), react, tailwindcss(3.x), shadcn/ui（含 @radix-ui/*、class-variance-authority、tailwindcss-animate）, lucide-react, swr, zod, react-hook-form(+@hookform/resolvers), date-fns, katex, clsx + tailwind-merge |
| 测试 | go test + testify；vitest + testing-library；playwright |

新增任何运行时依赖必须先在本表登记。

---

## 18. 交付节奏（20 天 × 2h）

| 天 | 模块 | 完成标志 |
| --- | --- | --- |
| D1 | 契约 v1.2 + 仓库骨架 + migration + 双 listener + CI 骨架 | G1 的迁移与端口断言 |
| D2 | GVA 骨架落地（response / enter / config / logger / middleware） | `/api/system/ping` 打通 |
| D3 | M1 鉴权 + 限流 + 审计 | G1 全过 |
| D4 | M2 文章 CRUD + 状态机 + revision + slug | 草稿/发布/归档/软删可用 |
| D5 | M2 编辑器 + 自动保存 + 服务端预览 | 可写长文并预览 |
| D6 | M2 渲染管线 + 搜索 | G2 全过 |
| D7 | M3a 流式上传 + 单档 WebP + 媒体库 | 图文混排跑通 |
| D8 | M3b 四档变体 + blurhash + 本地原图永久保留 | G3 全过 |
| D9 | M4a 首页/列表/详情/标签/归档 | 375px 通读 |
| D10 | M4b 搜索/系列/关于/友链/404/410 | G4 全过 |
| D11 | M5 评论 | G5 评论断言 |
| D12 | M6 点赞 + PV/UV + 统计 | G5 全过 |
| D13 | M7 定时发布 + 版本回滚 + 预览链接 | G6 全过 |
| D14 | M8 附件 + 后台任务/审计/设置页 | 管理端闭环 |
| D15 | M9a SEO（sitemap/rss/json-ld/og） | Search Console 可提交 |
| D16 | M9b 部署 + nginx + 隧道 + 证书 | G7 全过 |
| D17 | M10 备份恢复 + 监控告警 + runbook | 空机恢复演练成功 |
| D18 | 测试补齐 + 覆盖率门禁 | G8 测试断言 |
| D19 | 性能与内存实测 + 可访问性 + 全站校对 | G8 性能断言 |
| D20 | 缓冲：只修不加 | 上线 |

---

## 19. 变更控制与 Git 规范

1. 任何偏离 §1 硬约束或 §4/§5 契约的改动，**先改本文档**，再改代码。
2. 涉及数据模型变更必须新增 migration，不得修改历史 migration。
3. 重大决策写入 `docs/adr/NNNN-标题.md`，格式：背景 / 选项 / 决定 / 后果。
4. 本文档自身版本化：头部 `version` 递增次版本号，§22 记录变更。

**提交规范（Conventional Commits 1.0.0）**

```
<type>(<scope>): <中文主题>

<正文：为什么这么改，取舍是什么>

<脚注：BREAKING CHANGE / Refs #12>
```

| type | 用途 |
| --- | --- |
| `feat` | 新功能 |
| `fix` | 修 bug |
| `docs` | 文档 / ADR |
| `refactor` | 不改行为的重构 |
| `perf` | 性能 |
| `style` | 纯格式 |
| `test` | 测试 |
| `build` | 依赖 / Dockerfile |
| `ci` | CI |
| `chore` | 杂项 |
| `revert` | 回滚 |

| scope | 模块 |
| --- | --- |
| `server` | Go 通用 |
| `api` | 接口契约 |
| `web` | Next 应用 |
| `admin` | 管理端 |
| `media` | 媒体管线 |
| `auth` | 鉴权 |
| `db` | 迁移 / 数据 |
| `deploy` | 部署 |
| `docs` | 文档 |
| `deps` | 依赖升级 |
| `ci` | 流水线 |

**硬规则**

1. 标题 ≤50 字符（中文按 2 计），不加句号。
2. 正文每行 ≤72，写动机与取舍，不写"修改了 xx 文件"。
3. 一次提交只碰一个模块；跨 3 个以上模块禁止。
4. 提交必须能通过 `make check`。
5. 分支：`feat/<scope>-<slug>`、`fix/<scope>-<slug>`、`docs/<slug>`、`chore/<slug>`；生命周期 ≤2 天。
6. 合并进 `main` 用 squash，squash 标题必须重写成上述格式；`main` 禁 force push，CI 必须绿。
7. 不提交 `.env`、`*.db`、`uploads/`、`.next/`、`node_modules/`。
8. 破坏性变更写 `BREAKING CHANGE:` 脚注，并同日更新本文档。
9. 发布打 tag：`v0.1.0` 起，SemVer，附注写明覆盖到哪个门禁。
10. **每次对话（一个任务批次）结束前必须提交**：作者信息沿用仓库已配置的 git 用户，不临时改 `git config`；同一批次一次提交。

---

## 20. 完成定义（DoD）

一个功能算"完成"，当且仅当：

- [ ] 接口与 §5 契约一致（路径、入参、出参、`code`；业务失败一律 HTTP 200，例外见 §5 清单）
- [ ] 表结构与 §4 一致，且通过 migration 创建
- [ ] §16 对应门禁断言全部通过
- [ ] 无 §17.1 禁止项；新增依赖已在 §17.2 登记
- [ ] 日志、审计、限流三件套已接
- [ ] 错误路径有明确 `code`，不泄露内部信息
- [ ] 前端在 375px 与 1440px 下均可用（管理页仅要求 1440px）
- [ ] 文档与 `docs/agent.md` 无冲突

---

## 21. 文档体系

| 文件 | 职责 | 维护时机 |
| --- | --- | --- |
| `docs/agent.md` | 唯一实现契约 | 每次契约变更 |
| `docs/standards/go.md` | Go 细则（命名、错误、测试模板） | 规范变化时 |
| `docs/standards/web.md` | 前端细则（组件、hooks、表单模板） | 规范变化时 |
| `docs/runbook.md` | 部署、证书、备份、恢复、排查 | 每次运维变更 |
| `docs/api.md` | 接口清单（OpenAPI 生成） | 每次接口变更 |
| `docs/adr/` | 重大决策记录 | 每次架构决策 |
| `docs/review-draft.md` | v1.1 评审过程留档 | 只读归档 |

---

## 22. 变更记录

| 版本 | 日期 | 变更 |
| --- | --- | --- |
| 1.17 | 2026-10-21 | **D20 功能可用性修复（编辑页分类/展示 + 全站排查）**：① 文章标签与系列此前**只有后端支持**——`post_tags` 只被删除、从未被写入，编辑器也没有任何入口，导致标签/系列的真实功能不可用（公开标签云、标签页、系列页、相关文章恒为空）。现在：`POST|PUT|PATCH /posts` 新增 `tagIds`（替换式、≤20、id 必须存在），新增/更新在同一事务里写 `post_tags`，`seriesId` 补存在性校验；管理端详情回显 `tagIds` + `cover`/`ogImage`，列表项回显 `tags`/`seriesName`；编辑器补齐**标签多选、系列下拉（含系列内序号）、封面/OG 图选择、置顶、允许评论、lang** 六类控件。② 编辑器预览改走 `DecorateContent`（预览 = 发布结果的 G2 断言此前不成立）；静态页正文同样补 srcset 与原图链接；`DecorateContent` 的匹配前缀改为跟着 `img_base_url` 走（不再硬编码 `uploads/`）。③ 存储页「清理孤儿」此前只删概览里截断的 200 条，界面却写"按全量处理"——cleanup 改为全量扫描。④ 点赞限流两个方向共用同一个桶（此前各建一个 = 实际 20 次/分钟，与 10 次/分钟的契约不符）。⑤ 前端对齐：`imgBaseUrl`≤200、友链 name≤100/description≤200、跳转 `toPath` 前缀校验、附件 fileName≤200/version≤50/sort≤99999；设置页只提交真正改动的字段（只调告警不再顶掉 `content_version`）；表单错误统一渲染 `data` 里的字段错误数组（`describeApiError`）；会话过期（`code 1000`）自动回登录页；管理端首页接上 `GET /stats/overview`（此前是纯静态占位页）；CSP `img-src` 放开 `https:`（友链头像与跨源 `imgBaseUrl`）；预览链接失效改按 `code 2006` 判定；评论页「来自《标题》」指向公网站点；非文章页补 PV 打点（"全站 PV/UV"此前只统计文章页） |
| 1.16 | 2026-10-21 | **D16 部署 + D17 备份/告警（M9b / M10）**：新增告警通道（§13 / §14）——事件 `job_failed` / `disk_high` / `cert_expiring`，渠道 webhook 或 Telegram（配置存 `settings.notify`，密钥永不回显，`null` 保持不变、`""` 清空），投递成功后 24h 静默、失败 15 分钟重试、条件恢复即清零，状态存 `settings.notify_state`；新增 `POST /notify/test`（按已保存配置立刻投递，不看 `enabled`）；新增每周任务 `integrity_check`（周一 04:30，`PRAGMA integrity_check` + 备份快照文件头校验）并加入 `POST /jobs` 手动白名单；新增配置 `tls.cert_file`（`BLOG_TLS_CERT_FILE`，容器只读看一眼宿主 certbot 证书的 `NotAfter`，留空跳过）；`PUT /settings` 只改 `notify` 时**不动** `content_version`；管理端设置页新增告警卡片与「发送测试」。**部署产物修正**：compose 显式 healthcheck + `stop_grace_period: 20s` + 可选 `/etc/letsencrypt` 只读挂载；nginx `/uploads/` 从 `alias` 改 `root`（1.24 上 `alias`+`try_files` 会拼错路径）、补 HSTS、去掉与 Go 重复的 `nosniff`；镜像装 `procps` 以支持自愈演练；§14 修正 s6/healthcheck 语义（进程退出由 s6 原地拉起，unhealthy 不会自动重启容器）；runbook 新增 §19 上线交接清单（含 G7 逐条命令与回执）、§20 告警、§21 空机恢复演练 |
| 1.15 | 2026-10-21 | **D15 SEO（M9a）**：Next 侧新增 `/sitemap.xml`（route handler，每请求取数，只含已发布文章 + 未删除静态页 + 非空标签/系列，`public, max-age=600`）、`/rss.xml`（RSS 2.0，最近 20 篇，`content:encoded` 全文）、`/robots.txt`（禁 `/admin`、`/preview`、`/api/`，带 `Sitemap:`）；每页 `generateMetadata` 给唯一 `title`/`description`/`canonical` 与 OG/Twitter 卡片（文章页 `og:type=article` + 封面 1600 变体），`/search`、`/preview/*`、`/admin*` 一律 `noindex`；文章页输出 `Article`、首页输出 `WebSite`+`SearchAction` JSON-LD（`<script>` 带 CSP nonce）；公开 API 新增 `GET /pages`（静态页清单，sitemap 用）；§3.1 目录、§11 SEO 行、§12 缓存表、§16 G7 断言与 `runbook` §18 同步 |
| 1.14 | 2026-10-20 | **连接建立后一律 HTTP 200，状态只存在 `code` 里**（gin-vue-admin 风格）：`response.FailWithDetailed` 固定 `200` 并保留 `Abort`；`data` 语义不变（无附加数据仍为 `null`，字段错误仍为 `[{field,message}]`）；`errs.HTTPStatus` / `AppError.HTTPStatus` 保留但不再决定状态码；前端 `fetcher.ts`、`lib/api/public.ts`（`isApiError` → `isApiCode`）、`middleware.ts`（按 `code 1007` 回 410 页面）、评论与点赞组件的错误分支全部改按 `code`；§5 明确**例外清单**（只有 HTTP 协议语义才保留真实状态码）：健康检查 `200/503`、静态文件直出 `200/404`、附件文件流 `200/206`、跳转 `301/302`、ETag `304`；**路由不存在**与 **Next 转发失败**也改成 `200` + `code 1002` / `code 1006`。登录改为**先判锁定再判限流**：连续 5 次错误口令后第 6 次直接 `code 2005`，不再被 `code 1005` 挡住。§2 拓扑与 §16 G1 断言同步（8640 上 `/api/admin/posts`、`/healthz` 改为 `200 + code 1002`） |
| 1.13 | 2026-10-20 | **图床下线，图片与附件全部本地自托管**：删除 Cloudreve/WebDAV 集成（`cloudreve.*` 配置、`gowebdav` 依赖、`media_archive` 任务、`media.archive` 审计、设置页图床块），C10 扩为"图片与附件都不入库、不上 CDN/对象存储"；原图**永久保留**（取消 30 天清理），「查看原图」改指同源 `/uploads/{key}/original.{ext}`；新增附件本地上传 `POST /attachments/upload`（multipart，落 `file.dir/{sha[:2]}/{sha256}{ext}`，按 sha256 去重）与附件安全黑名单（`415/2007`、`413/2008`），公开 `/attachments/{id}/go` 对本地附件直接下发（`octet-stream` + `Content-Disposition` + `nosniff` + Range）、对外链附件仍 302；新增 `GET /storage` 与 `POST /storage/cleanup`（占用统计、孤儿与缺失对账、孤儿清理）与 `DELETE /media/{id}/original`（删原图保变体）；`post_attachments` 增 `storage_key`/`mime`（migration 000003），`backup` 同时快照 `files/`，`cleanup` 不再动原图与附件 |
| 1.12 | 2026-10-19 | D14 内容管理与运维闭环（M8）：`tags`/`series`/`links`/`redirects`/`attachments` 管理端 CRUD（`GET|POST /x`、`PUT|DELETE /x/{id}`，分类列表带未删除文章数、删分类不删文章）；公开 `GET /attachments/{id}/go`（302 + `clicks+1`）与公开边缘按 `redirects.from_path` 精确 301/302（`hits+1`）；`GET|PUT /settings` 只允许改 `imgBaseUrl` / `siteTimezone` 并强制 `content_version+1`，图床口令永不返回；管理端 `GET /jobs` 与 `GET /audit-logs` 分页；web 新增标签/系列/友链/跳转/附件/任务/审计/设置页 |
| 1.11 | 2026-10-19 | D13 发布控制（M7）：新增 `POST /posts/{id}/schedule`（未来时间 → `scheduled` + 投递 `publish_scheduled` 任务，`unpublish`/回草稿撤任务）与 `POST /jobs`（手动投递 `cleanup`/`backup`/`link_check`）；`POST /posts/{id}/revisions/{rid}/restore` 回滚时**新写一条 revision**；预览链接改为一次性（首次访问写 `used_at`，之后或过期 = 410/2006），公开 `GET /preview/{token}` 为 `no-store`；§13 明确周期任务（backup 03:00 / cleanup 04:00 / link_check 周一 05:00，按站点时区，判据是当天是否已有同类型任务） |
| 1.10 | 2026-10-19 | D12 互动与统计（M6）：公开 `POST|DELETE /posts/{slug}/like`（幂等、10 次/分钟/visitor）、`GET /posts/{slug}/stats`（`no-store`，访客态）、`POST /events/view`（PV 明细 + 同 visitor 同文章 10 分钟去重，UV 按上海日期 `COUNT(DISTINCT visitor_hash)` 重算，同时写 `stats_daily` 文章行与 `post_id=0` 全站行）；管理端 `GET /stats/overview` 与 `GET /stats/daily`；访客标识改为 `X-Visitor-Id` 请求头（缺失按 `ip|ua` 兜底），公开 HTML 与公开读接口不 `Set-Cookie`；§11 允许 `{width=…}` 图片尺寸属性；编辑器预览改用 `PostContent`（行内 `$x$` 出公式）、支持拖拽上传与图片框拖拽改宽 |
| 1.9 | 2026-10-19 | D11 评论（M5）：公开 `GET|POST /posts/{slug}/comments`（嵌套固定两层、只出 approved、3 次/10 分钟/IP 限流）、管理端 `GET|PATCH /comments` 与 `POST /comments/bulk`；新评论一律 pending，`posts.comment_count` 只算 approved；`PublicPostDetail` 增 `allowComments`；§6 补评论状态机；管理端新增 `/admin/comments` 审核页 |
| 1.8 | 2026-10-19 | D9–D10 实现修订：公开端补 `GET /site`、`/tags`、`/tags/{slug}`、`/archive`、`/series`、`/series/{slug}`、`/links`、`/pages/{slug}`、`/posts/{slug}/related`；列表项与详情增 `cover`（四档 `variants` + `origUrl`）与 `tags`，详情增 `prev`/`next`；正文图片在读取时补 `srcset`/`sizes`/`width`/`height`/`loading`；`links.status` 枚举定为 `active`/`hidden`（与 000001 默认值一致）；410 由 `apps/web/middleware.ts` 返回（App Router 页面无法自定义状态码）；web 端新增 `PostImage`/`PostContent`/`PostCard`/`PostToc`/`Pagination`、blurhash 解码与 KaTeX 懒加载 |
| 1.7 | 2026-10-19 | D8 补充：`media_archive` 同时推四档变体（`cloudreve.archive_variants`，默认开）；新增 `cloudreve.public_base_url`，公开详情 `contentHtml` 在读取时给本站图片包上「查看原图」链接（`MediaItem.origUrl` 同步暴露）；明确图床「文件名规则」必须为 `{originname}` |
| 1.6 | 2026-10-19 | D8 实现修订：白名单补 `buckket/go-blurhash`；`media_process` / `media_archive` 接入 jobs 调度器（并发 1、退避 1m/5m/15m）；Cloudreve 归档配置（`cloudreve.*`，口令只走环境变量） |
| 1.5 | 2026-10-18 | D7 实现修订：管理端（8641）增加 `/uploads/*` 磁盘直出，供媒体库与编辑器预览（8640 仍由 nginx 直出）；白名单补 `golang.org/x/image`（缩放），明确 `gen2brain/webp` 用 `-tags nodynamic` 的 wazero 后端构建 |
| 1.4 | 2026-10-18 | D6 实现修订：新增错误码 1007 `ERROR_GONE`（归档与已删除文章返回 410，避免与 1002/404 混淆） |
| 1.0 | 2026-10-17 | 契约冻结：21 表、双 listener、12 天排期 |
| 1.3 | 2026-10-18 | §19 增加第 10 条：每次对话（任务批次）结束前必须提交，作者信息沿用仓库配置 |
| 1.2 | 2026-10-18 | D1–D3 实现修订：登录页落 `/admin/login`（8641 路由表不变，§3.1 去 `(auth)` 组、补 `(protected)` 与 `middleware.ts`）；`stats_daily.post_id=0` 明确为全站聚合；公开组补 `GET /system/ping`；CSRF 令牌改为派生（ADR 0003）；迁移驱动自写（ADR 0002）；初始管理员引导（ADR 0004）；G1 增加 8640 健康检查 404 断言；白名单补 `golang.org/x/sync` 与 shadcn 相关包 |
| 1.1 | 2026-10-18 | 评审修订：C3 端口语义、Go 全面改用 gin-vue-admin 骨架与 `{code,data,msg}` 信封、错误码数字化、前端改 dub 组织（`ui/`）、补 `pages`/`link_checks`/`cleanup`/预览链路/`used_at`、`settings` 增 `site_timezone`/`visitor_salt`、`media` 增 `orig_archived_at`、KaTeX 改客户端、SQLite 缓存按连接修正、CSP 改 nonce、原图保留 30 天、`admins.totp_secret` 按 C8 移除、排期 20 天、补 Git 规范与文档体系 |

---

> **本文件是唯一实现契约。** 与任何口头讨论、历史版本冲突时，以本文件为准；本文件自身需要修改时，走 §19 流程。
