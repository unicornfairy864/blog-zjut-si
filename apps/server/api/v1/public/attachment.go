package public

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/unicornfairy864/blog-zjut-si/apps/server/model/common/errs"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/model/common/request"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/model/common/response"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/service"
)

// AttachmentApi 处理公开端的附件下载跳转（§5）。
type AttachmentApi struct{}

// Go 处理 GET /api/attachments/{id}/go：查附件 → clicks+1 → 302 到 share_url。
//
// 响应必须 no-store：同一路径每次都会改变计数，且内容只是一次跳转（§5 / §12）。
// 计数失败不影响跳转本身：访客要的是文件，统计丢了只记日志。
func (AttachmentApi) Go(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.FailWithDetailed(c, errs.ErrorValidation,
			[]request.FieldError{{Field: "id", Message: "必须是正整数"}}, "参数校验失败")
		return
	}

	item, err := service.ServiceGroupApp.Content.AttachmentByID(id)
	if err != nil {
		response.FailWithError(c, err)
		return
	}

	service.ServiceGroupApp.Content.TrackAttachmentClick(item.ID)

	c.Header("Cache-Control", "no-store")
	c.Redirect(http.StatusFound, item.ShareURL)
}
