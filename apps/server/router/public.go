package router

import (
	"github.com/gin-gonic/gin"

	"github.com/unicornfairy864/blog-zjut-si/apps/server/api/v1"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/core/edge"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/middleware"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/model/common/errs"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/model/common/response"
)

// NewPublicEngine 构建 8640 公开边缘。
//
// 规则（§2 / C4）：/api/* 本地处理；/admin、/api/admin、/api/internal、
// /uploads、健康检查一律 404；其余转 Next。
func NewPublicEngine(proxy *edge.Proxy) *gin.Engine {
	engine := baseChain(gin.New())

	api := engine.Group("/api")
	api.Use(middleware.RateLimit(middleware.NewPublicAPILimiter(), nil))
	{
		api.GET("/system/ping", v1.ApiGroupApp.Public.Ping)

		// 公开文章与搜索（D6）：只读，全部带强 ETag（§5 / §12）。
		api.GET("/posts", v1.ApiGroupApp.Public.Post.List)
		api.GET("/posts/:slug", v1.ApiGroupApp.Public.Post.Detail)
		api.GET("/search", v1.ApiGroupApp.Public.Post.Search)
	}

	engine.NoRoute(func(c *gin.Context) {
		if edge.IsPrivatePath(c.Request.URL.Path) || edge.IsAPIPath(c.Request.URL.Path) {
			response.FailWithDetailed(c, errs.ErrorNotFound, nil, "页面不存在")
			return
		}
		proxy.Serve(c)
	})
	return engine
}
