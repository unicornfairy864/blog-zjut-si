package public

import (
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/unicornfairy864/blog-zjut-si/apps/server/model/common/errs"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/model/common/request"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/model/common/response"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/service"
)

// TagApi 处理 GET /api/tags 与 GET /api/tags/{slug}。
type TagApi struct{}

// List 处理 GET /api/tags：只出至少一篇已发布文章的标签，count 为文章数。
func (TagApi) List(c *gin.Context) {
	tags, err := service.ServiceGroupApp.Public.Tags()
	if err != nil {
		response.FailWithError(c, err)
		return
	}
	response.OkWithETag(c, gin.H{"list": tags})
}

// Detail 处理 GET /api/tags/{slug}：标签不存在 → 404 / 1002。
func (TagApi) Detail(c *gin.Context) {
	var page request.PageInfo
	if err := c.ShouldBindQuery(&page); err != nil {
		response.FailWithDetailed(c, errs.ErrorValidation, request.ValidationDetails(err), "参数校验失败")
		return
	}
	page.Normalize()

	detail, err := service.ServiceGroupApp.Public.TagPosts(strings.TrimSpace(c.Param("slug")), page.Page, page.PageSize)
	if err != nil {
		response.FailWithError(c, err)
		return
	}
	response.OkWithETag(c, detail)
}
