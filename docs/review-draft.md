# 设计评审草案（DRAFT v0 · 未生效）

> 对象：`docs/agent.md`（现行契约，471 行，自称"已冻结"）
> 方式：本文件是**评审意见 + 修订建议 + 三份规范草案**。**我没有动 `agent.md`**。
> 生效条件：你逐条批准后，我按 §8 的清单一次性合入 `agent.md`，并在文末追加"变更记录"。
> 驳回方式：直接回 "A5 不同意 / 按 C 方案" 即可，编号是稳定的。

---

## 0. 我读到的项目（复述，确认我理解没跑偏）

单人技术博客，一台 4C/4G 海外 VPS：

- **两个入口，两个 listener**：`8640` 公开（宿主 nginx 反代），`8641` 管理（只走 SSH 隧道）。管理端在 8640 上必须 404，`8641` 永不出公网。
- **职责切分**：Next.js 只渲染页面（公开页 SSR、管理页 CSR），Go 承担全部业务/API/静态校验/任务，SQLite 是唯一数据库，没有 Redis/CDN/ISR。
- **数据**：21 张表，Unix 秒、软删 30 天、图片不入库；Markdown → 服务端渲染成 HTML 落库。
- **媒体**：上传去重 → 四档 WebP + blurhash → 原图推 Cloudreve（WebDAV）后删本地。
- **运营**：s6-overlay 双进程、健康检查、`VACUUM INTO` 备份、迁移旧站 URL。
- **节奏**：12 天 × 2h。

**总体判断**：这是一份质量很高的契约，边界清楚、反模式清单到位。问题不在"想得不清楚"，而在三类：
1. **内部不一致**（有表无接口、有任务无表、有接口无页面）；
2. **与真实运行时行为冲突**（Docker 端口、SQLite per-connection 配置、Next CSP、FTS5 中文）；
3. **规范层缺失**（代码结构、前端目录、Git 流程、CI、迁移工具），以及**范围与工期不匹配**。

---

## 1. 需要你拍板的事（摘要，详见 §5 与文末问题）

| 编号 | 决策 | 我的建议 |
| --- | --- | --- |
| D-A | Go 结构"仿照 go-vue-admin"具体含义 | 采用 **gin-vue-admin 目录骨架**，但**不采用**它的 `{code,data,msg}` 信封与"无 repo 层"写法 |
| D-B | 前端参照仓库 | **dubinc/dub**（Next App Router + Tailwind + shadcn + SWR + Zod，同栈） |
| D-C | Git 流程 | **trunk-based + Conventional Commits + 中文主题** |
| D-D | v1 范围 | 把 §18 的"二期"功能从 v1 契约里**移出**（表可以留，接口不承诺） |
| D-E | 旧博客现状 | 需要你补充：平台、文章量、URL 形态、评论是否迁移 |

> **2026-10-18 已定**：D-A = **完全照搬 GVA**；D-B = **dub + `ui/`**；D-C = **分支 + squash**；D-D = **全量做**；D-E = **全新站，无旧内容**。
> 由此产生的最终条款见 **§9**；§5.1 / §5.2 保留为初版建议，与 §9 冲突处一律以 §9 为准。

---

## 2. 文档内部硬冲突（不改就会在实现时二选一）

> 标记：**P0** = 现在不解决，写代码时必然返工；**P1** = 上线前必须解决。

### A1 · P0 · `link_checks` 表不存在
- 位置：§13 `link_check` 任务 ⇒ "HEAD 检查外链与正文图片，写 `link_checks`"；§4 声明"21 张表"且无此表。
- 后果：周任务无处落库，枚举里却有 `link_check`。
- 建议：补表 `link_checks(id, post_id, url, status_code, ok, checked_at, error)`，21 → 22（若同时补 `pages` 见 A7，则 23）；并把 §4 标题改成"对应 migration 版本 N"而不是硬编码张数。

### A2 · P0 · 规定会"到期清理"的数据，却没有任何清理任务
- 位置：§4 写了 回收站 30 天、`page_views` 明细 90 天、`post_revisions` 只留 50 条、`preview_tokens` 24h、`sessions` 7 天；§13 的 `jobs.type` 枚举与任务表里**没有任何 cleanup 任务**。
- 后果：这些数据实际永不删除，磁盘只增不减；§15 的磁盘预算和 §14 的保留 30 天备份对不上。
- 建议：加 `jobs.type = cleanup`（或 `housekeeping`），每日 04:00 执行：物理删软删 >30 天的 post、删 `page_views` >90 天、裁剪 revision 到 50、删过期 session/preview_token、删 `login_attempts` 过期行。并补一条验收断言。

### A3 · P0 · 预览链路断在中间
- 位置：§5 管理组有 `POST /posts/{id}/preview-link`、`DELETE /preview-links/{token}`；公开组**没有** `GET /preview/{token}`；§3 公开路由没有 `/preview/*`；但 §12 又如 `robots.txt` 要禁 `/preview`。
- 建议：补 `GET /api/preview/{token}`（校验 + 一次性标记 + 返回草稿 HTML，`Cache-Control: no-store`）与页面 `app/(public)/preview/[token]/page.tsx`；路由表补 `/preview/[token]`。

### A4 · P0 · `preview_tokens` 缺"已使用"字段
- 位置：§4 表只有 `token_hash, post_id, expires_at, created_at`，但注释写"可一次使用"。
- 建议：加 `used_at INT NULL`，或明确"不做一次性，24h 内可重复访问"（我倾向后者：预览链接自己用，限制次数没有收益，反而误伤）。

### A5 · P0 · 编辑器"服务端预览接口"不存在
- 位置：§8 表单规范写"服务端预览接口"；§5 管理组没有任何渲染预览端点。
- 建议：补 `POST /api/admin/posts/preview`（入参 markdown，出参 HTML），或约定前端只做客户端渲染（那就得引 markdown 运行时，与"公开页 SSR"不冲突但增加 JS）。我倾向服务端预览，保证"编辑器所见 = 发布后所得"。

### A6 · P1 · `/links` 只有后台没有前台
- 位置：§4 有 `links` 表，§5 管理组有 links CRUD；§3 公开路由与 §5 公开组都没有友链页/接口。
- 建议：补 `GET /api/links` + `app/(public)/links/page.tsx`，或明确"v1 不做友链页，表与 CRUD 推迟"。

### A7 · P1 · `/about` 的内容没有数据来源；标签/系列列表接口缺失
- 位置：§3 有 `/about`，但 §4 无 `pages` 表，`settings` 也未约定 about 正文；§5 公开组只有 `GET /series/{slug}`，没有 `GET /series`；标签只能靠 `GET /posts?tag=`（未写明）。
- 建议三选一并写进契约：
  - (a) `about` 作为 `pages(slug, title, content_md, content_html, ...)` 表的一条记录（推荐，未来可加"关于本站/留言板"）；
  - (b) 放 `settings` 的 JSON 里；
  - (c) 仓库内 MDX 静态页（改内容要发版）。
- 同时补 `GET /series`、并明确 `GET /posts?tag={slug}&series={slug}` 的过滤参数。

### A8 · P0 · 契约文件本身不可追踪
- 位置：§3 树里写根目录 `AGENTS.md`，§19/§20 也引用 `AGENTS.md`；实际文件是 `docs/agent.md`；而 `.gitignore` 内容只有一行 `docs` ⇒ **整个 docs 被忽略**。
- 后果：契约、ADR 都不进版本库，"先改文档再改代码"无法审计；换机器/协作者直接丢失。
- 建议：`.gitignore` 改成只忽略 `docs/private/`（或 `*.local.md`），把 `docs/agent.md`、`docs/adr/`、本草案纳入版本控制；文件统一为 `docs/agent.md`，§3 树同步改。

### A9 · P0 · C3"端口只绑 127.0.0.1"与 Docker 端口映射冲突
- 位置：C3 与 §1 表格；§14 `nginx 80 → 127.0.0.1:8640`。
- 事实：`-p 127.0.0.1:8640:8640` 只约束**宿主机**监听地址；数据包进入容器时目的地址是容器 `eth0` 的 IP，容器内进程**必须**监听 `0.0.0.0:8640`（或至少容器 IP），否则连接被拒。
- 连带：容器看到的来源 IP 是 docker 网关（如 `172.17.0.1`），**不是** `127.0.0.1`，于是 §7 的 `TrustedProxies=["127.0.0.1"]` 失效，`X-Forwarded-For` 不被采信，限流按 IP 全部落到网关上。
- 建议改写成两条：
  1. **宿主侧**：`compose.yaml` 只发布 `127.0.0.1:8640` 与 `127.0.0.1:8641`；`0.0.0.0` 在**宿主**上禁止。
  2. **容器内**：Go 监听 `:8640`/`:8641`（即 `0.0.0.0`），由 Docker 的宿主绑定兜底；`TrustedProxies` 配置为 docker 网络 CIDR（如 `172.16.0.0/12`）+ `127.0.0.1`。
  - 若坚持容器内也只绑回环，唯一可行方案是 `network_mode: host`，但那样 8641 的安全边界就只剩 ufw 了，我不推荐。

### A10 · P1 · 内部组/健康检查挂在哪个 listener 没说
- 位置：§5 内部组"仅 127.0.0.1"，但没说是 8640 还是 8641；`/healthz` `/readyz` 也没标端口。
- 风险：若挂在 8640，`/readyz` 会把磁盘占用、队列积压、DB 状态暴露给公网。
- 建议：`/healthz` `/readyz` 只注册在 **8641**（隧道可达，docker HEALTHCHECK 从容器内打 `127.0.0.1:8641` 也可），8640 上只保留一个固定返回 `{"status":"ok"}` 的极简端点或干脆不暴露。

### A11 · P0 · "二期功能"却写进了 v1 冻结契约
- 位置：§18 二期 = 评论 → 搜索 → 统计 → 点赞 → 相关文章 → 定时发布 → 版本回滚 → 预览链接 → 系列 → 附件；但 §4 已经把这些表全部建好，§5 把接口全部写进契约，§16 还要求 D3–D4 就通过文章相关断言。
- 建议：**分层表达**——表结构可以一次性建全（migration 成本低），但 §5 标注 `V1` / `V2`，§18 的 12 天只承诺 `V1`，验收门禁按 `V1` 走。否则 D1–D12 根本做不完（见 §6.4）。

### A12 · P1 · `media.status` 缺 `uploading`
- 位置：§4 枚举 `processing|ready|failed`；§6 状态图第一行是 `uploading ─落盘校验─▶ processing`。
- 建议：说明"`uploading` 是内存态，不落库"（推荐，改一句话），或枚举加 `uploading`。

### A13 · P1 · 每日统计的时区没定义
- 位置：§4 全局约定"时间 `INTEGER` Unix 秒 UTC"，但 `stats_daily.date TEXT` 是按天聚合；§11 又说展示用 `Asia/Shanghai`。
- 风险：UTC 切日 vs 上海切日差 8 小时，凌晨 0–8 点的 PV 会算到前一天。
- 建议：明确 `stats_daily.date` 为 **`Asia/Shanghai` 的 `YYYY-MM-DD`**，并在 `settings` 里存 `site_timezone`，清理/聚合任务一律按站点时区。

### A14 · P0 · 原图归档后立刻删本地
- 位置：§10 第 7 步"归档成功 → 本地删除原图，`orig_archived=1`"；§14 备份只备份 `/data/blog/uploads`。
- 后果：
  1. 想新增一档尺寸（比如 3200 或 AVIF）时，本地没有原图，得先能从 Cloudreve 拉回来；
  2. 备份里没有原图，Cloudreve 挂了就永久丢图；
  3. "下载原图"只能跳 Cloudreve 分享页，与 §11"禁止把 Cloudreve 链接写进正文"的初衷相互抵消。
- 建议：**保留宽限期**——归档后本地原图保留 30 天（或磁盘 <70% 时保留），`orig_archived=1` 只表示"已有一份远端副本"，清理任务再删；备份脚本对原图做 `rsync --link-dest` 保留 90 天。这条改动同时让 §14 的备份语义成立。

---

## 3. 技术风险（建议在 D1 做 spike，半天内出结论）

### R1 · FTS5 + trigram 在纯 Go 驱动里不一定可用，且对中文短词无效
- `glebarez/sqlite` 基于 `modernc.org/sqlite`，FTS5 是否编进二进制约需实测（一条 `CREATE VIRTUAL TABLE ... USING fts5` 即可）。
- `trigram` 分词器要求查询 **≥3 字符**：搜"博客""Go"（2 字符）搜不到；而中文关键词恰恰大量是两字词。
- 建议：D1 验证三件事（FTS5 可用 / trigram 建表 / 2 字查询），失败则改"FTS5 处理 ≥3 字 + `content_text LIKE '%q%'` 兜底 1–2 字"，并补一条搜索验收断言（"两字词能搜到"）。

### R2 · KaTeX 服务端渲染在 Go 没有官方实现
- 现状：§11 一句"KaTeX 服务端渲染为 HTML"。Go 侧只能：
  - (a) 内嵌 `katex.min.js` 用 `goja` 跑（启动 +~5MB 内存，渲染 ms 级，但每次发布都要跑）；
  - (b) 发布时 `exec` 容器内的 Node 跑一个 `render-math.mjs`（依赖进程模型，需处理超时）；
  - (c) 客户端渲染（与"读路径不重复渲染"冲突，且增加 JS）。
- 我的建议：**(b)**。同一个容器里本来就有 Node（Next），把它当"渲染 worker"比在 Go 里跑 JS 引擎更省内存、更好调试；失败降级为保留 `$...$` 原文。
- 需要你确认：数学公式在 v1 是否必须？如果不是，整体推到 V2，渲染管线先只做 goldmark + chroma + bluemonday。

### R3 · "CSP 禁内联脚本"与 Next.js App Router 冲突
- App Router 的 RSC 水合依赖内联 `<script>self.__next_f.push(...)</script>`。`script-src 'self'` 会直接打断水合，页面变静态死图。
- 建议：改用 **nonce 方案**（`middleware.ts` 生成 nonce，`<Script nonce>` + `Content-Security-Policy: script-src 'self' 'nonce-xxx' 'strict-dynamic'`），或退一步允许 `'unsafe-inline'` 并只用 `object-src 'none'; base-uri 'self'` 等其余指令。
- 这条必须写进 §9，否则上线当天白屏。

### R4 · `cache_size=-65536` 是每连接的
- SQLite 的 `PRAGMA cache_size` 按连接生效。`MaxOpenConns=4` ⇒ 最坏 4 × 64MB = 256MB，而 §15 给 SQLite 的预算是 64MB。
- 建议：`cache_size=-16384`（16MB × 4 = 64MB），或连接池收到 2，或读连接与写连接分离。

### R5 · PRAGMA 必须写进 DSN / connect hook
- `journal_mode`、`foreign_keys`、`busy_timeout` 都是 per-connection。只 `db.Exec()` 一次的话，池里新建的连接全是默认值。
- 建议：用 glebarez 的 DSN 参数（`?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)...`）或在 `gorm.Open` 后注册 `ConnPool` 的 `OnConnect`。

### R6 · 中文标题的 slug 规则没定义
- §4 规定 slug 仅 `[a-z0-9-]`，但没说从中文标题怎么来：拼音？手填？时间戳？
- 建议：**手填优先 + 自动生成兜底**（`pinyin` 前 40 字符，失败用 `post-YYYYMMDD-HHmm`），并在发布时校验"非空 + 唯一 + 已发布不可改"。

### R7 · 图片隐私与格式
- EXIF 未处理：手机原图带 GPS 坐标，`orig` 推 Cloudreve 分享页后等于公开住址。
- 格式未约定：iPhone 的 HEIC 浏览器无法解码、Go 标准库也不支持；动图 GIF/WebP、CMYK JPEG 会静默失败。
- 建议：入库前**先按 EXIF Orientation 摆正、再剥离全部 EXIF**（保留 `DateTimeOriginal` 可选）；接受 `jpeg/png/webp/gif`，HEIC/AVIF 明确拒绝并给出前端提示（浏览器端可先转码）。

### R8 · 其他小风险
- Next 版本与 `--max-old-space-size=384` 未锁定，standalone 实际 RSS 需实测（§15 的 350M 是估计值）。
- `ETag` 必须基于响应体哈希（强 ETag）而不是 `updated_at`，否则 304 会掩盖发布。
- `posts_fts` 是 external content 表，触发器必须覆盖 `INSERT/UPDATE/DELETE`，且 `posts.id` 必须与 FTS 的 rowid 对齐。

---

## 4. 缺失项（文档没提，但开工就会撞上）

| 编号 | 缺失 | 建议 |
| --- | --- | --- |
| M1 | 没有 CI | GitHub Actions 三条：`server`（fmt/vet/test/build）、`web`（lint/typecheck/build）、`image`（docker build + 推送） |
| M2 | 没有本地任务入口 | 根 `Makefile`（或 `Taskfile.yaml`）：`make dev / check / build / migrate / seed`，CI 与本地共用同一组命令 |
| M3 | 没有 `.dockerignore`、没有 Node/pnpm 版本钉住 | 补 `.nvmrc` + `packageManager` 字段 + `.dockerignore`（排除 `node_modules`、`.next`、`docs`、`*.db`） |
| M4 | 没有旧站迁移工具 | 需要 `cmd/import`（Markdown/WordPress XML → posts + media + redirects），并在 D11 之前跑通一次 dry-run |
| M5 | 没有 TLS / 防火墙 / SSH 加固 | certbot 自动续期、`ufw` 只开 80/443/SSH、禁 root 密码登录、可选 fail2ban |
| M6 | 没有告警通道 | 任务失败、磁盘 >85%、证书到期 → 邮件/Telegram/webhook 至少一种；写进 §13 |
| M7 | 没有"管理端登录态守卫"位置 | 管理页全是 CSR，首屏会闪一下未登录内容；建议在 `app/(admin)/layout.tsx` 里先 `GET /auth/me` 再渲染，401 直接 `router.replace('/login')` |
| M8 | 没有 CSRF token 下发路径 | `sid` 是 HttpOnly，前端拿不到；double-submit 需要 `GET /auth/me` 返回 `csrf_token`（或另设非 HttpOnly 的 `csrf` cookie） |
| M9 | 没有 API 文档 / 类型来源 | 从 Go 侧生成 OpenAPI（`swag` 或手写 `openapi.yaml`），前端用 `openapi-typescript` 生成 `lib/types.ts`，避免手写 DTO 漂移 |
| M10 | 没有并发编辑冲突策略 | `posts.revision` 字段已有，但没约定语义；建议 `PUT /posts/{id}` 带 `If-Match: <revision>`，不匹配返回 `409 CONFLICT` |
| M11 | 没有 404/410 语义 | `archived` 与已删除文章对外返回 `410 Gone`（比 404 更利于 SEO 去索引） |
| M12 | 没有暗色模式 / 字体 / 空态规范 | 只写一句"支持 `prefers-color-scheme`"和"字体子集化（中文按 unicode-range 分片）"即可 |
| M13 | 没有评论通知 | 二期做评论时，至少要有一个"待审核"角标 + 可选的邮件通知 |
| M14 | 备份恢复没有书面步骤 | §14 要求"必须有书面步骤"，但没说是哪个文件；建议 `docs/runbook.md` |

---

## 5. 三份规范草案

### 5.1 Go 后端：以 gin-vue-admin 为骨架（初版建议 · 已被 §9.1 取代）

> 你最终选了"完全照搬"，本节的三处"建议保留"不再生效；照搬后的目录、信封、错误码见 §9.1。

**先说结论**：`go-vue-admin` 这个名字下没有权威仓库（GitHub 搜同名仓库最高 55 星，且是 goframe 或其他技术栈）。你说的应该是 **`flipped-aurora/gin-vue-admin`**（25,075 星，Apache-2.0，`server/` 就是标准 Go 后端骨架：`api/v1/{system,example} + enter.go`、`service/`、`model/`、`router/`、`middleware/`、`initialize/`、`global/`、`core/`、`task/`、`utils/`、`config/`、`model/common/response`）。我按它的骨架来。

另外必须提前说清：**gin-vue-admin 的约定与本文档有 3 处直接冲突**，不能既"完全照搬"又"保持契约"：

| 冲突点 | gin-vue-admin 的做法 | 本文档的约定 | 我的建议 |
| --- | --- | --- | --- |
| 响应信封 | `{code, data, msg}` | `{"error":{"code","message"}}` + 直接返数据 | **保留本文档**，只借结构 |
| 分层 | `api → service → model`，无独立 repo | `handler → service → repo` | **model 包内拆 `repo.go`**，逻辑上保留 repo 层 |
| 全局变量 | `global.GVA_DB` 到处用 | 只允许 DB/配置单例 | **收紧**：`global` 只放 `DB / Config / Log` |

如果你要的是"连信封都照搬"，那 §5 整节要重写，我可以做，但会丢掉现在这套错误码（`SLUG_CONFLICT`、`MEDIA_IN_USE` 这类语义化 code 比统一 `code:7` 好用得多）。

#### 目录树

```
apps/server/                        # Go module 根（module 名待定）
├── main.go                         # 与 GVA 一致：只做 initialize → run
├── go.mod
├── config.yaml                     # 与 GVA 一致：viper + yaml，敏感项走环境变量覆盖
├── config/                         # 配置结构体（GVA 同名）
├── global/                         # 仅 3 个文件：db.go / config.go / log.go
├── core/                           # 基础设施（无业务）
│   ├── log/                        # slog 初始化
│   ├── db/                         # sqlite 打开 + PRAGMA + 健康检查
│   ├── edge/                       # 双 listener + 反代 Next + 404 拦截（GVA 没有，本项目特有）
│   ├── storage/                    # 本地磁盘布局 / Cloudreve WebDAV
│   ├── render/                     # goldmark + chroma + bluemonday
│   └── mail/                       # V2：通知
├── api/v1/                         # ← GVA 的 api/v1：只做绑定、校验、响应
│   ├── public/                     # 8640：post.go tag.go archive.go search.go comment.go ...
│   ├── admin/                      # 8641：auth.go post.go media.go setting.go job.go ...
│   ├── internal/                   # 127.0.0.1：revalidate.go health.go
│   └── enter.go                    # ApiGroup 聚合（GVA 风格）
├── service/                        # ← GVA 的 service：唯一写业务的地方
│   ├── post/ media/ comment/ auth/ stat/ setting/ job/
│   └── enter.go
├── model/                          # ← GVA 的 model：实体 + DTO + repo
│   ├── post/      {post.go, request.go, response.go, repo.go, enter.go}
│   ├── media/ comment/ auth/ stat/ setting/ job/
│   └── common/
│       ├── response/               # 统一信封与错误构造（GVA 同名）
│       └── errs/                   # AppError + 错误码常量
├── router/                         # ← GVA 的 router：只注册路由与中间件链
│   ├── public.go  admin.go  internal.go  enter.go
├── middleware/                     # ← GVA 的 middleware
│   ├── request_id.go  logger.go  recovery.go  ratelimit.go
│   ├── auth.go  csrf.go  cors.go  security_header.go  audit.go
├── initialize/                     # ← GVA 的 initialize：启动装配
│   ├── viper.go  logger.go  gorm.go  migrate.go  router.go  task.go  server.go
├── task/                           # ← GVA 的 task：任务执行器（对应 §13）
│   ├── scheduler.go  publish.go  media_process.go  media_archive.go
│   ├── link_check.go  backup.go  cleanup.go
├── utils/                          # ← GVA 的 utils：纯函数，无 DB、无 gin
├── migrations/                     # golang-migrate 版本化 SQL（0001_init.up.sql ...）
├── resource/                       # 内嵌资源（katex.min.js、错误页模板、默认配置）
└── testdata/                       # 测试夹具（示例图片、样例 md）
```

**不引入** GVA 的：`plugin/`（插件体系）、`source/`（代码生成器）、`mcp/`、`docs/`（swagger 目录，改用根 `docs/api.md` + 生成物）。`enter.go` 聚合模式保留——这是 GVA 最值得抄的一点。

#### 分层与依赖规则（写进 §3，替换现有 5 条）

1. **依赖方向**：`api → service → model`；`initialize` 可依赖全部；`core/*` 不得反向依赖业务包；`utils` 不得依赖以上任何一层。
2. `api/v1/*` 只允许：参数绑定 → `binding`/`validator` 校验 → 调 service → 组装响应。**禁止** SQL、事务、状态机判断、权限判断。
3. `service` **不得导入 `gin`**，不得接收 `*gin.Context`；入参是 DTO，出参是领域对象或 `*errs.AppError`。
4. `model/<domain>/repo.go` 是**唯一**允许出现 `gorm` 与 SQL 的地方；repo 不做权限/状态判断。
5. `router` 只做注册：`public` / `admin` / `internal` 三个 engine，各自的中间件链在 `router/enter.go` 里显式列出。
6. `global` 只有 `DB`、`Config`、`Log` 三个变量；新增全局必须在本文件加一节说明。
7. 每个业务包提供 `enter.go`，用 `ServiceGroup` / `ApiGroup` 聚合，禁止跨包直接 new 具体实现。
8. `migrations/` 只增不改；任何表结构变更必须新文件 + 在 `docs/agent.md` §4 同步。

#### Go 代码风格（写进 §7 补充）

| 项 | 规定 |
| --- | --- |
| 命名 | 目录/包名小写单数；导出符号英文；中文注释允许，**标识符必须是英文** |
| 错误 | 哨兵错误在 `model/common/errs`；`fmt.Errorf("post: publish: %w", err)` 逐层包装；对外只出 `AppError` |
| 日志 | 只用 `slog`；字段 `request_id, module, post_id...`；禁止 `fmt.Print*`、禁止打印密码/邮箱/cookie |
| 上下文 | 所有 service/repo 方法第一个参数 `ctx context.Context`；不得存进结构体 |
| 并发 | 用 `errgroup`；禁止裸 `go func()`；channel 必须有明确的关闭方 |
| 时间 | 内存里用 `time.Time`，出入库一律 `dbtime.Unix`（自定义类型，实现 `Scanner/Valuer`） |
| 事务 | `db.Transaction(func(tx *gorm.DB) error {...})`；跨 repo 的事务由 service 发起，tx 透传 |
| 测试 | 表驱动 + `t.Parallel()`；service 覆盖率 ≥70%；每个 api 至少 1 happy + 1 error |
| Lint | `gofmt` + `goimports` + `golangci-lint`（`govet,errcheck,staticcheck,revive,gosec,bodyclose,unparam`） |
| 注释 | 所有导出符号必须有注释；复杂 SQL 上方写"为什么"而不是"是什么" |

### 5.2 前端：以 `dubinc/dub` 为参照（初版建议 · 已被 §9.3 取代）

> 你选了 `ui/` 而不是 `components/`，最终目录与别名见 §9.3。

**为什么是 dub**：同一个技术栈（Next App Router + Tailwind + shadcn/ui + SWR + react-hook-form + Zod + TypeScript），同样是 `apps/web` 单应用、同样有大量表单与列表页，且目录组织（`app/` 路由分组、`ui/` 按领域分组、`lib/` 按领域分组、`lib/zod`、`lib/swr`、`styles/`）可以直接映射到博客 + 管理端。

备选（如果你更喜欢）：`vercel/commerce`（Vercel 官方示例，更简单，但电商语义）、`shadcn-ui/taxonomy`（App Router 的经典范例，已归档）。

**只借约定，不借业务**：dub 的 Prisma / tRPC / Stripe / 多租户一律不引入。

#### 目录树

```
apps/web/
├── app/
│   ├── (public)/                   # 公开页，全部 SSR
│   │   ├── page.tsx                # /
│   │   ├── posts/page.tsx          # 列表（分页/标签/系列过滤）
│   │   ├── posts/[slug]/page.tsx   # 详情
│   │   ├── tags/[slug]/page.tsx
│   │   ├── series/[slug]/page.tsx
│   │   ├── archive/page.tsx
│   │   ├── search/page.tsx
│   │   ├── about/page.tsx
│   │   ├── links/page.tsx
│   │   └── layout.tsx
│   ├── (admin)/admin/              # 管理端，全部 'use client'
│   │   ├── layout.tsx              # 登录态守卫 + 侧边栏
│   │   ├── page.tsx                # 概览
│   │   ├── posts/[id]/edit/page.tsx
│   │   ├── media/page.tsx
│   │   ├── comments/page.tsx
│   │   ├── settings/page.tsx
│   │   └── jobs/page.tsx
│   ├── (auth)/login/page.tsx
│   ├── (preview)/preview/[token]/page.tsx
│   ├── layout.tsx  providers.tsx  globals.css(→styles)
│   ├── not-found.tsx  error.tsx  loading.tsx
│   ├── robots.ts  sitemap.ts  rss.xml/route.ts
│   └── api/                        # 仅 Next 自身端点（og 图、rss）；业务 API 一律走 Go
├── components/                     # dub 的 ui/ → 这里叫 components/（shadcn 默认约定）
│   ├── ui/                         # shadcn 生成物，**不手改**
│   ├── layout/                     # header footer nav theme-toggle
│   ├── post/                       # post-card post-list post-image toc prose code-block
│   ├── admin/                      # editor media-picker stat-card data-table
│   └── shared/                     # 通用壳
├── lib/
│   ├── api/                        # **唯一** fetch 封装
│   │   ├── fetcher.ts              # 基础请求、错误归一化、401 处理
│   │   ├── public.ts               # 服务端调用 127.0.0.1:8640
│   │   └── admin.ts                # 浏览器调用同源 /api/admin
│   ├── hooks/                      # use-posts use-media use-autosave ...
│   ├── zod/                        # 表单 schema（与 Go 校验一致）
│   ├── constants/                  # 站点常量、导航、枚举（与 §4 枚举同源）
│   ├── utils.ts  format.ts
│   └── types.ts                    # 由 OpenAPI 生成
├── styles/
│   ├── globals.css
│   └── prose.css
├── public/
├── tests/                          # vitest 单测
├── e2e/                            # playwright 三条关键流程
├── next.config.ts  tailwind.config.ts  postcss.config.mjs
├── tsconfig.json  eslint.config.mjs  .prettierrc
└── package.json
```

**与现行 §3 的差异**（需要你确认）：`app/(public)` → `app/(public)/` 保持；`components/ui` 保留；新增 `components/{layout,post,admin,shared}` 与 `lib/{api,hooks,zod,constants}`；`lib/api.ts` 改为 `lib/api/` 目录（一个文件放不下且不好拆）。

#### 前端规则（写进 §8，替换现有表格）

| 项 | 规定 |
| --- | --- |
| TypeScript | `strict: true`；禁止 `any`（用 `unknown` + zod 解析）；`noUncheckedIndexedAccess` 建议开 |
| 组件 | 服务端组件为默认；`'use client'` 只加在需要交互的文件；管理端整树 client（按 C9） |
| 命名 | 文件 kebab-case；组件 PascalCase；类型 `XxxProps`；不用 `React.FC` |
| 导出 | 只有 Next 约定文件用 default export，其余一律具名导出 |
| 数据 | 只用 `lib/api/*`；`lib/hooks/*` 包 SWR；组件内**禁止**直接 `fetch` |
| 表单 | `react-hook-form` + `zodResolver`；schema 在 `lib/zod/`；与服务端错误码映射到字段 |
| 样式 | Tailwind + `cn()`；颜色/间距只用 `tailwind.config.ts` 的 token；禁止内联 style（除动态尺寸） |
| 图标 | `lucide-react`（shadcn 默认） |
| 可访问性 | `eslint-plugin-jsx-a11y`；所有交互元素可键盘到达；图片 alt 必填 |
| 错误 | `lib/api/fetcher.ts` 统一抛 `ApiError{code,message,status}`；页面用 `error.tsx` 兜底，不暴露堆栈 |
| Server Actions | **禁止**（会绕过 `lib/api` 契约）；如需，先在本文档加一节 |
| 测试 | Vitest + Testing Library 覆盖 hooks/工具函数；Playwright 覆盖 登录 / 发文 / 传图 三条 |
| 质量门 | `pnpm lint && pnpm typecheck && pnpm test && pnpm build` 全绿才允许提交 |

### 5.3 Git 规范（新增一节，替换 §19 第 4 条的"提交信息格式"）

#### 分支

- 主干 `main`，永远可构建、可部署。
- 短分支：`feat/<scope>-<slug>`、`fix/<scope>-<slug>`、`chore/<slug>`、`docs/<slug>`。
- 生命周期 ≤ 2 天；合并进 `main` 用 **squash**，squash 后的标题即提交规范标题。
- 单人开发也建议走分支：出问题能整段丢弃，而且历史干净。如果你嫌麻烦，允许直接提交 `main`，但**必须**通过 `make check`。

#### 提交信息

采用 **Conventional Commits 1.0.0**，`type` 与 `scope` 用英文，**主题用中文**：

```
<type>(<scope>): <主题>

<正文（可选，说明为什么，不要重复"改了什么"）>

<脚注（可选）：BREAKING CHANGE / Refs #12>
```

**type**

| type | 用途 |
| --- | --- |
| `feat` | 新功能 |
| `fix` | 修 bug |
| `docs` | 文档、注释、ADR |
| `refactor` | 不改行为的重构 |
| `perf` | 性能 |
| `style` | 格式（gofmt/prettier），不含逻辑 |
| `test` | 测试 |
| `build` | 依赖、构建脚本、Dockerfile |
| `ci` | CI 配置 |
| `chore` | 杂项（版本号、脚本） |
| `revert` | 回滚 |

**scope**（与 §3 模块一一对应）

`server` `api` `web` `admin` `media` `auth` `db` `deploy` `docs` `deps` `ci`

**硬规则**

1. 一行标题 ≤ 50 个字符（中文按 2 计），不加句号。
2. 正文每行 ≤ 72，说明**动机与取舍**，不写"修改了 xx 文件"。
3. 一次提交只碰一个模块；跨 3 个以上模块的提交禁止（保留 §19 原规则）。
4. 提交必须能通过 `make check`；不允许"先提交再修"。
5. 不提交 `.env`、`*.db`、`uploads/`、`.next/`、`node_modules/`。
6. 破坏性变更必须写 `BREAKING CHANGE:` 脚注，并在同日更新 `docs/agent.md`。
7. 发布打 tag：`v0.1.0` 起，SemVer；tag 附注说明该版本覆盖的 §16 阶段。

**示例**

```
feat(server): 增加文章 slug 冲突检查
fix(web): 修复 375px 下代码块横向溢出
refactor(server): 把渲染管线从 handler 移到 service
perf(db): 为 posts 增加 (status, published_at) 部分索引
docs: 补充媒体归档策略 ADR
chore(deps): 升级 gorm 到 1.25.12
```

**不推荐**（会破坏 changelog 生成）

```
update code
fix bug
修改
feat: 一大堆改动
```

---

## 6. 其他建议（可选，不阻塞）

### 6.1 文档拆分
`agent.md` 已经 471 行且承担"契约 + 规范 + 计划 + 验收"四件事。建议拆：

- `docs/agent.md`：只留契约（§1–§6、§9、§17、§19、§20）
- `docs/standards/go.md`、`docs/standards/web.md`：§5.1 / §5.2 的详细版
- `docs/runbook.md`：部署、备份、恢复、证书、排查（对应 M14）
- `docs/api.md`：接口清单（由 OpenAPI 生成）
- `docs/adr/`：已规划，保持

### 6.2 文档版本化
给 `agent.md` 加 3 行头部：`version / updated_at / changelog`，每次改契约递增次版本号（`1.0 → 1.1`）。这次评审就是 `1.1`。

### 6.3 把"允许的依赖"列成白名单
§17 只列了禁止项，建议补一张"已批准依赖"表（gin/gorm/glebarez/golang-migrate/goldmark/chroma/bluemonday/goja-or-none/disintegration/imaging/webdav + 前端 next/react/tailwind/shadcn/swr/zod/rhf/date-fns）。避免实现时随手引库。

### 6.4 关于 12 天 × 2h 的工期
24 小时做完"21 张表 + 媒体管线 + 管理端 + 公开端 + SEO + 部署 + 数据迁移"，即使有 AI 辅助也非常紧。我的判断：
- 按 A11 把二期功能移出 v1 后，**可行但没余量**；
- 建议把 §18 的 `D12 缓冲` 变成真正的缓冲（不排任何任务），并把 D9 SEO 与 D11 迁移各留半天余量；
- 如果时间不够，优先砍"系列、附件、友链、版本回滚"，不要砍"索引、错误处理、备份恢复演练"。

### 6.5 关于环境（需要你知道）
本会话的 PowerShell 被 Windows 文件权限挡住了（`grantWrite(D:\ZJUT.SI\BLOG)`），所以这次我用文件工具完成，没有跑 `git`、`go`、`pnpm`。后续要 `git init` / 跑测试时，需要先修这个权限问题（我可以给一条一次性诊断修复命令，需要你批准）。

---

## 7. 待你回答的问题

| 编号 | 问题 | 我的建议 |
| --- | --- | --- |
| Q1 | "go-vue-admin"是指 `flipped-aurora/gin-vue-admin` 吗？采用 §5.1 的"结构照搬、契约保留"方案，还是连 `{code,data,msg}` 一起照搬？ | 结构照搬 + 契约保留 |
| Q2 | 前端参照选 `dubinc/dub`？`components/` 还是按 dub 用 `ui/`？ | dub + `components/`（shadcn 默认） |
| Q3 | Git 用 §5.3 的方案吗？`main` 直接提交还是必须走分支？ | Conventional Commits + 分支 + squash |
| Q4 | v1 范围按 A11 收缩（评论/搜索/统计/点赞/系列/附件/预览/定时/回滚推 V2）？ | 收缩，表建全、接口标 V2 |
| Q5 | 旧博客现在是什么？（WordPress / Hexo / Typecho / 其他）文章多少篇？URL 形态？评论要不要迁？ | 决定 M4 的迁移工具形态 |
| Q6 | 数学公式 v1 要吗？要的话接受"发布时调 Node 渲染"吗？ | 不要就砍，要就用 Node 子进程 |
| Q7 | 原图本地保留 30 天（A14）可以吗？ | 建议保留 |
| Q8 | 域名/证书现状？`blog.zjut.si` 已解析到这台 VPS 吗？ | 影响 M5 |
| Q9 | 是否已有 Git 仓库与远端（GitHub 私有 / 自建 Gitea）？ | 我推断当前目录**还没有** `.git` |
| Q10 | 仓库根目录规范：`apps/server` + `apps/web` 要不要加 pnpm workspace 根（`pnpm-workspace.yaml` + 根 `package.json`）？ | 建议加，方便统一脚本；不加也行 |

---

## 8. 批准后我会对 `agent.md` 做的修改（逐条清单）

1. **§0** 增加版本头（version / updated_at / changelog），本次为 `1.1`。
2. **§1 C3** 改写成"宿主绑定 127.0.0.1 / 容器内监听 0.0.0.0"，并补 `TrustedProxies` 说明（A9）。
3. **§2** 补"健康检查只挂 8641"（A10）。
4. **§3** 重写仓库结构为 `apps/server`（GVA 骨架）+ `apps/web`（dub 组织），并替换 5 条分层规则为 §5.1 的 8 条；`AGENTS.md` 统一为 `docs/agent.md`（A8）。
5. **§4** 补 `link_checks`、`pages`、`preview_tokens.used_at`；`stats_daily.date` 明确时区；表数量改为迁移版本号（A1/A4/A7/A13）。
6. **§5** 补 `GET /api/preview/{token}`、`POST /api/admin/posts/preview`、`GET /api/links`、`GET /api/series`、`GET /auth/me` 返回 `csrf_token`；给每个端点标 `V1`/`V2`（A3/A5/A6/A7/M8/A11）。
7. **§6** 补 `uploading` 不落库的说明（A12）。
8. **§7** 追加 §5.1 的编码风格表 + PRAGMA 走 DSN + `cache_size` 调整（R4/R5）。
9. **§8** 用 §5.2 的目录树与规则表替换（A5、M7）。
10. **§9** CSP 改 nonce 方案；补 EXIF 剥离、HEIC 拒绝、CSRF 下发（R3/R7/M8）。
11. **§10** 改为"归档后本地保留 30 天"（A14）；补"变体不超过原图尺寸"。
12. **§11** 补 slug 生成规则、归档/删除返回 410（R6/M11）。
13. **§12** 补强 ETag 语义（体哈希）。
14. **§13** 增加 `cleanup` 任务；task 表加"失败通知"（A2/M6）。
15. **§14** 补 TLS/续期/防火墙/SSH 加固；备份含原图 90 天（M5/A14）。
16. **§15** 修正 SQLite 内存预算（R4）。
17. **§16** 按 `V1` 重排断言，新增"两字词可搜""预览链接可用""cleanup 生效"。
18. **§17** 增加"已批准依赖白名单"（6.3）。
19. **§19** 用 §5.3 替换提交规范；补文档版本化流程。
20. **新增 §21**：`docs/runbook.md`、`docs/standards/*`、`docs/api.md` 的存在与维护要求。
21. **文末**：变更记录表（`1.0 冻结 → 1.1 评审修订`，列出上表编号）。

---

## 附：我对这份文档的总体评价

优点（值得保留，不要改）：
- 硬约束表（C1–C10）写得比大多数商业项目清楚，尤其是"管理端永不出现在 8640"和"图片不入库"。
- §17 反模式清单、§16 验收门禁、§20 DoD 三件套，是这份文档最值钱的部分。
- 明确写了"3065 端口不许动"这种环境约束。

主要风险：
- 冲突项集中在"数据/接口/页面"三者的对应关系上，**这类问题不会在写代码时报错，只会在联调时暴露**；
- 4 个技术点（FTS5 中文、KaTeX、CSP、Docker 回环）都是"文档看起来对、跑起来不对"的类型，建议 D1 一起 spike；
- 规范层是空白（Go 结构、前端结构、Git、CI），这次补上之后，后续 AI 编码才有可执行的约束。

---

## 9. 定稿条款（按你 2026-10-18 的选择）

| 决策 | 你的选择 | 影响 |
| --- | --- | --- |
| D-A | 完全照搬 gin-vue-admin | §3 目录与分层、§5 信封与错误码、§7 全部改写 |
| D-B | dub + `ui/` | §3/§8 前端目录与别名改写 |
| D-C | Conventional Commits + 分支 + squash | §19 提交规范替换 |
| D-D | 全量做，不砍二期 | §18 排期必须延长（见 9.5） |
| D-E | 全新站，无旧内容 | 迁移工具取消，§16/§18 的 D11 改写 |

> 本节与 §5.1/§5.2/§8 冲突时，**以本节为准**。批准后按 §9.7 合并。

---

### 9.1 Go：完全照搬 gin-vue-admin

#### 9.1.1 最终目录

```
apps/server/
├── main.go                      # 与 GVA 一致：仅 initialize → run
├── go.mod
├── config.yaml
├── config/                      # viper 配置结构体
├── global/                      # db.go / config.go / log.go（只这 3 个）
├── core/                        # GVA 的 core：基础设施
│   ├── log/  db/  storage/  render/  mail/
│   └── edge/                    # 本项目新增：双 listener + 反代 + 404 拦截
├── api/v1/                      # GVA 的 api/v1
│   ├── public/                  # 8640：post.go tag.go archive.go search.go comment.go like.go media.go
│   ├── admin/                   # 8641：auth.go post.go media.go comment.go setting.go job.go stat.go audit.go page.go
│   ├── internal/                # 仅 127.0.0.1：revalidate.go health.go
│   └── enter.go                 # ApiGroup 聚合
├── service/                     # GVA 的 service：业务逻辑
│   ├── post/ media/ comment/ auth/ stat/ setting/ job/ system/
│   └── enter.go
├── model/                       # GVA 的 model：实体 + DTO + 数据访问（同一文件）
│   ├── post/      {post.go, request.go, response.go, enter.go}
│   ├── media/ comment/ auth/ stat/ setting/ job/ page/
│   └── common/
│       ├── response/            # 统一信封（GVA 同名同路径）
│       ├── request/             # PageInfo 等通用入参
│       └── errs/                # 业务错误（AppError + 码表）
├── router/                      # public.go admin.go internal.go enter.go
├── middleware/                  # request_id logger recovery ratelimit auth csrf security audit
├── initialize/                  # viper logger gorm migrate router task server
├── task/                        # scheduler + publish/media_process/media_archive/link_check/backup/cleanup
├── utils/                       # 纯函数
├── migrations/                  # 0001_init.up.sql ...
├── resource/                    # 内嵌：katex、默认配置、错误页
└── testdata/
```

相对初版建议的 3 处改动：

1. **没有 `internal/`**：全部按 GVA 放模块根。
2. **没有 `repo.go`**：GVA 的 model 包本身就是数据访问层，实体与 `GetXxx / CreateXxx / UpdateXxx` 同文件、同包。
3. **`core/edge` 是本项目新增**（GVA 没有双 listener 场景），职责严格限定为：路由分发、`404` 拦截、Next 反代、Header 注入。

#### 9.1.2 分层规则（替换 §3 的 5 条）

1. 依赖方向 `api/v1 → service → model`；`initialize` 可依赖全部；`core/*`、`utils` 不得反向依赖业务包。
2. `api/v1/*` 只做：绑定 → `binding` 校验 → 调 service → `response.*` 输出。禁止 SQL、事务、状态机判断。
3. `service/*` 写全部业务规则；事务由 service 发起（`global.DB.Transaction`），tx 以参数透传。
4. `model/*` 是唯一出现 `gorm` 与 SQL 的地方；不做权限与状态机判断。
5. `api` 不得直接调用 model 的查询函数（等价的"handler 不碰 repo"）。
6. `global` 只有 `DB`、`Config`、`Log` 三个变量；新增全局必须先改契约。
7. `router/*` 只注册路由与中间件链，三个 engine（public / admin / internal）各自成文件。
8. 每个业务包一个 `enter.go` 聚合（`ServiceGroup` / `ApiGroup`），跨包只走聚合入口。

#### 9.1.3 响应包（照搬 GVA 的形状）

```go
// model/common/response/response.go
type Response struct {
    Code int         `json:"code"`
    Data interface{} `json:"data"`
    Msg  string      `json:"msg"`
}

type PageResult struct {
    List     interface{} `json:"list"`
    Total    int64       `json:"total"`
    Page     int         `json:"page"`
    PageSize int         `json:"pageSize"`
}

func OkWithData(data interface{}, c *gin.Context)
func OkWithMessage(message string, c *gin.Context)
func FailWithMessage(message string, c *gin.Context)
func FailWithDetailed(code int, data interface{}, message string, c *gin.Context)
```

- 成功：`{"code":0,"data":<数据>,"msg":"操作成功"}`
- 失败：`{"code":<数字码>,"data":<details|null>,"msg":"<人话>"}`
- 分页：`data` 固定为 `{"list":[],"total":0,"page":1,"pageSize":20}`
- **取消**原 §5 的 `{"error":{...}}` 信封与 `per_page` 命名，统一为 GVA 的 `page` / `pageSize`。

#### 9.1.4 错误码（数字码 + 原语义名对照）

| code | 常量 | HTTP | 原语义 |
| --- | --- | --- | --- |
| 0 | `SUCCESS` | 200 | 成功 |
| 7 | `ERROR` | 500 | 未分类失败 |
| 1000 | `ERROR_AUTH` | 401 | `UNAUTHORIZED` |
| 1001 | `ERROR_FORBIDDEN` | 403 | `FORBIDDEN`（含 CSRF 失败） |
| 1002 | `ERROR_NOT_FOUND` | 404 | `NOT_FOUND` |
| 1003 | `ERROR_VALIDATION` | 400 | `VALIDATION_FAILED` |
| 1004 | `ERROR_CONFLICT` | 409 | `CONFLICT` |
| 1005 | `ERROR_RATE_LIMITED` | 429 | `RATE_LIMITED` |
| 1006 | `ERROR_INTERNAL` | 500 | `INTERNAL` |
| 2001 | `ERROR_SLUG_CONFLICT` | 409 | `SLUG_CONFLICT` |
| 2002 | `ERROR_MEDIA_IN_USE` | 409 | `MEDIA_IN_USE`，`data` 带引用清单 |
| 2003 | `ERROR_IMAGE_TOO_LARGE` | 413 | `IMAGE_TOO_LARGE`（像素/字节超限） |
| 2004 | `ERROR_SLUG_IMMUTABLE` | 409 | 已发布改 slug 且未带 redirect |
| 2005 | `ERROR_LOGIN_LOCKED` | 423 | 登录锁定 30 分钟 |
| 2006 | `ERROR_PREVIEW_EXPIRED` | 410 | 预览链接过期/已用 |

保留原语义名作为 Go 常量名或注释，**不允许**在代码里出现裸数字。

#### 9.1.5 两处必须偏离 GVA（请你点头，或否决）

**偏离一：HTTP 状态码不照搬。**

GVA 的 `Result()` 是 `c.JSON(http.StatusOK, ...)`，错误也返回 200。但本文档 §16 的 D2/D3 断言写死了 `401`、`403`、`409`，§5 也要求区分 `409 MEDIA_IN_USE`；而且 nginx 日志、`ETag/304`、监控告警全依赖状态码。

做法：函数签名与调用方式完全不变，只把 `Result()` 改成按下表查状态码：

| code 段 | HTTP |
| --- | --- |
| 0 | 200 |
| 1003 | 400 |
| 1000 / 2005 | 401 / 423 |
| 1001 | 403 |
| 1002 | 404 |
| 1004 / 2001 / 2002 / 2004 | 409 |
| 2003 | 413 |
| 1005 | 429 |
| 2006 | 410 |
| 7 / 1006 / 其他 | 500 |

**如果你坚持全 200**：那 §16 D2 的"未登录 = 401""无 CSRF = 403"、D3 的"slug 冲突 409"必须全部改写成"断言 `code` 字段"，nginx 侧也无法再按状态码做限流与监控。我建议不要。

**偏离二：`service` 不接收 `*gin.Context`。**

GVA 个别 service 会把 `c` 传下去（主要为了 captcha / 客户端信息）。本项目禁止：service 只接收 `context.Context` + DTO。理由：可测试、可被 task 复用（定时发布、归档都要调用同一套 service）。

除此之外，全局变量、`enter.go` 聚合、`initialize` 装配顺序、`model` 承担 repo 职责，**全部照搬**。

---

### 9.2 §5 接口契约改写（替换整节）

**通用**

| 项 | 规定 |
| --- | --- |
| 前缀 | `/api`；GVA 风格路径（`/api/v1` 前缀**不引入**，保持现有 URL 不变） |
| 成功 | HTTP 200 + `{"code":0,"data":...,"msg":"操作成功"}` |
| 失败 | 对应 HTTP 状态 + `{"code":<数字>,"data":null,"msg":"..."}` |
| 校验失败 | `code:1003`，`data` 为 `{"field":"标题","reason":"不能为空"}` 的数组 |
| 分页入参 | `page`(≥1) `pageSize`(1–50, 默认 20) `keyword`(可选) |
| 分页出参 | `data:{list,total,page,pageSize}` |
| 时间 | 全部出参 Unix 秒整数 |
| 缓存 | 公开读接口强 `ETag` + `If-None-Match → 304` |
| CSRF | `/api/admin/*` 写操作必须 `X-CSRF-Token`；token 由 `GET /auth/me` 返回 |

**公开组（8640）**

`GET /site` · `GET /posts` · `GET /posts/{slug}` · `GET /posts/{slug}/related` · `GET /posts/{slug}/comments` · `POST /posts/{slug}/comments` · `POST /posts/{slug}/like` · `GET /tags` · `GET /tags/{slug}` · `GET /archive` · `GET /series` · `GET /series/{slug}` · `GET /search?q` · `GET /links` · `GET /pages/{slug}`（about 等）· `POST /events/view` · `GET /attachments/{id}/go` · **`GET /preview/{token}`**（`no-store`，补 A3）

**管理组（8641）**

`POST /auth/login` · `POST /auth/logout` · `GET /auth/me`（含 `csrf_token`） · `GET|POST /posts` · `GET|PUT|PATCH|DELETE /posts/{id}` · `POST /posts/{id}/publish` · `POST /posts/{id}/unpublish` · `GET /posts/{id}/revisions` · `POST /posts/{id}/revisions/{rid}/restore` · `POST /posts/{id}/preview-link` · `DELETE /preview-links/{token}` · **`POST /posts/preview`**（编辑器实时渲染，补 A5） · `GET|POST /media` · `DELETE /media/{id}` · `POST /media/{id}/retry` · `GET|PATCH /comments` · `POST /comments/bulk` · `GET|PUT /settings` · **`GET|PUT /pages`**（about 等静态页，补 A7） · `GET|POST|PUT|DELETE /{tags|series|links|redirects|attachments}` · `GET /stats/overview` · `GET /stats/daily?from&to` · `GET /jobs` · `POST /jobs/{id}/retry` · `GET /audit-logs`

**内部组（8641，仅容器内/隧道）**

`POST /api/internal/revalidate` · `GET /api/internal/health` · **`GET /healthz` / `GET /readyz` 只注册在 8641**（补 A10）

---

### 9.3 前端：dubinc/dub + `ui/`（替换 §3 与 §8）

```
apps/web/
├── app/
│   ├── (public)/                # / posts tags series archive search about links
│   ├── (admin)/admin/           # 全 CSR
│   ├── (auth)/login/
│   ├── (preview)/preview/[token]/
│   ├── layout.tsx  providers.tsx  not-found.tsx  error.tsx
│   ├── robots.ts  sitemap.ts  rss.xml/route.ts
│   └── api/                     # 仅 Next 自身端点（og 图、rss）
├── ui/                          # ← 对应 dub 的 apps/web/ui
│   ├── shadcn/                  # shadcn 生成物，不手改（dub 放在 packages/ui/src，本项目单应用收进来）
│   ├── layout/  post/  admin/  shared/
├── lib/                         # ← 对应 dub 的 apps/web/lib
│   ├── api/                     # fetcher.ts + public.ts + admin.ts（唯一 fetch 入口）
│   ├── hooks/  zod/  constants/  swr/
│   ├── utils.ts  format.ts  types.ts
├── styles/                      # globals.css prose.css
├── public/
├── tests/  e2e/
├── next.config.ts  tailwind.config.ts  postcss.config.mjs
├── tsconfig.json  eslint.config.mjs  .prettierrc  components.json
└── package.json
```

**别名（照 dub 的写法，不用 `@/*` 通配）**

```json
{
  "compilerOptions": {
    "baseUrl": ".",
    "paths": {
      "@/ui/*": ["ui/*"],
      "@/lib/*": ["lib/*"],
      "@/styles/*": ["styles/*"],
      "@/app/*": ["app/*"]
    }
  }
}
```

**`components.json`（shadcn 配置）**

```json
{
  "aliases": {
    "components": "@/ui",
    "ui": "@/ui/shadcn",
    "lib": "@/lib",
    "hooks": "@/lib/hooks",
    "utils": "@/lib/utils"
  }
}
```

**规则**：沿用 §5.2 的表格，只把路径换掉（`components/` → `ui/`，`lib/api.ts` → `lib/api/`）。另外三条来自 dub 的约定：

- 文件一律 kebab-case（`post-card.tsx`），组件名 PascalCase；
- 每个领域一个目录，目录内不再按 `components/`、`hooks/` 二级分（hook 放 `lib/hooks/`）；
- `app/api/` 只允许出现"Next 自身才有意义"的端点（og 图、rss），**任何业务数据不得经 Next 的 API 路由**。

---

### 9.4 Git（已确认 A，与 §5.3 一致）

在 §5.3 基础上补三条：

1. 分支名与 scope 对齐：`feat/server-post-crud`、`fix/web-375-overflow`、`docs/adr-media`。
2. squash 后的消息必须重写成规范格式（GitHub 默认的 `feat: xxx (#12)` 要手工改标题）。
3. `main` 加保护：禁止 force push；合并前 CI 必须绿。

---

### 9.5 范围与排期（全量做 → 排期必须延长）

选了"全量"，24 小时做不完。按每项功能 2h 的粒度重排为 **20 天 × 2h**：

| 天 | 内容 | 完成标志 |
| --- | --- | --- |
| D1 | 契约 v1.1 + 仓库骨架 + migration + 双 listener + CI 骨架 | 空库迁移通过；8640/8641 断言；CI 绿 |
| D2 | GVA 骨架落地（response/enter/config/logger/middleware） | 一条 `/api/system/ping` 打通 |
| D3 | 鉴权：登录/登出/me/会话/CSRF/限流/审计 | §16 D2 全过 |
| D4 | 文章 CRUD + 状态机 + revision + slug 规则 | 草稿/发布/归档/软删可用 |
| D5 | 编辑器 + 自动保存 + 服务端预览 | 可写长文并预览 |
| D6 | 渲染管线（goldmark/chroma/bluemonday/TOC/摘要/阅读时长）+ FTS spike | 编辑器所见 = 发布所得 |
| D7 | 媒体上传 + 去重 + 单档 WebP + 媒体库 | 图文混排跑通 |
| D8 | 四档变体 + blurhash + WebDAV 归档 + 原图保留 | 内存/去重断言 |
| D9 | 公开页：首页/列表/详情/标签/归档 | 375px 通读 |
| D10 | 公开页：搜索/系列/关于/友链/404/410 | 分页 + 标签组合正确 |
| D11 | 评论：提交/审核/嵌套/反垃圾 | 评论可见与审核 |
| D12 | 点赞 + PV/UV + stats_daily + 后台概览 | 统计数字自洽 |
| D13 | 定时发布 + 版本回滚 + 预览链接 | 三个任务可验证 |
| D14 | 附件下载区 + 后台任务/审计/设置页 | 管理端闭环 |
| D15 | SEO：sitemap/rss/json-ld/og/robots | Search Console 可提交 |
| D16 | 部署：Dockerfile + s6 + compose + nginx + 证书 + 隧道 | 双通道访问成功 |
| D17 | 备份恢复 + 监控告警 + runbook | 空机恢复演练成功 |
| D18 | 测试补齐 + 覆盖率门禁 | 单测/接口/E2E 全绿 |
| D19 | 性能与内存实测 + 可访问性 + 全站校对 | TTFB/LCP/内存达标 |
| D20 | 缓冲：只修不加 | 上线 |

> 如果 12 天不可动，只能回到 A 方案：砍掉 D11–D14 四天（评论/点赞统计/定时回滚预览/附件），把这几块表留在库里、接口不承诺。两者不能同时要。

---

### 9.6 "全新站，无旧内容"带来的删减

1. **删除 M4**（`cmd/import` 迁移工具）与 §18 D11 的"迁移 / 301"。
2. **§16 D11 改写**为："首次上线校对：无死链、404/410 正确、Lighthouse ≥ 90、sitemap 可抓取"。
3. **`redirects` 表保留**（未来改 slug 用），管理端 CRUD 保留，v1 初始为空，不做批量导入。
4. **`.gitignore` 仍必须修**（A8）：把 `docs` 改成只忽略 `docs/private/`，否则契约与 ADR 依旧不进库。
5. 首次上线内容由你在后台手写或粘贴 Markdown，**不需要**任何批量导入路径。

---

### 9.7 合并进 `agent.md` 的最终清单（覆盖 §8）

1. 文件头加 `version: 1.1 / updated: 2026-10-18 / changelog`。
2. §1 C3 改为"宿主 127.0.0.1 绑定 + 容器内 0.0.0.0 监听"（A9）；§2 补健康检查只在 8641（A10）。
3. §3 换成 §9.1.1 的 Go 树 + §9.3 的前端树；分层规则换成 §9.1.2 的 8 条；`AGENTS.md` 统一为 `docs/agent.md`（A8）。
4. §4 补 `link_checks`、`pages`、`preview_tokens.used_at`、`cleanup` 相关；`stats_daily.date` 明确 `Asia/Shanghai`；表数量改用 migration 版本（A1/A2/A4/A7/A13）。
5. §5 整节换成 §9.2：GVA 信封、数字错误码、`page/pageSize`、补 4 个端点、去掉 `{error}` 信封（A3/A5/A6/A7/M8）。
6. §6 补"`uploading` 为内存态不落库"（A12）；补"归档/删除对外 410"（M11）。
7. §7 换成 GVA 端口径：PRAGMA 走 DSN、`cache_size=-16384`、`TrustedProxies` 用 docker CIDR、service 不接 gin.Context（R4/R5/9.1.5）。
8. §8 换成 §9.3；补登录态守卫、CSRF 下发、`lib/api/fetcher` 统一错误（M7/M8）。
9. §9 安全：CSP 改 nonce、EXIF 剥离、HEIC 拒绝（R3/R7）。
10. §10 改为"归档后本地保留 30 天"、"变体不超过原图尺寸"（A14）。
11. §11 补 slug 生成规则、KaTeX 方案待定项标注（R6、Q6）。
12. §12 补 ETag 为响应体哈希（R8）。
13. §13 增加 `cleanup` 任务与失败通知（A2/M6）。
14. §14 补 TLS 续期、防火墙、SSH 加固；备份含原图（M5/A14）。
15. §15 修正 SQLite 内存预算（R4）。
16. §16 按全量重排断言，补"两字词可搜""预览可用""cleanup 生效""410"；D11 改为校对（9.6）。
17. §17 增加"已批准依赖白名单"（6.3）。
18. §18 换成 §9.5 的 20 天表（或 A 方案的 12 天表，二选一）。
19. §19 换成 §5.3 + §9.4 的提交规范；补文档版本化。
20. 新增 §21：`docs/runbook.md`、`docs/standards/*`、`docs/api.md` 的维护要求。
21. 文末变更记录：`1.0 冻结 → 1.1 评审修订（A1–A14, R1–R8, M1–M14, 三份规范）`。

---

## 10. 已确认（2026-10-18）

1. **§9.1.5 的两处偏离**：接受。HTTP 状态码按错误码映射；`service` 只接收 `context.Context`。
2. **§9.5 的排期**：20 天，全量，不砍功能。
3. **数学公式（Q6）**：v1 做，**客户端渲染**（`<span class="math" data-tex>` + 懒加载 KaTeX）。

> 以上已合入 [docs/agent.md](agent.md) v1.1。§1–§9 是评审时的原始建议，保留原样；§10–§11 是确认与追记。

## 11. 合并后新发现的一条实现约束

**CGO 关闭 + 纯 Go 编 WebP 没有标准库方案**：`golang.org/x/image/webp` 只支持解码。需要 `gen2brain/webp` 的 wazero 后端（或等价方案），已写入 `agent.md` §7.1 的 D1 必验清单（V3）；若验证失败，退路是降级 JPEG 三档或独立转码容器（后者违反 C1，必须重新评审）。

