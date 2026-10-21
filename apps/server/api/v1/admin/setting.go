package admin

import (
	"github.com/gin-gonic/gin"

	"github.com/unicornfairy864/blog-zjut-si/apps/server/model/common/errs"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/model/common/request"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/model/common/response"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/service"
)

// SettingApi 处理 /api/admin/settings：可写 imgBaseUrl / siteTimezone / notify（§5）。
type SettingApi struct{}

// settingUpdateRequest 是设置更新入参；nil 字段表示不修改。
type settingUpdateRequest struct {
	ImgBaseURL   *string               `json:"imgBaseUrl"`
	SiteTimezone *string               `json:"siteTimezone"`
	Notify       *service.NotifyUpdate `json:"notify"`
}

// Detail 处理 GET /api/admin/settings：可写项 + 只读回显（不含任何口令）。
func (SettingApi) Detail(c *gin.Context) {
	view, err := service.ServiceGroupApp.Content.Settings()
	if err != nil {
		response.FailWithError(c, err)
		return
	}
	response.OkWithData(c, view)
}

// Update 处理 PUT /api/admin/settings：写成功会 content_version+1，返回更新后的视图。
func (SettingApi) Update(c *gin.Context) {
	var req settingUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.FailWithDetailed(c, errs.ErrorValidation, request.ValidationDetails(err), "参数校验失败")
		return
	}

	view, err := service.ServiceGroupApp.Content.UpdateSettings(
		service.SettingsUpdate{
			ImgBaseURL:   req.ImgBaseURL,
			SiteTimezone: req.SiteTimezone,
			Notify:       req.Notify,
		},
		actorFrom(c),
	)
	if err != nil {
		response.FailWithError(c, err)
		return
	}
	response.OkWithData(c, view)
}
