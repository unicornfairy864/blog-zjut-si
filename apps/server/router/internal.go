package router

import (
	"github.com/gin-gonic/gin"

	"github.com/unicornfairy864/blog-zjut-si/apps/server/api/v1"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/model/common/errs"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/model/common/response"
)

// NewInternalEngine 构建内部边缘：/api/internal/*、/healthz、/readyz。
//
// 只挂在 8641（由 NewAdminGateway 分发），8640 上这些路径直接 404。
func NewInternalEngine() *gin.Engine {
	engine := baseChain(gin.New())

	engine.GET("/healthz", v1.ApiGroupApp.Internal.Healthz)
	engine.GET("/readyz", v1.ApiGroupApp.Internal.Ready)

	api := engine.Group("/api/internal")
	{
		api.GET("/health", v1.ApiGroupApp.Internal.InternalHealth)
	}

	engine.NoRoute(func(c *gin.Context) {
		// 路由不存在也回 200 + code 1002：连接已建立，状态只存在信封里（§5）。
		response.FailWithDetailed(c, errs.ErrorNotFound, nil, "接口不存在")
	})
	return engine
}
