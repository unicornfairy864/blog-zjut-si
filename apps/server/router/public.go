package router

import (
	"github.com/gin-gonic/gin"

	"github.com/unicornfairy864/blog-zjut-si/apps/server/api/v1"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/core/edge"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/global"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/middleware"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/model/common/errs"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/model/common/response"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/service"
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

		// 互动与统计（D12）：点赞两个方向都幂等，**合计** 10 次 / 分钟 / visitor
		// （两个路由共用同一个桶；各建一个会变成 20 次，与 §5 不符）。
		// stats 是访客态读（no-store），打点写 PV 明细（§5 / §16 G5）。
		likeLimiter := middleware.NewLikeLimiter()
		api.POST("/posts/:slug/like",
			middleware.RateLimit(likeLimiter, middleware.VisitorKey),
			v1.ApiGroupApp.Public.Interaction.Like)
		api.DELETE("/posts/:slug/like",
			middleware.RateLimit(likeLimiter, middleware.VisitorKey),
			v1.ApiGroupApp.Public.Interaction.Unlike)
		api.GET("/posts/:slug/stats", v1.ApiGroupApp.Public.Interaction.Stats)
		api.POST("/events/view", v1.ApiGroupApp.Public.Interaction.View)

		// 一次性预览链接（D13）与附件跳转（D14）：都带 no-store。
		api.GET("/preview/:token", v1.ApiGroupApp.Public.Preview.Detail)
		api.GET("/attachments/:id/go", v1.ApiGroupApp.Public.Attachment.Go)

		// 公开只读聚合（D9–D10）：站点信息、标签、归档、系列、友链与静态页。
		api.GET("/site", v1.ApiGroupApp.Public.Site.Detail)
		api.GET("/tags", v1.ApiGroupApp.Public.Tag.List)
		api.GET("/tags/:slug", v1.ApiGroupApp.Public.Tag.Detail)
		api.GET("/archive", v1.ApiGroupApp.Public.Archive.Detail)
		api.GET("/series", v1.ApiGroupApp.Public.Series.List)
		api.GET("/series/:slug", v1.ApiGroupApp.Public.Series.Detail)
		api.GET("/links", v1.ApiGroupApp.Public.Link.List)
		// 静态页清单要注册在 :slug 之前，否则会被当成 slug 吃掉。
		api.GET("/pages", v1.ApiGroupApp.Public.Page.List)
		api.GET("/pages/:slug", v1.ApiGroupApp.Public.Page.Detail)
	}

	engine.NoRoute(func(c *gin.Context) {
		path := c.Request.URL.Path
		if edge.IsPrivatePath(path) || edge.IsAPIPath(path) {
			// 路由不存在也回 200 + code 1002：连接已建立，状态只存在信封里（§5）。
			response.FailWithDetailed(c, errs.ErrorNotFound, nil, "页面不存在")
			return
		}
		// 改过 slug 的老链接：命中 redirects 就在边缘跳走，不落到 Next 的 404（§5）。
		if redirect, err := service.ServiceGroupApp.Content.RedirectFor(path); err != nil {
			global.Log.Warn("查询跳转失败", "path", path, "err", err)
		} else if redirect != nil {
			c.Header("Cache-Control", "no-cache")
			c.Redirect(redirect.Status, redirect.ToPath)
			return
		}
		proxy.Serve(c)
	})
	return engine
}
