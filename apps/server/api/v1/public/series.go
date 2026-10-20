package public

import (
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/unicornfairy864/blog-zjut-si/apps/server/model/common/response"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/service"
)

// SeriesApi 处理 GET /api/series 与 GET /api/series/{slug}。
type SeriesApi struct{}

// List 处理 GET /api/series：count 为系列下的已发布文章数。
func (SeriesApi) List(c *gin.Context) {
	items, err := service.ServiceGroupApp.Public.Series()
	if err != nil {
		response.FailWithError(c, err)
		return
	}
	response.OkWithETag(c, gin.H{"list": items})
}

// Detail 处理 GET /api/series/{slug}：文章按 series_order 升序；系列不存在 → HTTP 200 + code 1002。
func (SeriesApi) Detail(c *gin.Context) {
	detail, err := service.ServiceGroupApp.Public.SeriesDetail(strings.TrimSpace(c.Param("slug")))
	if err != nil {
		response.FailWithError(c, err)
		return
	}
	response.OkWithETag(c, detail)
}
