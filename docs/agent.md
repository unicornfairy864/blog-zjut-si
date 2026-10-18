# AGENTS.md

> 项目：`zjut.si` 个人博客（Go + Next.js + SQLite 单容器）
> 路径：`docs/agent.md`（仓库根若需要 `AGENTS.md`，放一个只含链接的 3 行索引，不要复制正文；Windows 下不要用软链）
> 版本：**v1.5**（2026-10-18 D7 实现修订，见 §22）
> 状态：**设计契约**。任何偏离本文档的实现都必须先更新本文档
> 读者：执行编码的 AI Agent 与项目所有者（验收方）
> 阅读顺序：§1 硬红线 → §2 拓扑 → §3 结构 → §4 数据模型 → §5 接口 → §7/§8 规范 → §16 验收

---

## 0. 一句话定义

一台 4C/4G 海外 VPS 上的**单人技术博客**：Next.js 负责所有页面渲染，Go 负责全部业务逻辑与 API，SQLite 存数据，浏览器与宿主 nginx 承担全部缓存职责，管理端经 SSH 隧道访问、永不上公网。

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
| C10 | **图片不入库、不上 CDN** | 磁盘存文件，DB 只存 key 与元数据 |

---

## 2. 系统拓扑

```
公网 :80 ──▶ 宿主 nginx ──▶ 127.0.0.1:8640 ──▶ Go 公开边缘
                                                 ├ /api/*        → Go 本地
                                                 ├ /admin/*      → 404
                                                 ├ /uploads/*    → 404（nginx 直出）
                                                 └ 其余          → Next :3000

本地 :8641 ─▶ ssh -L 8641:127.0.0.1:8641 ─▶ Go 管理边缘
                                              ├ /api/admin/*   → Go 本地
                                              ├ /api/internal/*→ Go 本地
                                              ├ /healthz|/readyz→ Go 本地
                                              ├ /admin*        → Next :3000（含 /admin/login）
                                              ├ /_next/*       → Next :3000（+ /favicon.ico）
                                              └ 其余           → 404
```

管理端 8641 额外直出 `/uploads/*`（仅本地媒体库与编辑器预览用）；8640 上 `/uploads` 仍为 404，由宿主 nginx 直出。

| 组件 | 职责 | 明确不做 |
| --- | --- | --- |
| 宿主 nginx | TLS/80 入口、静态图片直出、限流、防盗链 | 不代理缓存 HTML，不做鉴权 |
| Go 进程 | 双 listener 边缘路由、全部 API、静态文件校验、任务调度 | 不渲染 HTML 页面 |
| Next 进程 | 全部页面渲染（公开 SSR + 管理 CSR + 预览页） | 不直连数据库、不写业务规则 |
| SQLite | 唯一持久化存储 | 不存图片二进制 |
| Cloudreve | 原图归档 + 文件分享页（WebDAV） | 不作为前台图片源 |

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
│     ├─ app/api/             # 仅 og 图 / rss 等 Next 自身端点
│     ├─ ui/{shadcn,layout,post,admin,shared}
│     ├─ lib/{api,hooks,zod,constants,swr}
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
| `media` | id, sha256, storage_driver, storage_key, original_name, mime, ext, bytes, width, height, pixels, variants_json, blurhash, alt, status, error, orig_archived, orig_archived_at INT NULL, created_at, updated_at | UNIQUE(sha256); INDEX(status)；`orig_archived_at` 用于 30 天后清理本地原图 |
| `post_attachments` | id, post_id, title, share_url, file_name, bytes, sha256, version, sort, clicks, created_at | INDEX(post_id) |
| `comments` | id, post_id, parent_id, root_id, author_name, author_email, email_hash, author_url, content_md, content_html, status, ip_hash, ua, created_at, updated_at | INDEX(post_id,status,created_at); INDEX(status,created_at) |
| `likes` | post_id, visitor_hash, created_at | PK(post_id,visitor_hash) |
| `page_views` | id, post_id NULL, path, visitor_hash, referrer_host, created_at | INDEX(post_id,created_at)；明细保留 90 天 |
| `stats_daily` | date TEXT, post_id INT, pv INT, uv INT | PK(date,post_id)；date 为上海日期；`post_id=0` 表示全站聚合行（PK 不允许 NULL） |
| `jobs` | id, type, payload, run_at, status, attempts, max_attempts, last_error, locked_at, created_at | INDEX(status,run_at) |
| `settings` | key TEXT PK, value_json, updated_at | 必含 `content_version`、`img_base_url`、`site_timezone`、`visitor_salt` |
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
jobs.status     : pending | running | done | failed
jobs.type       : publish_scheduled | media_process | media_archive | link_check | backup | cleanup
storage_driver  : local | cloudreve
```

---

## 5. 接口契约

**通用（gin-vue-admin 信封）**

| 项 | 规则 |
| --- | --- |
| 前缀 | `/api`（不引入 `/api/v1` 前缀，URL 保持简洁） |
| 成功 | HTTP 200 + `{"code":0,"data":<数据>,"msg":"操作成功"}` |
| 失败 | 对应 HTTP 状态 + `{"code":<数字码>,"data":<details\|null>,"msg":"<人话>"}` |
| 分页入参 | `page`(≥1) `pageSize`(1–50, 默认 20) `keyword`(可选) |
| 分页出参 | `data:{"list":[],"total":0,"page":1,"pageSize":20}` |
| 时间 | 全部出参为 Unix 秒整数 |
| 缓存头 | 公开读接口必须带强 `ETag`（响应体哈希），支持 `If-None-Match → 304` |
| CSRF | 所有 `/api/admin/*` 写操作必须校验 `X-CSRF-Token`；token 由 `GET /auth/me` 下发，值由会话令牌哈希派生（ADR 0003），会话期内稳定 |

**错误码（数字码 + 常量名，禁止裸数字）**

| code | 常量 | HTTP | 语义 |
| --- | --- | --- | --- |
| 0 | `SUCCESS` | 200 | 成功 |
| 7 | `ERROR` | 500 | 未分类失败 |
| 1000 | `ERROR_AUTH` | 401 | 未登录 / 会话失效 |
| 1001 | `ERROR_FORBIDDEN` | 403 | 越权 / CSRF 失败 |
| 1002 | `ERROR_NOT_FOUND` | 404 | 资源不存在 |
| 1003 | `ERROR_VALIDATION` | 400 | 参数校验失败，`data` 带字段错误数组 |
| 1004 | `ERROR_CONFLICT` | 409 | 通用冲突 |
| 1005 | `ERROR_RATE_LIMITED` | 429 | 限流 |
| 1006 | `ERROR_INTERNAL` | 500 | 内部错误（不暴露堆栈） |
| 1007 | `ERROR_GONE` | 410 | 已归档或已删除，资源不再提供 |
| 2001 | `ERROR_SLUG_CONFLICT` | 409 | slug 重复 |
| 2002 | `ERROR_MEDIA_IN_USE` | 409 | 媒体被引用，`data` 带引用清单 |
| 2003 | `ERROR_IMAGE_TOO_LARGE` | 413 | 像素或字节超限 |
| 2004 | `ERROR_SLUG_IMMUTABLE` | 409 | 已发布文章改 slug 未写 redirect |
| 2005 | `ERROR_LOGIN_LOCKED` | 423 | 登录锁定 30 分钟 |
| 2006 | `ERROR_PREVIEW_EXPIRED` | 410 | 预览链接过期或已用 |

**公开组（8640）**

`GET /site` · `GET /posts` · `GET /posts/{slug}` · `GET /posts/{slug}/related` · `GET /posts/{slug}/comments` · `POST /posts/{slug}/comments` · `POST /posts/{slug}/like` · `GET /tags` · `GET /tags/{slug}` · `GET /archive` · `GET /series` · `GET /series/{slug}` · `GET /search?q` · `GET /links` · `GET /pages/{slug}` · `POST /events/view` · `GET /attachments/{id}/go` · `GET /preview/{token}`（`no-store`） · `GET /system/ping`（骨架探活）

**管理组（8641）**

`POST /auth/login` · `POST /auth/logout` · `GET /auth/me` · `GET|POST /posts` · `GET|PUT|PATCH|DELETE /posts/{id}` · `POST /posts/{id}/publish` · `POST /posts/{id}/unpublish` · `GET /posts/{id}/revisions` · `POST /posts/{id}/revisions/{rid}/restore` · `POST /posts/{id}/preview-link` · `DELETE /preview-links/{token}` · `POST /posts/preview` · `GET|POST /media` · `DELETE /media/{id}` · `POST /media/{id}/retry` · `GET|PATCH /comments` · `POST /comments/bulk` · `GET|PUT /settings` · `GET|PUT /pages/{slug}` · `GET|POST|PUT|DELETE /{tags|series|links|redirects|attachments}` · `GET /stats/overview` · `GET /stats/daily?from&to` · `GET /jobs` · `POST /jobs/{id}/retry` · `GET /audit-logs`

**内部组（8641，仅容器内/隧道）**

`POST /api/internal/revalidate` · `GET /api/internal/health` · `GET /healthz` · `GET /readyz`（DB 可查 + 磁盘 <85% + uploads 可写 + 队列积压 <100）

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
- 已发布文章 `slug` 不可变；确需修改时必须同时写 `redirects`，否则 `2004`
- 每次 publish：`revision+1`、写 `post_revisions`、重算 `content_html/content_text/word_count/reading_time`
- `archived` 与已删除文章对外返回 **410**，不出现在列表、搜索、sitemap、RSS

**媒体**

```
uploading(内存态) ─落盘校验─▶ processing ─成功─▶ ready
                                └─失败(重试2次)─▶ failed ─retry─▶ processing
ready: refs==0 → 物理删除；refs>0 → 409 (2002)
```

- worker 全局并发 **固定为 1**
- `pixels > 50_000_000` → `failed / 2003`
- 引用检查三处：`cover_media_id`、`og_media_id`、`content_md` 含 `storage_key`

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
| 响应 | 只用 `model/common/response`：`OkWithData` / `FailWithDetailed`；HTTP 状态码由 `code` 查表得出（见 §5） |
| 中间件顺序 | request_id → panic 恢复 → 访问日志 → 限流 → 鉴权 → CSRF → handler |
| 日志 | `slog` JSON 到 stdout，字段 `ts,level,request_id,method,path,status,dur_ms,ip_hash`；禁止打印密码、邮箱、cookie |
| 错误处理 | 业务错误用 `errs.AppError{Code,HTTPStatus,Message}`；禁止把 `err.Error()` 直接返回前端 |
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
| Markdown 编辑 | 受控 textarea + 防抖自动保存（30s 或失焦）+ `POST /posts/preview` 服务端预览 |
| 图片 | 不使用 `next/image`，统一 `<PostImage>` 输出 480/960/1600/2400 + blurhash 占位 |
| 数学 | **客户端 KaTeX**：渲染管线输出 `<span class="math" data-tex="...">`，`<MathRender>` 懒加载替换；无公式页面不加载 |
| 命名 | 文件 kebab-case、组件 PascalCase、类型 `XxxProps`；不用 `React.FC` |
| 导出 | 只有 Next 约定文件用 default export，其余具名导出 |
| 错误 | `lib/api/fetcher.ts` 统一抛 `ApiError{code,message,status}`；`error.tsx` 兜底，不暴露堆栈 |
| 登录态 | `app/(admin)/admin/(protected)/layout.tsx` 先 `GET /auth/me`，401 直接跳 `/admin/login`，避免闪烁 |
| 可访问性 | 语义标签、焦点可见、图片 alt 必填、对比度 AA、`eslint-plugin-jsx-a11y` |
| 质量门 | `pnpm lint && pnpm typecheck && pnpm test && pnpm build` 全绿才允许提交 |
| 禁止 | Server Actions（会绕过 `lib/api`）、组件内拼 API 路径、`app/api` 透传业务数据、AntD/MUI/Redux |

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
| EXIF | 入库前按 Orientation 摆正并剥离全部 EXIF（含 GPS），再生成变体与归档 |
| SQL | 全参数化；FTS5 查询字符串必须转义 |
| 越权 | 8640 层直接 404 掉 `/admin`、`/api/admin`，不依赖登录态判断 |
| 限流 | 登录 5/min（按用户名）；评论 3/10min/IP；公开 API 20 r/s；点赞 10/min/visitor |
| 响应头 | `X-Content-Type-Options: nosniff`、`Referrer-Policy: strict-origin-when-cross-origin`、`Permissions-Policy` 最小化 |
| CSP | `script-src 'self' 'nonce-{n}' 'strict-dynamic'`，nonce 由 `middleware.ts` 注入。**禁止 `unsafe-inline`**，否则 Next RSC 水合会失败；`style-src 'self' 'unsafe-inline'` 可保留（Tailwind 运行时无内联样式，但 shadcn 组件有） |
| 敏感信息 | `.env` 不入库不入镜；`admin_email` 不在任何公开接口出现 |
| 审计 | 所有管理端写操作写 `audit_logs`（action + target + diff 摘要） |

---

## 10. 媒体管线规范

**上传流程**

1. 浏览器端预压缩（长边 2560、WebP q0.85）→ 失败则原图直传
2. Go 流式落盘临时文件 → 魔数/大小/像素校验 → 摆正并剥离 EXIF
3. `sha256` 去重：命中则直接返回已有 `media`
4. 入库 `processing` → 投递 `media_process`
5. worker（并发 1）：生成 480/960/1600/2400 四档 WebP（q82）+ 20px blurhash；**变体不超过原图尺寸，不放大**
6. `ready` → 投递 `media_archive`，WebDAV 推原图到 Cloudreve `/blog/{sha256}.{ext}`
7. 归档成功 → `orig_archived=1`，**本地原图保留 30 天**；`cleanup` 任务在 30 天后或磁盘 >85% 时删除

**URL 规则（唯一允许的形态）**

```
{img_base_url}/{sha[0:2]}/{sha}/{variant}.webp
例：http://blog.zjut.si/uploads/ab/abcdef.../1600.webp
```

- 禁止查询参数做版本（`?v=` 一律不允许）
- 禁止把 Cloudreve 分享链接写进正文
- `media` 表永不存 URL，只存 `storage_key` + `variants_json`

**删除**：有引用返回 `409 / 2002` + 引用清单；无引用才删文件与记录。

---

## 11. 内容与渲染规范

| 项 | 规定 |
| --- | --- |
| 存储格式 | Markdown（GFM）+ frontmatter 语义字段存在表列，不存 YAML |
| 渲染 | 服务端 goldmark + chroma + bluemonday；`content_html` 落库，读路径不重复渲染 |
| 数学 | goldmark 阶段只把 TeX 包成 `<span class="math" data-tex="{escaped}">`，**不在服务端渲染 KaTeX**；前端 `<MathRender>` 动态 import `katex` 并替换 innerHTML；无 JS 时显示 TeX 原文 |
| 代码块 | 行号、复制按钮、文件名标签、语言高亮（diff 支持） |
| TOC | 从 `h2/h3` 提取，生成锚点，正文标题自动加 `id` |
| 摘要 | 未填 `excerpt` 时取正文前 160 字纯文本 |
| 阅读时长 | `ceil(word_count / 300)` 分钟；`word_count` = 中文字符数 + 英文单词数（中文按字符计） |
| slug | 手填优先；未填时取标题拼音前 40 字符，失败用 `post-{YYYYMMDD}-{HHmm}`；发布后不可变 |
| 上一篇/下一篇 | 按 `published_at` 相邻，仅同状态 |
| 时间展示 | 站点时区 `Asia/Shanghai`；存储 UTC |
| SEO | 每页唯一 `title/description/canonical`；文章页输出 `Article` JSON-LD；`sitemap.xml` 仅含 published；`rss.xml` 全文；`robots.txt` 禁 `/admin`、`/preview` |
| 草稿隔离 | 未发布内容在任何公开接口、sitemap、RSS、搜索中均不可见 |
| 410 | `archived` 与已删除文章返回 410，不返回 404 |

---

## 12. 缓存与静态资源策略

| 资源 | 头 | 说明 |
| --- | --- | --- |
| `/_next/static/*` | `public, max-age=31536000, immutable` | 文件名含哈希 |
| `/uploads/*` | `public, max-age=31536000, immutable` | 路径含内容哈希，nginx 直出 |
| KaTeX 字体与 CSS | `public, max-age=31536000, immutable` | 自托管，不打 CDN |
| HTML | `Cache-Control: no-cache` + 强 `ETag` | 每次回源校验，304 省流量 |
| 公开 API | `Cache-Control: no-cache` + `ETag` | ETag 取响应体哈希 |
| 管理端全部 | `no-store` | 禁止任何中间层缓存 |

**明确不做**：Redis、ISR、`revalidate` 定时、`proxy_cache`、CDN、Service Worker。
**预留**：nginx 侧保留 `proxy_cache_path` 配置位（默认注释），后期开启不改 upstream。

---

## 13. 异步任务与定时

| 任务 | 触发 | 行为 |
| --- | --- | --- |
| `publish_scheduled` | 每分钟 | `scheduled` 且 `published_at<=now` → 发布 |
| `media_process` | 上传后 | 生成变体 + blurhash |
| `media_archive` | 变体就绪 | WebDAV 推原图到 Cloudreve |
| `link_check` | 每周一 | HEAD 检查外链与正文图片，写 `link_checks` |
| `backup` | 每日 03:00（Asia/Shanghai） | `VACUUM INTO` + uploads 快照 |
| `cleanup` | 每日 04:00（Asia/Shanghai） | 物理删软删 >30 天的文章、删 `page_views` >90 天、裁剪 revision 到 50、删过期 session/preview_token/login_attempts、删归档满 30 天的本地原图 |

调度器：单 goroutine ticker，每 60s 扫描；取任务前 `UPDATE ... SET status='running', locked_at=? WHERE id=? AND status='pending'` 保证互斥。
**任一任务进入 `failed` 或磁盘 >85%，必须触发一次通知**（邮件 / Telegram / webhook 任选其一，配置存 `settings`）。

---

## 14. 运维与部署

| 项 | 规定 |
| --- | --- |
| 基础镜像 | Ubuntu 24.04 宿主；容器内 debian-slim + 静态二进制 + Node 运行时 |
| 进程托管 | s6-overlay：`go` 与 `next` 两个 service，任一退出 → 容器重启 |
| 健康检查 | `HEALTHCHECK` 打 `127.0.0.1:8641/readyz`，间隔 30s，3 次失败重启 |
| 资源 | `mem_limit: 1.5g`、`restart: always`、`logging: json-file, max-size=10m, max-file=3` |
| 卷 | `/data/blog/db`（SQLite）、`/data/blog/uploads`（图片） |
| nginx | 80 → `127.0.0.1:8640`；`client_max_body_size 32m`；`proxy_request_buffering off`；`/uploads/` 直出磁盘；`valid_referers` 防盗链 |
| TLS | certbot 自动签发 + 自动续期（systemd timer），`renew --dry-run` 每月跑一次；443 强制跳转 |
| 防火墙 | `ufw` 仅开 22/80/443；8640/8641 不对公网开放（由 compose 的 127.0.0.1 绑定兜底） |
| SSH | 禁 root 密码登录，仅密钥；可选 fail2ban |
| 隧道 | `ssh -L 8641:127.0.0.1:8641 user@host`，浏览器访问 `http://localhost:8641` |
| 备份 | `VACUUM INTO /backup/blog-YYYYMMDD.db` + uploads `rsync --link-dest`（含原图，保留 90 天）；库保留 30 天；每周 `PRAGMA integrity_check` |
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
| G1 骨架与鉴权 | D1–D3 | 空库全量迁移成功（23 表 + FTS 触发器）；`curl 127.0.0.1:8640/api/admin/posts` = 404；`curl 127.0.0.1:8640/healthz` = 404；`curl 公网IP:8641` 连接拒绝；5 次错误锁定 30min；会话 7 天过期；登出即失效；未登录 = 401/1000；无 CSRF = 403/1001；响应体为 `{code,data,msg}` |
| G2 文章与渲染 | D4–D6 | 草稿不可见；slug 冲突 = 409/2001；`<script>` 被剥离；自动保存不产生多余 revision；发布后立即可见；编辑器预览 = 发布结果；2 字中文词可搜到；归档 = 410 |
| G3 媒体 | D7–D8 | 31MB 拒绝 = 413；50MP 拒绝 = 2003；同图去重；引用删除 = 409/2002 + 清单；四档变体可访问；转码峰值 <300MB；GPS EXIF 不出现在输出图；HEIC 被拒并有提示；归档 30 天内本地原图仍在 |
| G4 公开端 | D9–D10 | 375px 无横向滚动；分页 + 标签组合正确；404 无堆栈；`srcset` 生效；系列/友链/关于页可访问 |
| G5 互动 | D11–D12 | 评论 3/10min 限流；嵌套 ≤2 层；spam 不进公开列表；点赞幂等；同 visitor 10 分钟内 PV 只 +1；`stats_daily` 按上海时区切日 |
| G6 发布控制 | D13 | 定时发布到点生效（误差 <90s）；回滚生成新 revision；预览链接 24h 后 = 410/2006；`cleanup` 能删掉构造的过期数据 |
| G7 SEO 与部署 | D15–D16 | sitemap 无草稿；RSS 校验通过；JSON-LD 无错误；canonical 唯一；`kill -9` 后 30s 自愈；隧道可登录；nginx 静态图片可访问；证书续期 dry-run 通过 |
| G8 备份、测试与性能 | D17–D19 | `VACUUM INTO` 可还原；空机恢复演练成功；单测/接口/E2E 全绿；service 覆盖率 ≥70%；TTFB <300ms；LCP <2.5s；内存不超 §15 |

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
| 把 Cloudreve 分享链接写进正文 | 链接会变，批量破图 |
| 图片 URL 加 `?v=时间戳` | 摧毁浏览器缓存 |
| 公开页面 `Set-Cookie` | 无意义且污染缓存层 |
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
| Go | gin, gorm, glebarez/sqlite, golang-migrate, viper, goldmark(+GFM/attributes), chroma/v2, bluemonday, google/uuid, golang.org/x/crypto(argon2), golang.org/x/sync(errgroup), gen2brain/webp（wazero，构建加 `-tags nodynamic`）, golang.org/x/image(draw), gowebdav, golang.org/x/time |
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
| D8 | M3b 四档变体 + blurhash + WebDAV 归档 | G3 全过 |
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

- [ ] 接口与 §5 契约一致（路径、入参、出参、`code`、HTTP 状态）
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
| 1.5 | 2026-10-18 | D7 实现修订：管理端（8641）增加 `/uploads/*` 磁盘直出，供媒体库与编辑器预览（8640 仍由 nginx 直出）；白名单补 `golang.org/x/image`（缩放），明确 `gen2brain/webp` 用 `-tags nodynamic` 的 wazero 后端构建 |
| 1.4 | 2026-10-18 | D6 实现修订：新增错误码 1007 `ERROR_GONE`（归档与已删除文章返回 410，避免与 1002/404 混淆） |
| 1.0 | 2026-10-17 | 契约冻结：21 表、双 listener、12 天排期 |
| 1.3 | 2026-10-18 | §19 增加第 10 条：每次对话（任务批次）结束前必须提交，作者信息沿用仓库配置 |
| 1.2 | 2026-10-18 | D1–D3 实现修订：登录页落 `/admin/login`（8641 路由表不变，§3.1 去 `(auth)` 组、补 `(protected)` 与 `middleware.ts`）；`stats_daily.post_id=0` 明确为全站聚合；公开组补 `GET /system/ping`；CSRF 令牌改为派生（ADR 0003）；迁移驱动自写（ADR 0002）；初始管理员引导（ADR 0004）；G1 增加 8640 健康检查 404 断言；白名单补 `golang.org/x/sync` 与 shadcn 相关包 |
| 1.1 | 2026-10-18 | 评审修订：C3 端口语义、Go 全面改用 gin-vue-admin 骨架与 `{code,data,msg}` 信封、错误码数字化、前端改 dub 组织（`ui/`）、补 `pages`/`link_checks`/`cleanup`/预览链路/`used_at`、`settings` 增 `site_timezone`/`visitor_salt`、`media` 增 `orig_archived_at`、KaTeX 改客户端、SQLite 缓存按连接修正、CSP 改 nonce、原图保留 30 天、`admins.totp_secret` 按 C8 移除、排期 20 天、补 Git 规范与文档体系 |

---

> **本文件是唯一实现契约。** 与任何口头讨论、历史版本冲突时，以本文件为准；本文件自身需要修改时，走 §19 流程。
