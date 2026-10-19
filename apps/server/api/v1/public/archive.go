package public

import (
	"github.com/gin-gonic/gin"

	"github.com/unicornfairy864/blog-zjut-si/apps/server/model/common/response"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/service"
)

// ArchiveApi 处理 GET /api/archive。
type ArchiveApi struct{}

// Detail 处理 GET /api/archive：仅已发布，按站点时区按年倒序、月倒序。
func (ArchiveApi) Detail(c *gin.Context) {
	archive, err := service.ServiceGroupApp.Public.Archive()
	if err != nil {
		response.FailWithError(c, err)
		return
	}
	response.OkWithETag(c, archive)
}
