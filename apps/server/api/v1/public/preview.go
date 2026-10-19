package public

import (
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/unicornfairy864/blog-zjut-si/apps/server/model/common/response"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/service"
)

// PreviewApi 处理公开端的一次性预览链接（§5）。
type PreviewApi struct{}

// Detail 处理 GET /api/preview/{token}：首次访问消费令牌，之后或过期 = 410/2006。
//
// 响应必须 no-store：内容是草稿，且每次请求都会改变令牌状态。
func (PreviewApi) Detail(c *gin.Context) {
	post, err := service.ServiceGroupApp.Post.PreviewByToken(strings.TrimSpace(c.Param("token")))
	if err != nil {
		response.FailWithError(c, err)
		return
	}
	response.OkWithNoStore(c, post)
}
