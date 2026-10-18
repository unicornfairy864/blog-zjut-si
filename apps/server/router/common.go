// Package router 只注册路由与中间件链；三个 engine（public / admin / internal）各自成文件。
package router

import (
	"github.com/gin-gonic/gin"

	"github.com/unicornfairy864/blog-zjut-si/apps/server/global"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/middleware"
)

// baseChain 装配三条边缘共用的中间件链：request_id → 恢复 → 访问日志 → 安全头。
func baseChain(engine *gin.Engine) *gin.Engine {
	engine.Use(
		middleware.RequestID(),
		middleware.Recovery(),
		middleware.AccessLog(),
		middleware.SecurityHeaders(),
	)
	if err := engine.SetTrustedProxies(global.Config.Server.TrustedProxies); err != nil {
		global.Log.Warn("设置可信代理失败", "err", err)
	}
	return engine
}
