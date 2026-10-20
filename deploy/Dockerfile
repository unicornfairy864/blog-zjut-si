# syntax=docker/dockerfile:1.7
#
# zjut.si 博客镜像：Next + Go 两个进程，由 s6-overlay 托管在同一个容器里（C1）。
#
# 构建上下文必须是仓库根目录：
#   docker compose -f deploy/compose.yaml build

# ---------- 1. Web 依赖 ----------
FROM node:24-bookworm-slim AS web-deps
ENV PNPM_HOME=/pnpm \
    PATH=/pnpm:$PATH
# 固定 pnpm 版本：pnpm 12 不再读取 package.json 里的 pnpm 字段，
# 构建白名单在 pnpm-workspace.yaml（必须一起 COPY）。
RUN npm install --global pnpm@12.4.2
WORKDIR /src/apps/web
COPY apps/web/package.json apps/web/pnpm-lock.yaml apps/web/pnpm-workspace.yaml ./
RUN pnpm install --frozen-lockfile

# ---------- 2. Web 构建 ----------
FROM web-deps AS web-build
COPY apps/web/ ./
# 构建期禁止访问 API（agent.md §8）：所有取数都发生在运行时。
RUN pnpm build

# ---------- 3. Go 构建 ----------
FROM golang:1.26-bookworm AS server-build
WORKDIR /src/apps/server
COPY apps/server/go.mod apps/server/go.sum ./
RUN go mod download
COPY apps/server/ ./
# CGO disabled；迁移脚本已 //go:embed 进二进制。
# -tags nodynamic 让 gen2brain/webp 走内置 wazero（不依赖宿主 libwebp）。
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -tags nodynamic -trimpath -ldflags "-s -w" -o /out/blogd .

# ---------- 4. 运行时 ----------
FROM node:24-bookworm-slim AS runtime

# slim 镜像不带 xz-utils，`tar -J` 会报 "xz: Cannot exec"；解包 s6-overlay 前必须先装。
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates curl tzdata xz-utils \
    && rm -rf /var/lib/apt/lists/*

ARG S6_OVERLAY_VERSION=3.2.1.0
ADD https://github.com/just-containers/s6-overlay/releases/download/v${S6_OVERLAY_VERSION}/s6-overlay-noarch.tar.xz /tmp/
ADD https://github.com/just-containers/s6-overlay/releases/download/v${S6_OVERLAY_VERSION}/s6-overlay-x86_64.tar.xz /tmp/
RUN tar -C / -Jxpf /tmp/s6-overlay-noarch.tar.xz \
    && tar -C / -Jxpf /tmp/s6-overlay-x86_64.tar.xz \
    && rm -f /tmp/s6-overlay-*.tar.xz

COPY --from=server-build /out/blogd /usr/local/bin/blogd
COPY --from=web-build /src/apps/web/.next/standalone /app
COPY --from=web-build /src/apps/web/.next/static /app/.next/static
COPY --from=web-build /src/apps/web/public /app/public

COPY apps/server/config.yaml /etc/blog/config.yaml
COPY deploy/s6/s6-rc.d/ /etc/s6-overlay/s6-rc.d/
# Windows 检出没有可执行位，显式补上，否则 s6-rc 拒绝启动服务。
RUN chmod +x /etc/s6-overlay/s6-rc.d/blogd/run /etc/s6-overlay/s6-rc.d/web/run

ENV NODE_ENV=production \
    NODE_OPTIONS=--max-old-space-size=384 \
    PORT=3000 \
    HOSTNAME=127.0.0.1 \
    BLOG_CONFIG=/etc/blog/config.yaml \
    BLOG_DB_PATH=/data/blog/db/blog.db \
    BLOG_UPLOAD_DIR=/data/blog/uploads \
    BLOG_FILE_DIR=/data/blog/files \
    BLOG_BACKUP_DIR=/data/blog/backup

# 三个数据目录都必须存在且属于 node（uid 1000）：config 加载时会尝试创建它们，
# 落在 root 属主的目录上会直接拒绝启动。compose 的 bind mount 会盖住这些路径，
# 宿主侧同样要 mkdir + chown（docs/runbook.md §11.1）。
RUN mkdir -p /data/blog/db /data/blog/uploads /data/blog/files /data/blog/backup \
    && chown -R node:node /data/blog

EXPOSE 8640 8641
VOLUME ["/data/blog/db", "/data/blog/uploads", "/data/blog/files", "/data/blog/backup"]

# 健康检查只打 8641，8640 不暴露任何状态（agent.md §2 / §14）。
HEALTHCHECK --interval=30s --timeout=5s --start-period=40s --retries=3 \
    CMD curl -fsS http://127.0.0.1:8641/readyz >/dev/null || exit 1

ENTRYPOINT ["/init"]
