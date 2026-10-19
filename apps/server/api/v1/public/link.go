package public

import (
	"github.com/gin-gonic/gin"

	"github.com/unicornfairy864/blog-zjut-si/apps/server/model/common/response"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/service"
)

// LinkApi 处理 GET /api/links。
type LinkApi struct{}

// List 处理 GET /api/links：只出 status='visible' 的友链，按 sort 升序、id 升序。
func (LinkApi) List(c *gin.Context) {
	items, err := service.ServiceGroupApp.Public.Links()
	if err != nil {
		response.FailWithError(c, err)
		return
	}
	response.OkWithETag(c, gin.H{"list": items})
}
