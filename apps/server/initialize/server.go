package initialize

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"golang.org/x/sync/errgroup"

	"github.com/unicornfairy864/blog-zjut-si/apps/server/global"
)

// serveHTTP 运行一个 listener，并在 ctx 取消时优雅关闭。
//
// 两条 listener 各自一个 http.Server；任一异常退出都会让 errgroup 取消 ctx。
func serveHTTP(ctx context.Context, addr string, handler http.Handler, name string) error {
	server := &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: global.Config.Server.ReadHeaderTimeout(),
		IdleTimeout:       global.Config.Server.IdleTimeout(),
	}

	group, groupCtx := errgroup.WithContext(ctx)

	group.Go(func() error {
		<-groupCtx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), global.Config.Server.ShutdownTimeout())
		defer cancel()
		global.Log.Info("正在关闭 listener", "listener", name)
		return server.Shutdown(shutdownCtx)
	})

	group.Go(func() error {
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("%s listener 启动失败: %w", name, err)
		}
		return nil
	})

	return group.Wait()
}
