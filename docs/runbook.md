# 运行手册（Runbook）

> 覆盖范围：D1–D3（骨架、迁移、双 listener、鉴权）。备份/恢复、告警、证书续期在 D16–D17 补齐。
> 契约以 [`agent.md`](agent.md) 为准；本文只写"怎么做"。

## 1. 拓扑与端口

| 端口 | 谁在听 | 谁能访问 | 用途 |
| --- | --- | --- | --- |
| `80/443` | 宿主 nginx | 公网 | TLS 入口、`/uploads/` 直出、反代 `127.0.0.1:8640` |
| `127.0.0.1:8640` | Go 公开边缘 | 仅宿主 | `/api/*` 本地处理，其余转 Next :3000 |
| `127.0.0.1:8641` | Go 管理边缘 | 仅宿主 / SSH 隧道 | `/api/admin/*`、`/api/internal/*`、`/healthz`、`/readyz`、`/admin*` |
| `127.0.0.1:3000` | Next | 仅容器内 | 页面渲染，只被 Go 反代 |
| `3065` | **既有服务** | — | 与本项目无关，任何改动都禁止 |

容器内进程监听 `:8640`/`:8641`（即 `0.0.0.0`），由 compose 映射到宿主 `127.0.0.1`（C3）。

## 2. 首次部署

```bash
# 1) 主机准备（Ubuntu 24.04）：docker + compose plugin、ufw 只开 22/80/443
sudo ufw allow 22,80,443/tcp
sudo ufw enable

# 2) 取代码
git clone git@github.com:unicornfairy864/blog-zjut-si.git /srv/blog
cd /srv/blog/deploy

# 3) 首次启动配置（.env 已被 gitignore）
cp .env.example .env
vi .env      # 填 BLOG_ADMIN_USERNAME / BLOG_ADMIN_PASSWORD（≥16 位）

# 4) 数据目录（bind mount，见 compose.yaml）
#    容器里 blogd 以 node(uid 1000) 运行，属主必须是 1000，否则 SQLite 无法建库、
#    启动时会直接报「创建上传目录失败 / 创建附件目录失败」。
#    backup 目录必须与 uploads/files 在同一个文件系统上（备份用硬链接）。
sudo mkdir -p /data/blog/db /data/blog/uploads /data/blog/files /data/blog/backup
sudo chown -R 1000:1000 /data/blog

# 5) 让宿主 nginx 能读图片（附件不直出，不需要这一步）；详见 §11.1
sudo setfacl -R  -m u:www-data:rX /data/blog/uploads
sudo setfacl -R -d -m u:www-data:rX /data/blog/uploads

# 6) 构建并启动（首次会构建 Next 与 Go，约数分钟）
docker compose up -d --build

# 7) 确认两个进程都在
docker compose ps
docker compose logs -f --tail=100
```

> 本机 Windows 上 `pnpm build` 若报 `EPERM: symlink`，是 standalone 产物需要软链而系统未开
> 开发者模式；只做本地验证时用 `NEXT_DISABLE_STANDALONE=1 pnpm build`。容器内构建不受影响。

启动流程（进程内顺序）：配置 → 日志 → 打开 SQLite 并推进迁移 → 读取 `visitor_salt` →（库为空时）创建首个管理员 → 两条 listener + 调度器。

首个管理员只在 `admins` 表为空且两个环境变量都非空时创建；口令只以 argon2id 哈希入库，不写日志。建好后建议把 `.env` 里的 `BLOG_ADMIN_PASSWORD` 删掉并 `docker compose up -d` 让容器重建。

## 3. 访问管理端

管理端永不暴露公网。二选一：

```bash
# 方式 A：只转发端口，浏览器访问 http://localhost:8641/admin/login
ssh -N -L 8641:127.0.0.1:8641 user@your-host

# 方式 B：走 SSH 的 SOCKS（适合想同时打开浏览器 DevTools 的场景）
ssh -N -D 1080 user@your-host
```

- 浏览器请固定用 `http://localhost:8641`。`localhost` 与 `127.0.0.1` 在 cookie 作用域上是两个 host，混用会表现为"刚登录又回到登录页"。
- 登录页地址是 `/admin/login`；未登录访问 `/admin` 会先请求 `GET /api/admin/auth/me`，
  拿到 `code 1000`（HTTP 200）后跳回登录页。

## 4. 迁移

- 默认由进程启动时自动推进（`db.auto_migrate=true`，脚本 `//go:embed` 进二进制）。
- 需要单独推进（预发验证、回滚前）时：

```bash
# 容器内直接跑二进制，绕过 s6
docker compose run --rm --entrypoint /usr/local/bin/blogd \
  -e BLOG_MIGRATE_ONLY=1 blog

# 本机开发
cd apps/server && BLOG_MIGRATE_ONLY=1 go run .
```

- 迁移记录在 `schema_migrations`（`version` + `dirty`）。**出现 `dirty` 时进程会拒绝启动**：先恢复备份，再人工确认。
- 回滚策略：只回滚应用到上一版本 + 恢复数据库备份；不手改 schema（`AutoMigrate` 全程禁止）。
- 新增变更必须新增 `migrations/NNNNNN_name.{up,down}.sql`，不得修改历史迁移。

## 5. 健康检查与日志

```bash
curl -s http://127.0.0.1:8641/healthz          # 进程活着
curl -s http://127.0.0.1:8641/readyz | jq      # DB / 磁盘 / uploads / 队列
docker inspect --format '{{json .State.Health}}' zjut-blog | jq
```

`readyz` 的四项：`database`（Ping）、`disk`（<85%）、`uploads`（可写）、`queue`（pending <100）。任一项失败返回 503，容器健康检查随之失败并重启。

日志是 JSON 到 stdout，关键字段：`ts / level / msg / request_id / method / path / status / dur_ms / ip_hash`。排查时先按 `request_id` 聚合：

```bash
docker compose logs blog | jq -c 'select(.request_id=="<id>")'
```

## 6. 备份与恢复

`backup` 任务（D14 已落地，每日 03:00 / 可在 `/admin/jobs` 手动投递）会做 `VACUUM INTO` 与
`uploads`、`files` 两个目录的硬链接快照，见 §11.4。下面是不依赖任务的手工兜底：

```bash
# 备份：先停容器，避免 WAL 半写
cd /srv/blog/deploy && docker compose stop blog
sudo cp -a /data/blog/db/blog.db        /backup/blog-$(date +%F).db
sudo cp -a /data/blog/uploads           /backup/uploads-$(date +%F)
sudo cp -a /data/blog/files             /backup/files-$(date +%F)
docker compose start blog

# 恢复
cd /srv/blog/deploy && docker compose stop blog
sudo cp -a /backup/blog-2026-10-18.db   /data/blog/db/blog.db
sudo rm -f /data/blog/db/blog.db-wal /data/blog/db/blog.db-shm
docker compose start blog
```

**说明**：`blog.db` 里的 `post_attachments.storage_key` / `media.storage_key` 只是相对路径，
恢复时必须把 `uploads` 与 `files` 一起还原，否则管理端存储页会把它们全部报成"缺失文件"。

## 7. 环境变量

前缀 `BLOG_`，层级用下划线。全部有默认值（见 `apps/server/config.yaml`）。

| 变量 | 默认 | 说明 |
| --- | --- | --- |
| `BLOG_SERVER_PUBLIC` | `:8640` | 公开边缘监听地址 |
| `BLOG_SERVER_ADMIN` | `:8641` | 管理边缘监听地址 |
| `BLOG_WEB_UPSTREAM` | `http://127.0.0.1:3000` | Next 地址 |
| `BLOG_DB_PATH` | `./data/blog.db` | SQLite 文件 |
| `BLOG_DB_MAX_OPEN_CONNS` | `4` | 连接池（×16MB 缓存） |
| `BLOG_DB_AUTO_MIGRATE` | `true` | 启动时推进迁移 |
| `BLOG_UPLOAD_DIR` | `./data/uploads` | 图片目录（原图 + 四档变体） |
| `BLOG_UPLOAD_MAX_BYTES` | `31457280` | 单张图片上限（30MB） |
| `BLOG_FILE_DIR` | `./data/files` | 附件目录（`{sha[:2]}/{sha256}{ext}`），不能与 `BLOG_UPLOAD_DIR` 相同 |
| `BLOG_FILE_MAX_BYTES` | `20971520` | 单个附件上限（20MB） |
| `BLOG_AUTH_SESSION_TTL_HOURS` | `168` | 会话有效期（滑动） |
| `BLOG_AUTH_COOKIE_SECURE` | `false` | 隧道走 http，保持 false |
| `BLOG_LOG_LEVEL` | `info` | `debug`/`info`/`warn`/`error` |
| `BLOG_TASK_ENABLED` | `true` | 调度器开关 |
| `BLOG_UPLOAD_DIR` | `./data/uploads` | 图片目录（原图 + 四档变体）；容器里 compose 覆盖为 `/data/blog/uploads` |
| `BLOG_FILE_DIR` | `./data/files` | 附件目录；容器里 compose 覆盖为 `/data/blog/files` |
| `BLOG_BACKUP_DIR` | `./data/backup` | `backup` 任务的落点：`blog-YYYYMMDD.db` + `uploads-YYYYMMDD/` + `files-YYYYMMDD/`；容器里 compose 覆盖为 `/data/blog/backup`，必须与 uploads/files 同一个文件系统 |
| `BLOG_CONFIG` | 空 | 显式指定配置文件 |
| `BLOG_MIGRATE_ONLY` | 空 | 设为 `1` 时只迁移后退出 |
| `BLOG_ADMIN_USERNAME` / `BLOG_ADMIN_PASSWORD` | 空 | 首次引导创建管理员 |

## 8. 常见故障

| 现象 | 排查 |
| --- | --- |
| 页面显示 `{"code":1006,"msg":"页面服务暂不可用"}`（**HTTP 200**） | Next 没起来：`docker compose logs blog \| grep web`；或 `BLOG_WEB_UPSTREAM` 写错。页面转发失败也是 200，要看响应体 |
| 8641 连接被拒绝 | compose 的 ports 是否仍是 `127.0.0.1:8641`；隧道是否建立 |
| 登录返回 `code 2005`（HTTP 200） | 触发锁定（5 次失败/10 分钟 → 锁 30 分钟）。**锁定判定先于限流，所以第 6 次就直接是 2005**；等 30 分钟自动解锁，不要手改库 |
| 登录返回 `code 1005`（HTTP 200） | 同用户名 5 次/分钟限流（账号还没到锁定阈值时才会出现），稍等再试 |
| 写操作返回 `code 1001`（HTTP 200），`msg` 为 CSRF 校验失败 | 刷新页面重新 `GET /auth/me` 取 token；确认没有用 `127.0.0.1` 与 `localhost` 混访 |
| 一直跳回登录页 | cookie 被浏览器拦截（无痕/第三方策略）、host 不一致、或系统时间偏差超过 7 天 |
| 容器反复重启 | 看 `dirty` 是否非空、端口是否被占用、`/data` 权限是否可写 |
| 容器启动即退出，日志 `加载配置失败` + `创建上传/附件目录失败: mkdir ./data: permission denied` | 数据目录没建或属主不是 1000：`sudo mkdir -p /data/blog/{db,uploads,files,backup} && sudo chown -R 1000:1000 /data/blog`；确认 compose 的 `BLOG_*_DIR` 都是 `/data/blog/...` 绝对路径 |
| 容器起来了但 `backup` 任务失败，日志 `创建备份目录失败` | 备份目录只在任务执行时创建，不影响启动：`sudo mkdir -p /data/blog/backup && sudo chown 1000:1000 /data/blog/backup` |
| `readyz` 503 | 看 `checks` 数组里哪一项 false：磁盘满 / uploads 不可写 / 队列积压 |

## 9. 发布与回滚

1. 合并到 `main`（squash，标题按 Conventional Commits）。
2. `docker compose up -d --build`，观察 `logs -f` 与 `readyz`。
3. 回滚：`git checkout <上一个 tag>` → `docker compose up -d --build`；若涉及数据变更，同时恢复备份。

## 10. 证书与 nginx（D16 落地）

`deploy/nginx/blog.conf` 已给出 80/443 双 server 块与 `certbot` 续期路径；人工步骤：

```bash
sudo cp deploy/nginx/blog.conf /etc/nginx/sites-available/blog.conf
sudo cp deploy/nginx/blog_proxy_params /etc/nginx/conf.d/blog_proxy_params
sudo ln -sf /etc/nginx/sites-available/blog.conf /etc/nginx/sites-enabled/blog.conf
sudo nginx -t && sudo systemctl reload nginx
sudo certbot --nginx -d blog.zjut.si        # 首次
sudo certbot renew --dry-run                # 每月演练
```

## 11. 本地存储：图片与附件

**没有图床**：v1.13 起 Cloudreve / WebDAV 集成已删除，图片与附件都是本机磁盘上的**唯一副本**，
所以备份必须覆盖这两个目录（§11.5），空间回收只能在 `/admin` 里手动做。

| 内容 | 容器内路径 | 宿主路径 | 对外出口 |
| --- | --- | --- | --- |
| 图片（原图 + 四档变体） | `/data/blog/uploads` | 同名 bind mount | 宿主 nginx `location /uploads/` 直出；8641 上由 Go 直出（隧道预览用） |
| 附件 | `/data/blog/files` | 同名 bind mount | 只经 `GET /api/attachments/{id}/go`，**不做静态直出** |

### 11.1 目录与权限（部署时做一次）

```bash
sudo mkdir -p /data/blog/db /data/blog/uploads /data/blog/files /data/blog/backup

# 容器以 uid 1000 写文件（目录 0750、文件 0640），nginx 默认用户 www-data 读不到
sudo setfacl -R  -m u:www-data:rX /data/blog/uploads
sudo setfacl -R -d -m u:www-data:rX /data/blog/uploads
```

- 只给 **uploads** 放权限就够了：附件不直出，`files/` 不需要 nginx 可读。
- `-d` 是默认 ACL，之后新建的子目录自动继承；不加就得每次新增都重跑一遍。
- 判据：`403` 是目录缺 `x` 位、nginx 穿不过去（连不存在的文件也 403）；`404` 才是路径/文件名写错。
- 没有 acl 支持时退而求其次：`sudo chmod -R a+rX /data/blog/uploads`。
- 验证：

  ```bash
  curl -sI https://blog.zjut.si/uploads/<sha 前两位>/<sha>/1600.webp | head -3   # 期望 200
  ```

### 11.2 附件

- 上限 `BLOG_FILE_MAX_BYTES`（默认 20MB）；扩展名/文件名黑名单见 agent.md §10，命中 → `code 2007`，
  超限 → `code 2008`（都是 HTTP 200）。
- 落盘 `/data/blog/files/{sha[:2]}/{sha256}{ext}`，按内容去重：同一份文件被多篇文章引用时只占一份磁盘，
  删掉记录后若没人再引用才会删文件。
- 下载只走 `GET /api/attachments/{id}/go`：响应固定 `Content-Disposition: attachment` +
  `application/octet-stream` + `nosniff`，并支持 `Range`；所以**不要**给 `/data/blog/files` 加 nginx 直出。
- 自查：

  ```bash
  ls -l /data/blog/files/*/* | head
  curl -sI https://blog.zjut.si/api/attachments/1/go | head -5     # 期望 200 + attachment 头
  curl -s https://blog.zjut.si/api/attachments/9999/go             # 期望 {"code":1002,…}（HTTP 200）
  ```

### 11.3 空间回收与对账（`/admin/storage`）

页面（或 `GET /api/admin/storage`）给出原图 / 变体 / 附件 / 合计占用、已删原图数，以及两张对账清单：

| 清单 | 含义 | 处理 |
| --- | --- | --- |
| 孤儿文件 | 磁盘上有、数据库里没有任何记录引用 | 点「清理孤儿文件」（`POST /api/admin/storage/cleanup`）删掉 |
| 缺失文件 | 数据库里有记录、磁盘上找不到 | **不会自动处理**：多半是手工删过文件或恢复备份不完整。要么补文件，要么在媒体库删掉该记录 |

- **删原图保变体**：媒体库每张图的「删除原图」（`DELETE /api/admin/media/{id}/original`）。
  原图是重新转码的唯一来源，删掉后不能 `retry`、`origUrl` 与「查看原图」链接也会消失，四档变体不受影响。
- 原图与附件**不参与 `cleanup` 自动清理**：它们是唯一副本，回收空间只能走上面两条路径。

### 11.4 从图床时代升级上来（一次性）

`media.orig_archived` 换了语义，升级后必须对一次账，否则老图会全部显示成「原图已删除」：

| 版本 | 语义 |
| --- | --- |
| v1.12 及以前 | `1` = 已推送到 Cloudreve；`cleanup` 会在 30 天后删掉本地原图 |
| v1.13 起 | `1` = 本地原图已被管理端手动删除（变体仍在），此时不给 `origUrl`、也不许 `retry` |

**先停容器再改库**（避免 WAL 半写），然后：

```bash
sqlite3 /data/blog/db/blog.db <<'SQL'
-- 1) 清掉图床时代的归档标记；真正的原图还在不在由下一步对账
UPDATE media SET orig_archived = 0, orig_archived_at = NULL;
-- 2) 图床时代的 media_archive 任务已经没有 handler，直接删掉（留着会被标成"未知任务类型"）
DELETE FROM jobs WHERE type = 'media_archive';
SQL
docker compose start
```

改完打开 `/admin/storage`，看「缺失文件」里 `kind=original` 的条目——这些图的原图在旧
`cleanup` 里已经被删掉了（图床也已下线，找不回来）：

- 想保留文章里的图：四档变体还在，正文与列表照常显示，只是没有「查看原图」；
- 想让状态一致：在媒体库对它们点「删除原图」（幂等，会把 `orig_archived` 打回 `1`），或直接删掉整张图。

### 11.5 备份

`backup` 任务（每日 03:00，站点时区；也可在 `/admin/jobs` 手动投递一次）产出：

```
$BLOG_BACKUP_DIR/blog-YYYYMMDD.db      # VACUUM INTO，保留 30 天
$BLOG_BACKUP_DIR/uploads-YYYYMMDD/     # 图片硬链接快照，保留 90 天
$BLOG_BACKUP_DIR/files-YYYYMMDD/       # 附件硬链接快照，保留 90 天
```

硬链接要求备份目录与数据目录在**同一个文件系统**上，否则会自动退回复制（慢且占空间）。
所以 `BLOG_BACKUP_DIR` 必须留在 `/data/blog` 下（compose 默认 `/data/blog/backup`，是独立 bind mount）。
目录建不出来只影响备份任务本身，不会拦住站点启动；任务日志里有具体原因。
恢复演练见 §16。

---

## 12. 公开端自测（D9–D10 / G4）

容器起来后（`docker compose up -d`），先在容器里打接口，再上浏览器看版式：

```bash
# 1) 分页：第 2 页与第 1 页内容不同，total 一致
curl -s 'http://127.0.0.1:8640/api/posts?page=1&pageSize=10' | head -c 300
curl -s 'http://127.0.0.1:8640/api/posts?page=2&pageSize=10' | head -c 300

# 2) 标签组合：/tags 里的 count 与该标签详情页的 total 一致
curl -s http://127.0.0.1:8640/api/tags
curl -s http://127.0.0.1:8640/api/tags/<slug>

# 3) 归档按年/月分组，条目数与 /api/site 的 counts.posts 一致
curl -s http://127.0.0.1:8640/api/archive | head -c 400

# 4) 404 / 410 看的是 HTML 层（不是 API 层）
curl -sI http://127.0.0.1:8640/posts/不存在的slug | head -1    # 期望 404
curl -sI http://127.0.0.1:8640/posts/<已归档slug> | head -1    # 期望 410

# 5) srcset 生效：详情 HTML 里图片带 4 档候选
curl -s http://127.0.0.1:8640/posts/<slug> | grep -o 'srcset="[^"]*"' | head -2

# 6) 系列 / 友链 / 关于（静态页）
curl -s http://127.0.0.1:8640/api/series
curl -s http://127.0.0.1:8640/api/links
curl -s http://127.0.0.1:8640/api/pages/about
```

浏览器侧：DevTools 切到 375px（iPhone SE），逐页确认**没有横向滚动条**；正文图片点开应打开本地原图
（`/uploads/{sha 前两位}/{sha}/original.jpg`，由 nginx 直出）。

### 12.1 测试数据从哪来

标签 / 系列 / 友链 / 静态页的管理端界面还没做（排在 D13–D14），D9–D10 验收时先用 sqlite3
直接塞数据。**先停容器**（避免 WAL 半写），改完再起：

```bash
cd /srv/blog/deploy && docker compose stop
sqlite3 /data/blog/db/blog.db <<'SQL'
-- 标签：给一篇文章挂上 Go 标签
INSERT INTO tags (name, slug, description, created_at)
  VALUES ('Go', 'go', '', strftime('%s','now'));
INSERT INTO post_tags (post_id, tag_id)
  VALUES ((SELECT id FROM posts WHERE status='published' ORDER BY published_at DESC LIMIT 1),
          (SELECT id FROM tags WHERE slug='go'));

-- 系列：把同一篇文章放进系列
INSERT INTO series (name, slug, description, created_at)
  VALUES ('从零写博客', 'blog-from-scratch', '', strftime('%s','now'));
UPDATE posts
   SET series_id = (SELECT id FROM series WHERE slug='blog-from-scratch'), series_order = 1
 WHERE id = (SELECT id FROM posts WHERE status='published' ORDER BY published_at DESC LIMIT 1);

-- 友链（status 只有 active / hidden，默认 active 即公开可见）
INSERT INTO links (name, url, avatar, description, sort, status, created_at)
  VALUES ('示例友链', 'https://example.com', '', '测试用', 1, 'active', strftime('%s','now'));

-- 静态页 /about
INSERT INTO pages (slug, title, content_md, content_html, content_text,
                   seo_title, seo_description, created_at, updated_at)
  VALUES ('about', '关于',
          '## 关于本站',
          '<h2 id="about">关于本站</h2><p>本站是个人技术博客。</p>',
          '关于本站 本站是个人技术博客。', '', '',
          strftime('%s','now'), strftime('%s','now'));
SQL
docker compose start
```

塞完数据后 `curl -s http://127.0.0.1:8640/api/tags` 应能看到 `go`，`/about` 页面也能打开。

---

## 13. 评论自测（D11 / G5 的评论部分）

设已发布文章的 slug 为 `<slug>`，容器在跑。公开接口直接打 8640，管理端审核建议开隧道用浏览器
（`http://localhost:8641/admin/comments`）。

```bash
# 1) 发一条评论 → 一律 pending
curl -s -X POST http://127.0.0.1:8640/api/posts/<slug>/comments \
  -H 'Content-Type: application/json' \
  -d '{"authorName":"测试","contentMd":"第一条评论"}'
# 期望：{"code":0,"data":{"id":1,"status":"pending","createdAt":…},"msg":"操作成功"}

# 2) pending 不进公开列表
curl -s http://127.0.0.1:8640/api/posts/<slug>/comments      # total = 0

# 3) 管理端通过后再看 → total = 1，且列表里没有邮箱/ip_hash/ua
#    浏览器：/admin/comments → 待审核 → 通过
curl -s http://127.0.0.1:8640/api/posts/<slug>/comments

# 4) 嵌套：回复顶层评论（parentId=1），通过后出现在该评论的 replies 里（固定两层）
curl -s -X POST http://127.0.0.1:8640/api/posts/<slug>/comments \
  -H 'Content-Type: application/json' \
  -d '{"authorName":"乙","contentMd":"回复","parentId":1}'

# 5) 限流：连发 4 条，第 4 条的响应体 code 应为 1005（3 次 / 10 分钟 / IP）
for i in 1 2 3 4; do
  curl -s -X POST http://127.0.0.1:8640/api/posts/<slug>/comments \
    -H 'Content-Type: application/json' -d '{"authorName":"限流","contentMd":"x"}' \
    | grep -o '"code":[0-9-]*'
done; echo

# 6) 关闭评论的文章 → 响应体 code 1001（HTTP 200）
#    目前只能用接口或 SQL 关：UPDATE posts SET allow_comments = 0 WHERE slug = '<slug>';
#    （管理端编辑器的「允许评论」开关排在 D14 管理端补全里一起做）

# 7) 标 spam 后从公开列表消失；标 deleted 是软删，管理端 status=deleted 仍能查到
```

XSS 顺带验一下：正文里带 `<script>alert(1)</script>` 的评论，入库后的 `contentHtml` 应已被过滤
（和文章正文同一套 goldmark + bluemonday）。

限流是**进程内令牌桶**（`utils.NewKeyedLimiter`），重启容器即清零 —— 单实例部署够用，不是缺陷。

---

## 14. 互动与统计自测（D12 / G5 的点赞与 PV 部分）

设已发布文章的 slug 为 `<slug>`，容器在跑。访客标识用请求头 `X-Visitor-Id`（8–64 位
`[A-Za-z0-9_-]`）；**不带这个头时服务端按 `ip|ua` 兜底**，curl 连续打会被当成同一个访客。

```bash
V='X-Visitor-Id: test-visitor-0001'

# 1) 点赞幂等：连点两次，likeCount 只 +1
curl -s -X POST -H "$V" http://127.0.0.1:8640/api/posts/<slug>/like   # {"liked":true,"likeCount":1}
curl -s -X POST -H "$V" http://127.0.0.1:8640/api/posts/<slug>/like   # 仍为 1

# 2) 取消点赞同样幂等
curl -s -X DELETE -H "$V" http://127.0.0.1:8640/api/posts/<slug>/like # {"liked":false,"likeCount":0}
curl -s -X DELETE -H "$V" http://127.0.0.1:8640/api/posts/<slug>/like # 仍为 0

# 3) 访客态读是 no-store（不能进任何缓存）
curl -sI -H "$V" http://127.0.0.1:8640/api/posts/<slug>/stats | grep -i cache-control

# 4) 点赞限流 10 次 / 分钟 / visitor：第 11 次的响应体 code 为 1005
for i in $(seq 1 11); do
  curl -s -X POST -H "$V" http://127.0.0.1:8640/api/posts/<slug>/like | grep -o '"code":[0-9-]*'
done; echo

# 5) PV：同 visitor 10 分钟内只 +1，另一个 visitor 再 +1
BODY='{"path":"/posts/<slug>","slug":"<slug>","referrer":"https://example.com/x"}'
curl -s -X POST -H 'Content-Type: application/json' -H "$V" -d "$BODY" http://127.0.0.1:8640/api/events/view
# 期望 {"recorded":true,"viewCount":1}
curl -s -X POST -H 'Content-Type: application/json' -H "$V" -d "$BODY" http://127.0.0.1:8640/api/events/view
# 期望 {"recorded":false,"viewCount":1}
curl -s -X POST -H 'Content-Type: application/json' -H 'X-Visitor-Id: test-visitor-0002' \
  -d "$BODY" http://127.0.0.1:8640/api/events/view
# 期望 {"recorded":true,"viewCount":2}

# 6) 非文章页只进全站行（viewCount 为 null）
curl -s -X POST -H 'Content-Type: application/json' -H "$V" -d '{"path":"/"}' \
  http://127.0.0.1:8640/api/events/view

# 7) 打点字段校验：path 必须以 / 开头，否则响应体 code 1003（HTTP 200）
curl -s -X POST -H 'Content-Type: application/json' \
  -d '{"path":"no-slash"}' http://127.0.0.1:8640/api/events/view | grep -o '"code":[0-9-]*'
```

落库口径用 sqlite3 对一下（**先停容器**再读，避免 WAL 半写）：

```bash
sqlite3 /data/blog/db/blog.db "
SELECT date, post_id, pv, uv FROM stats_daily ORDER BY date, post_id;   -- 应同时有 post_id=0 与文章行
SELECT post_id, path, referrer_host, length(visitor_hash) FROM page_views ORDER BY id DESC LIMIT 5;
SELECT post_id, count(*) FROM likes GROUP BY post_id;"
```

管理端看板（隧道里 `http://localhost:8641/admin/stats`）应能对上：

```bash
curl -s http://127.0.0.1:8641/api/admin/stats/overview     # 需带会话 cookie
curl -s 'http://127.0.0.1:8641/api/admin/stats/daily?from=2026-10-01&to=2026-10-19'
```

- `views.today` / `uvToday` 是 `stats_daily` 当天全站行；`uvToday` 是**去重访客数**（同一天同一 visitor 只算一个）。
- 日期都是 `Asia/Shanghai`，容器时区无关；跨日验证可以把系统时间调过当地 00:00 再看 `date` 是否翻页。
- 点赞限流与 PV 去重都在进程内，重启容器清零；`page_views` 明细由 `cleanup` 保留 90 天。

---

## 15. 发布控制自测（D13 / G6）

隧道里打开 `http://localhost:8641/admin/posts/{id}`，或直接用接口（记得带 `X-CSRF-Token`）。

```bash
# 1) 定时发布：publishedAt 必须是未来时间
NOW=$(date +%s); AT=$((NOW + 180))
curl -s -X POST http://127.0.0.1:8641/api/admin/posts/<id>/schedule \
  -H 'Content-Type: application/json' -H "X-CSRF-Token: $CSRF" \
  -d "{\"publishedAt\":$AT,\"note\":\"定时\"}"          # status=scheduled，published_at=$AT
curl -s 'http://127.0.0.1:8641/api/admin/jobs?status=pending'   # 应有一条 publish_scheduled，run_at=$AT
curl -s -X POST ... -d "{\"publishedAt\":$NOW}"                 # 过去时间 → code 1003（HTTP 200）field=publishedAt

# 2) 到点生效（误差 <90s）：等 3 分钟后再看
sleep 200
curl -s http://127.0.0.1:8640/api/posts/<slug> | head -c 200    # 200：已发布
curl -s 'http://127.0.0.1:8641/api/admin/jobs?status=done'      # publish_scheduled 已 done

# 3) 取消定时：重新排一条，再 unpublish，等到点后文章仍是草稿
#    （unpublish / PATCH 回 draft 都会撤掉待执行任务；jobs 里该条会消失）

# 4) 版本回滚：改正文 → 发布 → 回滚到上一条 revision
curl -s http://127.0.0.1:8641/api/admin/posts/<id>/revisions    # data.list[]，按 revisionNo 倒序
curl -s -X POST http://127.0.0.1:8641/api/admin/posts/<id>/revisions/<rid>/restore \
  -H 'Content-Type: application/json' -H "X-CSRF-Token: $CSRF" -d '{}'
# 期望：正文回到旧版本，且 revision 号 +1（历史不被改写，列表多出一条「回滚到版本 N」）

# 5) 预览链接：一次性、24h
curl -s -X POST http://127.0.0.1:8641/api/admin/posts/<id>/preview-link \
  -H 'Content-Type: application/json' -H "X-CSRF-Token: $CSRF" -d '{}'
# → {"token":"…","url":"/preview/…","expiresAt":…}
curl -s -i http://127.0.0.1:8640/api/preview/<token> | head -3   # 200 + Cache-Control: no-store
curl -s http://127.0.0.1:8640/api/preview/<token> | grep -o '"code":[0-9-]*'  # 2006（已使用，HTTP 200）
curl -s http://127.0.0.1:8640/api/preview/deadbeef | grep -o '"code":[0-9-]*' # 1002（形状不对，HTTP 200）
# 隧道里点开预览地址也能看：http://localhost:8641/preview/<token>（8641 也转 /preview*）
# 再次生成会作废该文旧的未使用链接
```

`code 2006` 的另一种构造方式（不用真等 24h）：停容器后

```bash
sqlite3 /data/blog/db/blog.db "UPDATE preview_tokens SET expires_at = strftime('%s','now') - 10;"
```

## 16. 清理与备份自测（D13 / G6 的 cleanup 断言）

周期任务按站点时区在 03:00（backup）/ 04:00（cleanup）/ 周一 05:00（link_check）自动入队；
**判据是 jobs 表当天是否已有同类型记录**，所以重启不会重复跑，也不会漏跑。
自测时不用等时刻点，直接手动投递：

```bash
# 手动投递（白名单只有 cleanup / backup / link_check）
curl -s -X POST http://127.0.0.1:8641/api/admin/jobs \
  -H 'Content-Type: application/json' -H "X-CSRF-Token: $CSRF" -d '{"type":"cleanup"}'
```

先构造"过期数据"，再投递 `cleanup`，然后核对被删掉（**先停容器**再改库，避免 WAL 半写）：

```bash
sqlite3 /data/blog/db/blog.db <<'SQL'
-- 1) 回收站里 31 天前的文章（挑一篇带本地附件的，物理删除后附件文件也该消失）
UPDATE posts SET deleted_at = strftime('%s','now') - 31*86400 WHERE id = <某篇>;
-- 2) 91 天前的访问明细
UPDATE page_views SET created_at = strftime('%s','now') - 91*86400 WHERE id = (SELECT max(id) FROM page_views);
-- 3) 已过期的会话与预览链接
UPDATE sessions SET expires_at = strftime('%s','now') - 10;
UPDATE preview_tokens SET expires_at = strftime('%s','now') - 10;
SQL
docker compose start
```

投递 cleanup 后（`jobs` 里能看到该条的 `attempts`/`status`）：

```bash
sqlite3 /data/blog/db/blog.db "
SELECT count(*) FROM posts WHERE id = <某篇>;                 -- 0（已物理删除）
SELECT count(*) FROM page_views WHERE created_at < strftime('%s','now') - 90*86400;  -- 0
SELECT count(*) FROM sessions WHERE expires_at < strftime('%s','now');               -- 0
SELECT count(*) FROM preview_tokens WHERE expires_at < strftime('%s','now');         -- 0"
ls /data/blog/uploads/<sha 前两位>/<sha>/original.*    # 仍在：原图不参与自动清理（§10）
ls /data/blog/files/<被删文章的 storage_key>            # 已删除：没人再引用
```

备份（`backup` 任务，落点 `BLOG_BACKUP_DIR`，默认 `./data/backup`）：

```bash
curl -s -X POST http://127.0.0.1:8641/api/admin/jobs \
  -H 'Content-Type: application/json' -H "X-CSRF-Token: $CSRF" -d '{"type":"backup"}'
ls -lh /data/blog/backup/   # blog-YYYYMMDD.db + uploads-YYYYMMDD/ + files-YYYYMMDD/（硬链接快照）
```

- 库保留 30 天、图片与附件快照保留 90 天，超期由下一次 `backup` 自己清掉。
- `VACUUM INTO` 出来的 `.db` 可直接 `sqlite3 blog-YYYYMMDD.db .tables` 验证；恢复步骤见 §9。

---

## 17. 内容管理自测（D14）

管理端（隧道 `http://localhost:8641`）逐页过一遍：`/admin/tags`、`/admin/series`、`/admin/links`、
`/admin/redirects`、`/admin/attachments`、`/admin/jobs`、`/admin/audit-logs`、`/admin/settings`。

```bash
# 1) 分类：新建 → 改名 → 删除；中文名不填 slug 响应体 code 1003，slug 撞车 code 2001（都是 HTTP 200）
curl -s -X POST http://127.0.0.1:8641/api/admin/tags -H 'Content-Type: application/json' \
  -H "X-CSRF-Token: $CSRF" -d '{"name":"Go","slug":"go","description":""}'
curl -s -X DELETE http://127.0.0.1:8641/api/admin/tags/<id> -H "X-CSRF-Token: $CSRF"
#    删除只解除关联，不删文章（`/admin/tags` 里 count 会变 0）

# 2) 跳转：建一条老链接 → 公开端直接 301 且 hits+1
curl -s -X POST http://127.0.0.1:8641/api/admin/redirects -H 'Content-Type: application/json' \
  -H "X-CSRF-Token: $CSRF" -d '{"fromPath":"/posts/old-slug","toPath":"/posts/new-slug","status":301}'
curl -s -o /dev/null -w '%{http_code} %{redirect_url}\n' http://127.0.0.1:8640/posts/old-slug   # 301 → /posts/new-slug

# 3) 附件：本地上传 → 公开 /go 直接下发且 clicks+1
curl -s -X POST http://127.0.0.1:8641/api/admin/attachments/upload \
  -H "X-CSRF-Token: $CSRF" \
  -F "postId=<id>" -F "title=源码" -F "file=@/tmp/src.zip"
curl -sI http://127.0.0.1:8640/api/attachments/<id>/go | head -6
#    200 + Content-Disposition: attachment + Content-Type: application/octet-stream + nosniff
curl -s -X POST http://127.0.0.1:8641/api/admin/attachments/upload \
  -H "X-CSRF-Token: $CSRF" -F "postId=<id>" -F "file=@/tmp/shell.php" \
  | grep -o '"code":[0-9-]*'                                             # 期望 2007（HTTP 200）
# 外链附件仍可登记（JSON，shareUrl 必填），/go 对它 302
curl -s -X POST http://127.0.0.1:8641/api/admin/attachments -H 'Content-Type: application/json' \
  -H "X-CSRF-Token: $CSRF" \
  -d '{"postId":<id>,"title":"外链","shareUrl":"https://example.com/src.zip","fileName":"src.zip","bytes":1024,"sort":0}'

# 4) 存储管理：/admin/storage 应显示原图/变体/附件占用；孤儿清单可一键清掉
curl -s http://127.0.0.1:8641/api/admin/storage | head -c 400

# 4) 设置：改时区 → content_version+1（公开读的 ETag 随之失效）
curl -s -X PUT http://127.0.0.1:8641/api/admin/settings -H 'Content-Type: application/json' \
  -H "X-CSRF-Token: $CSRF" -d '{"siteTimezone":"Asia/Shanghai","imgBaseUrl":""}'
curl -s http://127.0.0.1:8640/api/site | head -c 200       # contentVersion 应比改之前大
curl -s -X PUT ... -d '{"siteTimezone":"Mars/Olympus"}'    # code 1003（HTTP 200）
```

- `/admin/audit-logs` 应能看到上面每一步的动作（`tag.create`、`redirect.create`、`attachment.upload`、`settings.update`…）。
- 设置接口**永不返回** `visitor_salt`；`/admin/settings` 里只回显目录、上限与任务周期。

