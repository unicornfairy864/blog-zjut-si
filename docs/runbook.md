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
#    容器里 blogd 以 node(uid 1000) 运行，属主必须是 1000，否则 SQLite 无法建库。
sudo mkdir -p /data/blog/db /data/blog/uploads
sudo chown -R 1000:1000 /data/blog

# 5) 构建并启动（首次会构建 Next 与 Go，约数分钟）
docker compose up -d --build

# 6) 确认两个进程都在
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
- 登录页地址是 `/admin/login`；未登录访问 `/admin` 会先请求 `GET /api/admin/auth/me`，401 后跳回登录页。

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

## 6. 备份与恢复（D17 前的临时做法）

D17 会落地 `VACUUM INTO` + uploads `rsync --link-dest` 的定时任务。当前手工步骤：

```bash
# 备份：先停容器，避免 WAL 半写
cd /srv/blog/deploy && docker compose stop blog
sudo cp -a /data/blog/db/blog.db        /backup/blog-$(date +%F).db
sudo cp -a /data/blog/uploads           /backup/uploads-$(date +%F)
docker compose start blog

# 恢复
cd /srv/blog/deploy && docker compose stop blog
sudo cp -a /backup/blog-2026-10-18.db   /data/blog/db/blog.db
sudo rm -f /data/blog/db/blog.db-wal /data/blog/db/blog.db-shm
docker compose start blog
```

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
| `BLOG_UPLOAD_DIR` | `./data/uploads` | 图片目录 |
| `BLOG_UPLOAD_MAX_BYTES` | `31457280` | 单文件上限（30MB） |
| `BLOG_AUTH_SESSION_TTL_HOURS` | `168` | 会话有效期（滑动） |
| `BLOG_AUTH_COOKIE_SECURE` | `false` | 隧道走 http，保持 false |
| `BLOG_LOG_LEVEL` | `info` | `debug`/`info`/`warn`/`error` |
| `BLOG_TASK_ENABLED` | `true` | 调度器开关 |
| `BLOG_CONFIG` | 空 | 显式指定配置文件 |
| `BLOG_MIGRATE_ONLY` | 空 | 设为 `1` 时只迁移后退出 |
| `BLOG_ADMIN_USERNAME` / `BLOG_ADMIN_PASSWORD` | 空 | 首次引导创建管理员 |

## 8. 常见故障

| 现象 | 排查 |
| --- | --- |
| 页面 502「页面服务暂不可用」 | Next 没起来：`docker compose logs blog \| grep web`；或 `BLOG_WEB_UPSTREAM` 写错 |
| 8641 连接被拒绝 | compose 的 ports 是否仍是 `127.0.0.1:8641`；隧道是否建立 |
| 登录返回 423 | 触发锁定（5 次失败/10 分钟 → 锁 30 分钟）。等 30 分钟自动解锁；不要手改库 |
| 登录返回 429 | 同用户名 5 次/分钟限流，稍等再试 |
| 写操作 403，`msg` 为 CSRF 校验失败 | 刷新页面重新 `GET /auth/me` 取 token；确认没有用 `127.0.0.1` 与 `localhost` 混访 |
| 一直跳回登录页 | cookie 被浏览器拦截（无痕/第三方策略）、host 不一致、或系统时间偏差超过 7 天 |
| 容器反复重启 | 看 `dirty` 是否非空、端口是否被占用、`/data` 权限是否可写 |
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
