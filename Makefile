# zjut.si 博客 —— 常用命令
#
# Windows 上没有 make 时，直接执行下面每条命令 `cd` 后的部分即可。

SHELL := /bin/sh

GO ?= go
PNPM ?= pnpm
SERVER_DIR := apps/server
WEB_DIR := apps/web

.PHONY: help dev dev-server dev-web build build-server build-web check fmt vet lint web-lint web-typecheck migrate

help:
	@echo "make dev-server   启动 Go（:8640 公开 / :8641 管理）"
	@echo "make dev-web      启动 Next（:3000，由 Go 反代）"
	@echo "make build        构建 Go 二进制与 Next 产物"
	@echo "make check        fmt + vet + lint + typecheck"
	@echo "make migrate      只执行数据库迁移后退出"

# 两个进程需要分别起：Go 反代到 Next，顺序不限但两个都要在。
dev: dev-server

dev-server:
	cd $(SERVER_DIR) && $(GO) run .

dev-web:
	cd $(WEB_DIR) && $(PNPM) dev

build: build-server build-web

build-server:
	cd $(SERVER_DIR) && CGO_ENABLED=0 $(GO) build -tags nodynamic -trimpath -ldflags "-s -w" -o blogd .

build-web:
	cd $(WEB_DIR) && $(PNPM) build

check: fmt vet lint web-lint web-typecheck

fmt:
	cd $(SERVER_DIR) && test -z "$$(gofmt -l .)" || (gofmt -l . && exit 1)

vet:
	cd $(SERVER_DIR) && $(GO) vet ./...

lint:
	cd $(SERVER_DIR) && golangci-lint run --timeout=5m

web-lint:
	cd $(WEB_DIR) && $(PNPM) lint

web-typecheck:
	cd $(WEB_DIR) && $(PNPM) typecheck

# 迁移通常由进程启动时自动执行；这个目标用于预发/回滚前的单独推进。
migrate:
	cd $(SERVER_DIR) && BLOG_MIGRATE_ONLY=1 $(GO) run .
