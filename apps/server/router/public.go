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
		api.GET("/posts/:slug/related", v1.ApiGroupApp.Public.Post.Related)
		api.GET("/search", v1.ApiGroupApp.Public.Post.Search)

		// 公开评论（D11）：读带 ETag；写单独挂 3 次 / 10 分钟 / IP 的限流（§9 / §16 G5）。
		api.GET("/posts/:slug/comments", v1.ApiGroupApp.Public.Comment.List)
		api.POST("/posts/:slug/comments",
			middleware.RateLimit(middleware.NewCommentLimiter(), nil),
			v1.ApiGroupApp.Public.Comment.Create)

		// 公开只读聚合（D9–D10）：站点信息、标签、归档、系列、友链与静态页。
		api.GET("/site", v1.ApiGroupApp.Public.Site.Detail)
		api.GET("/tags", v1.ApiGroupApp.Public.Tag.List)
		api.GET("/tags/:slug", v1.ApiGroupApp.Public.Tag.Detail)
		api.GET("/archive", v1.ApiGroupApp.Public.Archive.Detail)
		api.GET("/series", v1.ApiGroupApp.Public.Series.List)
		api.GET("/series/:slug", v1.ApiGroupApp.Public.Series.Detail)
		api.GET("/links", v1.ApiGroupApp.Public.Link.List)
		api.GET("/pages/:slug", v1.ApiGroupApp.Public.Page.Detail)
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
