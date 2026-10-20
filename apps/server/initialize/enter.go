// Package initialize 负责所有初始化与进程生命周期；main.go 只做 Init → Run。
package initialize

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/gin-gonic/gin"
	"golang.org/x/sync/errgroup"

	"github.com/unicornfairy864/blog-zjut-si/apps/server/config"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/core/edge"
	corelog "github.com/unicornfairy864/blog-zjut-si/apps/server/core/log"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/global"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/model"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/router"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/service"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/task"
)

// migrateOnly 表示本次启动只推进迁移后退出（运维用，见 docs/runbook.md）。
var migrateOnly bool

// Init 按固定顺序完成初始化：配置 → 日志 → 数据库 → 站点设置 → 初始管理员。
func Init() {
	gin.SetMode(gin.ReleaseMode)

	cfg, err := config.Load(os.Getenv("BLOG_CONFIG"))
	if err != nil {
		fallback := slog.New(slog.NewJSONHandler(os.Stdout, nil))
		fallback.Error("加载配置失败", "err", err)
		os.Exit(1)
	}
	global.Config = cfg
	global.Log = corelog.New(cfg.Log.Level, os.Stdout)
	slog.SetDefault(global.Log)

	initDB()
	if os.Getenv("BLOG_MIGRATE_ONLY") == "1" {
		migrateOnly = true
		return
	}

	initSettings()
	bootstrapAdmin()
}

// Run 启动调度器与两条 listener，阻塞到收到 SIGINT/SIGTERM。
func Run() {
	if migrateOnly {
		global.Log.Info("仅执行迁移，进程退出")
		return
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	proxy, err := edge.NewProxy(global.Config.Web.Upstream, global.Log)
	if err != nil {
		fatal("初始化反向代理失败", err)
	}

	group, groupCtx := errgroup.WithContext(ctx)

	group.Go(func() error {
		handlers := map[string]task.Handler{
			model.JobTypePublishScheduled: service.ServiceGroupApp.Task.HandlePublishScheduled,
			model.JobTypeMediaProcess:     service.ServiceGroupApp.Media.HandleProcess,
			model.JobTypeLinkCheck:        service.ServiceGroupApp.Task.HandleLinkCheck,
			model.JobTypeBackup:           service.ServiceGroupApp.Task.HandleBackup,
			model.JobTypeCleanup:          service.ServiceGroupApp.Task.HandleCleanup,
		}
		return task.NewScheduler(global.Config.Task, handlers).Start(groupCtx)
	})

	group.Go(func() error {
		global.Log.Info("公开边缘监听", "addr", global.Config.Server.Public)
		return serveHTTP(groupCtx, global.Config.Server.Public, router.NewPublicEngine(proxy), "public")
	})

	group.Go(func() error {
		gateway := router.NewAdminGateway(router.NewAdminEngine(proxy), router.NewInternalEngine())
		global.Log.Info("管理边缘监听", "addr", global.Config.Server.Admin)
		return serveHTTP(groupCtx, global.Config.Server.Admin, gateway, "admin")
	})

	if err := group.Wait(); err != nil && !errors.Is(err, context.Canceled) {
		global.Log.Error("服务异常退出", "err", err)
		os.Exit(1)
	}
	global.Log.Info("服务已停止")
}

// fatal 记录并终止进程；只用于初始化阶段。
func fatal(msg string, err error) {
	global.Log.Error(msg, "err", err)
	os.Exit(1)
}
