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
| `BLOG_CLOUDREVE_ENABLED` | `false` | 是否启用原图归档 |
| `BLOG_CLOUDREVE_WEBDAV_URL` | 空 | Cloudreve WebDAV 地址（如 `https://pan.example.com/dav`） |
| `BLOG_CLOUDREVE_USERNAME` / `BLOG_CLOUDREVE_PASSWORD` | 空 | WebDAV 账号；口令含 `$` 时在 `.env` 写成 `$$` |
| `BLOG_CLOUDREVE_BASE_PATH` | `/blog` | 远端目录，原图写成 `{base}/{sha256}.{ext}` |
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

## 11. 图床：Cloudreve WebDAV 归档（D8）

本站的图片**不在图床上直接对外提供**：公开站由 nginx 直出本机 `/data/blog/uploads`，
Cloudreve 只保存**原图归档**（§10），用于 30 天后清理本地原图、以及原图长期留存。

### 11.1 Cloudreve 侧

1. 在 Cloudreve 里新建目录 `/blog`（与 `BLOG_CLOUDREVE_BASE_PATH` 对应）。
2. 右上角头像 → **WebDAV 账号**，新建一个账号：
   - 目录权限只勾 `/blog`，不要给整盘；
   - 记下 WebDAV 用户名与密码。
3. WebDAV 地址通常是 `https://<你的域名>/dav`（Cloudreve v3/v4 默认路径）。
   先手工验证一次：
   ```bash
   curl -u '用户名:口令' -X PROPFIND https://pan.example.com/dav/blog -H 'Depth: 0'
   ```
   返回 `207 Multi-Status` 即通；`401` 是账号或路径权限不对。

### 11.2 博客侧

编辑 `deploy/.env`：

```dotenv
BLOG_CLOUDREVE_ENABLED=true
BLOG_CLOUDREVE_WEBDAV_URL=https://pan.example.com/dav
BLOG_CLOUDREVE_USERNAME=blog-archive
BLOG_CLOUDREVE_PASSWORD=your-password
BLOG_CLOUDREVE_BASE_PATH=/blog
```

口令包含 `$` 时必须写成 `$$`（compose 会先做变量插值）。改完：

```bash
cd /srv/blog/deploy
docker compose up -d          # 只改 env，不需要 --build
docker compose logs --tail=50 blog
```

### 11.3 验证归档

1. 媒体库上传一张图，等状态从「处理中」变成可预览。
2. 查看任务：
   ```bash
   docker compose exec blog sh -c 'true'   # 容器内没有 sqlite3，任务状态用 API 或日志看
   docker compose logs --tail=100 blog | grep media_archive
   ```
3. Cloudreve 的 `/blog` 下应出现 `{sha256}.jpg`（或原扩展名）。
4. 本地原图仍在（30 天保留期，`cleanup` 在 D14 接入）：
   ```bash
   ls -l /data/blog/uploads/*/*/original.*
   ```

归档失败时任务会按 1m / 5m / 15m 退避重试 3 次，之后 `jobs.status=failed`；
日志里搜 `任务失败`，常见原因是 WebDAV 地址少了 `/dav`、账号没给 `/blog` 写权限、
或口令里的 `$` 没转义。

### 11.4 不启用会怎样

`BLOG_CLOUDREVE_ENABLED=false` 时上传、四档变体、媒体库全部照常工作，只是不推原图。
之后想补归档：把开关打开并重建容器，然后在媒体库对每张图点「重试」即可重新投递任务
（重试会重新生成变体并投递归档）。

### 11.5 磁盘上的物理文件名不可预测（实测结论，别拿它做直出）

Cloudreve 落盘的文件名由**存储策略的两个模板**拼成（源码 `pkg/filemanager/fs/dbfs/dbfs.go`
的 `generateSavePath`）：

```
物理路径 = path.Join(策略.DirNameRule, 策略.FileNameRule)
```

默认 `FileNameRule = {uid}_{randomkey8}_{originname}`，所以磁盘上是 `1_bTi2XvqV_target.png`
（`1` = uid，`bTi2XvqV` = 8 位随机，`target.png` = 原名）。随机段是**每次上传时服务端现生成**的，
WebDAV 不会把它交出来：

- `PROPFIND` 只返回虚拟路径和 4 字符 `getetag`（形如 `"XqUl"`，是 `pkg/hashid` 对文件 ID 的编码），
  与物理名里的随机段无关；实测用 ETag 拼 `{uid}_{etag}_{name}` 等 10 种组合全部 404；
- 同名、同内容都不去重：两次上传是两份物理文件，配额各自 +size；
- 结论：**WebDAV 无法直接或低成本拿到物理名**，也无法反推。公开图必须走确定性路径，
  即 `/data/blog/uploads/{sha[:2]}/{sha}/{480,960,1600,2400}.webp`，Cloudreve 只当归档。

如果确实要让 Cloudreve 的目录被匿名直出、且名字可预测，只能改策略模板（后台 → 存储策略 → 编辑，
字段就是 `DirNameRule` / `FileNameRule`，admin API 直接收整个 policy 对象）：

| 目标 | 写法 |
| --- | --- |
| 平铺、直接用虚拟文件名 | `DirNameRule = <data>/public`，`FileNameRule = {originname}` |
| 保留虚拟目录结构 | `DirNameRule = <data>/public/{path}`，`FileNameRule = {originname}` |

可用变量（`pkg/util/common.go` 的 `ReplaceMagicVar`）：`{randomkey16}`、`{randomkey8}`、
`{timestamp}`、`{timestamp_nano}`、`{randomnum2|3|4|8}`、`{uid}`、`{datetime}`、`{date}`、`{year}`、
`{month}`、`{day}`、`{hour}`、`{minute}`、`{second}`、`{uuid}`、`{ext}`、`{originname}`、
`{originname_without_ext}`、`{path}`、`{blob_name}`、`{blob_name_without_ext}`。

改之前必须知道四件事：

1. **只影响新上传**：老文件仍是旧名，旧 URL 继续可用，不会自动迁移（实测改规则后
   `/public/1_bTi2XvqV_target.png` 仍是 `200`，而 `/public/target.png` 是 `404`）。
2. **名字里不能含随机段**：`{uuid}`、`{randomkey8}`、`{randomkey16}` 是每次上传现场生成的，
   WebDAV 不回传，客户端**永远算不出 URL**。要可预测只能用常量、`{uid}`、`{path}`、`{originname}`。
   推荐的最终形态是 `FileNameRule = {originname}`：物理名 = 虚拟名，URL 即
   `<公开前缀>/{sha256}.{ext}`，而唯一性由 Cloudreve 的同目录重名约束 + 博客的 sha256 文件名保证。
3. **同名会撞**：模板里不含随机段时，不同虚拟目录的同名文件指向同一物理路径，Cloudreve 在非覆盖
   模式下会直接报「文件已存在」（`pkg/filemanager/driver/local/local.go` 有 `util.Exists` 检查）。
   博客上传用 `{sha256}.{ext}` 命名不会撞，手工上传的图会。
4. **直出即公开**：`location /public/ { alias <策略根>/; }` 公开的是**整个策略根目录**，不是网盘里
   那个名叫 `public` 的虚拟文件夹 —— 该策略下任何目录的文件都平铺在这个物理目录里，只要知道文件名
   就能匿名下载。改成可预测命名后等于可被枚举下载，所以这种策略只应给专用归档账号用。
   另外**绝不能**把 alias 指到 `<data>` 父目录（会暴露 `cloudreve.db` / `conf.ini`）。

**权限：Cloudreve 建的目录是 0744，nginx 必然 403。**
本地驱动建文件、建目录都用 `Perm = 0744`（`pkg/filemanager/driver/local/local.go`），
目录 0744 表示 other 只有 `r` 没有 `x` —— www-data 无法**穿透**该目录，所以它下面
**任何**路径（哪怕是根本不存在的文件）都返回 `403`，而不是 `404`。这也是区分「名字不对」和
「没权限」的判据：`404` = 名字不对，`403` = 目录穿不过去。

给策略根补一次权限，`-d` 让之后新建的 0744 目录自动继承：

```bash
sudo setfacl -R  -m u:www-data:rX <策略根>
sudo setfacl -R -d -m u:www-data:rX <策略根>
# 没有 acl 时退而求其次：sudo chmod -R a+rX <策略根>
curl -sI https://pan.example.com/public/blog/<文件名> | head -3
```

注意博客容器自己写的文件（`/data/blog/uploads`）是 uid 1000、目录 0750/文件 0640，
那是另一处 ACL，见 §10。

