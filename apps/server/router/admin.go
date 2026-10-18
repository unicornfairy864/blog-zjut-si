package router

import (
	"github.com/gin-gonic/gin"

	"github.com/unicornfairy864/blog-zjut-si/apps/server/api/v1"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/core/edge"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/middleware"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/model/common/errs"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/model/common/response"
)

// NewAdminEngine 构建 8641 管理边缘。
//
// 规则（§2 / C5）：/api/admin/* 本地处理；/admin*（含 /admin/login）与 /_next/* 转 Next；其余 404。
func NewAdminEngine(proxy *edge.Proxy) *gin.Engine {
	engine := baseChain(gin.New())

	admin := engine.Group("/api/admin")
	{
		// 登录不能要求已登录，也不能要求 CSRF（此时还没有会话）。
		admin.POST("/auth/login", v1.ApiGroupApp.Admin.Login)

		authorized := admin.Group("")
		authorized.Use(middleware.Auth(), middleware.CSRF())
		{
			authorized.GET("/auth/me", v1.ApiGroupApp.Admin.Me)
			authorized.POST("/auth/logout", v1.ApiGroupApp.Admin.Logout)
		}
	}

	engine.NoRoute(func(c *gin.Context) {
		if edge.IsAdminPagePath(c.Request.URL.Path) {
			proxy.Serve(c)
			return
		}
		response.FailWithDetailed(c, errs.ErrorNotFound, nil, "接口不存在")
	})
	return engine
}
