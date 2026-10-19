package public

import (
	"github.com/gin-gonic/gin"

	"github.com/unicornfairy864/blog-zjut-si/apps/server/model/common/response"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/service"
)

// SiteApi 处理 GET /api/site。
type SiteApi struct{}

// Detail 处理 GET /api/site：站点时区、图片前缀、内容版本与四项计数。
func (SiteApi) Detail(c *gin.Context) {
	info, err := service.ServiceGroupApp.Public.Site()
	if err != nil {
		response.FailWithError(c, err)
		return
	}
	response.OkWithETag(c, info)
}
