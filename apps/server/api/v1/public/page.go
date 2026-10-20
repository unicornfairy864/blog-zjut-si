package public

import (
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/unicornfairy864/blog-zjut-si/apps/server/model/common/response"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/service"
)

// PageApi 处理 GET /api/pages/{slug}。
type PageApi struct{}

// Detail 处理 GET /api/pages/{slug}：不存在 → code 1002，软删 → code 1007（都是 HTTP 200）。
func (PageApi) Detail(c *gin.Context) {
	page, err := service.ServiceGroupApp.Public.Page(strings.TrimSpace(c.Param("slug")))
	if err != nil {
		response.FailWithError(c, err)
		return
	}
	response.OkWithETag(c, page)
}
