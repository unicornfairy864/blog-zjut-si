package admin

import (
	"github.com/gin-gonic/gin"

	"github.com/unicornfairy864/blog-zjut-si/apps/server/model/common/response"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/service"
)

// StorageApi 处理 /api/admin/storage*：磁盘对账与孤儿清理（§5 / §10）。
type StorageApi struct{}

// Overview 处理 GET /api/admin/storage：占用统计 + 孤儿/缺失文件清单。
func (StorageApi) Overview(c *gin.Context) {
	overview, err := service.ServiceGroupApp.Media.StorageOverview()
	if err != nil {
		response.FailWithError(c, err)
		return
	}
	response.OkWithData(c, overview)
}

// Cleanup 处理 POST /api/admin/storage/cleanup：删除没有任何记录引用的孤儿文件。
func (StorageApi) Cleanup(c *gin.Context) {
	result, err := service.ServiceGroupApp.Media.CleanupOrphans(actorFrom(c))
	if err != nil {
		response.FailWithError(c, err)
		return
	}
	response.OkWithDetailed(c, result, "孤儿文件已清理")
}
