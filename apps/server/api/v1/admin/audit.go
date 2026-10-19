package admin

import (
	"encoding/json"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/unicornfairy864/blog-zjut-si/apps/server/model"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/model/common/dbtime"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/model/common/errs"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/model/common/request"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/model/common/response"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/service"
)

// AuditApi 处理 /api/admin/audit-logs（§5）。
type AuditApi struct{}

// auditLogItem 是审计列表项：meta 从库里的 JSON 字符串还原成对象，ipHash 只在这里出现（§9）。
type auditLogItem struct {
	ID         int64          `json:"id"`
	AdminID    int64          `json:"adminId"`
	Action     string         `json:"action"`
	TargetType string         `json:"targetType"`
	TargetID   string         `json:"targetId"`
	Meta       map[string]any `json:"meta"`
	IPHash     string         `json:"ipHash"`
	CreatedAt  dbtime.Unix    `json:"createdAt"`
}

// List 处理 GET /api/admin/audit-logs?action=&targetType=&targetId=&page=&pageSize=。
func (AuditApi) List(c *gin.Context) {
	var page request.PageInfo
	if err := c.ShouldBindQuery(&page); err != nil {
		response.FailWithDetailed(c, errs.ErrorValidation, request.ValidationDetails(err), "参数校验失败")
		return
	}
	page.Normalize()

	logs, total, err := service.ServiceGroupApp.Ops.AuditLogs(model.AuditLogFilter{
		Action:     strings.TrimSpace(c.Query("action")),
		TargetType: strings.TrimSpace(c.Query("targetType")),
		TargetID:   strings.TrimSpace(c.Query("targetId")),
	}, page.Page, page.PageSize)
	if err != nil {
		response.FailWithError(c, err)
		return
	}

	items := make([]auditLogItem, 0, len(logs))
	for i := range logs {
		items = append(items, toAuditLogItem(&logs[i]))
	}
	response.OkWithPage(c, items, total, page.Page, page.PageSize)
}

// toAuditLogItem 把实体转成出参；meta 解析失败时给空对象，不让一条脏数据毁掉整页。
func toAuditLogItem(log *model.AuditLog) auditLogItem {
	meta := map[string]any{}
	if strings.TrimSpace(log.MetaJSON) != "" {
		_ = json.Unmarshal([]byte(log.MetaJSON), &meta)
	}
	return auditLogItem{
		ID:         log.ID,
		AdminID:    log.AdminID,
		Action:     log.Action,
		TargetType: log.TargetType,
		TargetID:   log.TargetID,
		Meta:       meta,
		IPHash:     log.IPHash,
		CreatedAt:  log.CreatedAt,
	}
}
