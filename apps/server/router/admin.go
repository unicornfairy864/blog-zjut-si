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
			authorized.POST("/posts/:id/schedule", v1.ApiGroupApp.Admin.Post.Schedule)
			authorized.POST("/posts/:id/unpublish", v1.ApiGroupApp.Admin.Post.Unpublish)
			authorized.GET("/posts/:id/revisions", v1.ApiGroupApp.Admin.Post.Revisions)
			authorized.POST("/posts/:id/revisions/:rid/restore", v1.ApiGroupApp.Admin.Post.RestoreRevision)

			// 预览链接（D13）：生成一次性链接与立即作废。
			authorized.POST("/posts/:id/preview-link", v1.ApiGroupApp.Admin.Post.PreviewLink)
			authorized.DELETE("/preview-links/:token", v1.ApiGroupApp.Admin.Post.DeletePreviewLink)

			// 媒体（D7）：上传、媒体库、删除、重试与"删原图保变体"（§10）。
			authorized.GET("/media", v1.ApiGroupApp.Admin.Media.List)
			authorized.POST("/media", v1.ApiGroupApp.Admin.Media.Upload)
			authorized.DELETE("/media/:id", v1.ApiGroupApp.Admin.Media.Delete)
			authorized.POST("/media/:id/retry", v1.ApiGroupApp.Admin.Media.Retry)
			authorized.DELETE("/media/:id/original", v1.ApiGroupApp.Admin.Media.RemoveOriginal)

			// 评论审核（D11）：列表、单条与批量；写操作在 authorized 组内自带 CSRF（§5）。
			authorized.GET("/comments", v1.ApiGroupApp.Admin.Comment.List)
			authorized.PATCH("/comments", v1.ApiGroupApp.Admin.Comment.Patch)
			authorized.POST("/comments/bulk", v1.ApiGroupApp.Admin.Comment.Bulk)

			// 统计看板（D12）：overview 与全站日粒度（§5）。
			authorized.GET("/stats/overview", v1.ApiGroupApp.Admin.Stats.Overview)
			authorized.GET("/stats/daily", v1.ApiGroupApp.Admin.Stats.Daily)

			// 内容管理（D14）：标签 / 系列 / 友链 / 跳转的 CRUD（§5「内容管理口径」）。
			authorized.GET("/tags", v1.ApiGroupApp.Admin.Content.Tags)
			authorized.POST("/tags", v1.ApiGroupApp.Admin.Content.CreateTag)
			authorized.PUT("/tags/:id", v1.ApiGroupApp.Admin.Content.UpdateTag)
			authorized.DELETE("/tags/:id", v1.ApiGroupApp.Admin.Content.DeleteTag)
			authorized.GET("/series", v1.ApiGroupApp.Admin.Content.Series)
			authorized.POST("/series", v1.ApiGroupApp.Admin.Content.CreateSeries)
			authorized.PUT("/series/:id", v1.ApiGroupApp.Admin.Content.UpdateSeries)
			authorized.DELETE("/series/:id", v1.ApiGroupApp.Admin.Content.DeleteSeries)
			authorized.GET("/links", v1.ApiGroupApp.Admin.Content.Links)
			authorized.POST("/links", v1.ApiGroupApp.Admin.Content.CreateLink)
			authorized.PUT("/links/:id", v1.ApiGroupApp.Admin.Content.UpdateLink)
			authorized.DELETE("/links/:id", v1.ApiGroupApp.Admin.Content.DeleteLink)
			authorized.GET("/redirects", v1.ApiGroupApp.Admin.Content.Redirects)
			authorized.POST("/redirects", v1.ApiGroupApp.Admin.Content.CreateRedirect)
			authorized.PUT("/redirects/:id", v1.ApiGroupApp.Admin.Content.UpdateRedirect)
			authorized.DELETE("/redirects/:id", v1.ApiGroupApp.Admin.Content.DeleteRedirect)

			// 附件与设置（D14 / D15 本地附件）：外链登记、本地上传、设置回显与更新。
			authorized.GET("/attachments", v1.ApiGroupApp.Admin.Attachment.List)
			authorized.POST("/attachments", v1.ApiGroupApp.Admin.Attachment.Create)
			authorized.POST("/attachments/upload", v1.ApiGroupApp.Admin.Attachment.Upload)
			authorized.PUT("/attachments/:id", v1.ApiGroupApp.Admin.Attachment.Update)
			authorized.DELETE("/attachments/:id", v1.ApiGroupApp.Admin.Attachment.Delete)
			authorized.GET("/settings", v1.ApiGroupApp.Admin.Setting.Detail)
			authorized.PUT("/settings", v1.ApiGroupApp.Admin.Setting.Update)

			// 存储管理（§10）：磁盘占用对账、孤儿文件清理。
			authorized.GET("/storage", v1.ApiGroupApp.Admin.Storage.Overview)
			authorized.POST("/storage/cleanup", v1.ApiGroupApp.Admin.Storage.Cleanup)

			// 任务与审计（D13/D14）：队列查看、手动投递、重试与审计查询。
			authorized.GET("/jobs", v1.ApiGroupApp.Admin.Job.List)
			authorized.POST("/jobs", v1.ApiGroupApp.Admin.Job.Enqueue)
			authorized.POST("/jobs/:id/retry", v1.ApiGroupApp.Admin.Job.Retry)
			authorized.GET("/audit-logs", v1.ApiGroupApp.Admin.Audit.List)
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
