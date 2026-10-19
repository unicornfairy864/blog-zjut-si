package router

import (
	"github.com/gin-gonic/gin"

	"github.com/unicornfairy864/blog-zjut-si/apps/server/api/v1"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/core/edge"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/global"
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

			// 文章（D4）：草稿 CRUD + 状态机 + 版本快照。
			authorized.GET("/posts", v1.ApiGroupApp.Admin.Post.List)
			authorized.POST("/posts", v1.ApiGroupApp.Admin.Post.Create)
			// 预览是不落库的渲染，放在 :id 之前注册以保持静态路由优先。
			authorized.POST("/posts/preview", v1.ApiGroupApp.Admin.Post.Preview)
			authorized.GET("/posts/:id", v1.ApiGroupApp.Admin.Post.Detail)
			authorized.PUT("/posts/:id", v1.ApiGroupApp.Admin.Post.Replace)
			authorized.PATCH("/posts/:id", v1.ApiGroupApp.Admin.Post.Update)
			authorized.DELETE("/posts/:id", v1.ApiGroupApp.Admin.Post.Delete)
			authorized.POST("/posts/:id/publish", v1.ApiGroupApp.Admin.Post.Publish)
			authorized.POST("/posts/:id/unpublish", v1.ApiGroupApp.Admin.Post.Unpublish)
			authorized.GET("/posts/:id/revisions", v1.ApiGroupApp.Admin.Post.Revisions)

			// 媒体（D7）：上传、媒体库、删除与重试。
			authorized.GET("/media", v1.ApiGroupApp.Admin.Media.List)
			authorized.POST("/media", v1.ApiGroupApp.Admin.Media.Upload)
			authorized.DELETE("/media/:id", v1.ApiGroupApp.Admin.Media.Delete)
			authorized.POST("/media/:id/retry", v1.ApiGroupApp.Admin.Media.Retry)

			// 评论审核（D11）：列表、单条与批量；写操作在 authorized 组内自带 CSRF（§5）。
			authorized.GET("/comments", v1.ApiGroupApp.Admin.Comment.List)
			authorized.PATCH("/comments", v1.ApiGroupApp.Admin.Comment.Patch)
			authorized.POST("/comments/bulk", v1.ApiGroupApp.Admin.Comment.Bulk)
		}
	}

	// 管理端直出本地上传的图片：8641 只经隧道可达，8640 仍由 nginx 直出（§2）。
	engine.GET("/uploads/*filepath", edge.ServeUploads(global.Config.Upload.Dir))

	engine.NoRoute(func(c *gin.Context) {
		if edge.IsAdminPagePath(c.Request.URL.Path) {
			proxy.Serve(c)
			return
		}
		response.FailWithDetailed(c, errs.ErrorNotFound, nil, "接口不存在")
	})
	return engine
}
