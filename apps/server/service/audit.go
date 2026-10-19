package service

import (
	"encoding/json"
	"fmt"

	"gorm.io/gorm"

	"github.com/unicornfairy864/blog-zjut-si/apps/server/global"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/model"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/model/common/dbtime"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/utils"
)

// 审计动作名；写库后不可随意改名（历史日志要能聚合）。
const (
	AuditActionLogin       = "auth.login"
	AuditActionLogout      = "auth.logout"
	AuditActionLoginFailed = "auth.login_failed"
	AuditActionLoginLocked = "auth.login_locked"
	AuditActionAdminBoot   = "auth.admin_bootstrap"

	AuditActionPostCreate    = "post.create"
	AuditActionPostUpdate    = "post.update"
	AuditActionPostDelete    = "post.delete"
	AuditActionPostPublish   = "post.publish"
	AuditActionPostUnpublish = "post.unpublish"
	AuditActionPostArchive   = "post.archive"
	AuditActionPostSchedule  = "post.schedule"
	AuditActionPostRestore   = "post.restore"

	AuditActionPreviewCreate = "preview.create"
	AuditActionPreviewDelete = "preview.delete"

	AuditActionTagCreate      = "tag.create"
	AuditActionTagUpdate      = "tag.update"
	AuditActionTagDelete      = "tag.delete"
	AuditActionSeriesCreate   = "series.create"
	AuditActionSeriesUpdate   = "series.update"
	AuditActionSeriesDelete   = "series.delete"
	AuditActionLinkCreate     = "link.create"
	AuditActionLinkUpdate     = "link.update"
	AuditActionLinkDelete     = "link.delete"
	AuditActionRedirectCreate = "redirect.create"
	AuditActionRedirectUpdate = "redirect.update"
	AuditActionRedirectDelete = "redirect.delete"

	AuditActionAttachmentCreate = "attachment.create"
	AuditActionAttachmentUpdate = "attachment.update"
	AuditActionAttachmentDelete = "attachment.delete"

	AuditActionSettingsUpdate = "settings.update"
	AuditActionJobEnqueue     = "job.enqueue"
	AuditActionJobRetry       = "job.retry"

	AuditActionMediaUpload  = "media.upload"
	AuditActionMediaDelete  = "media.delete"
	AuditActionMediaRetry   = "media.retry"
	AuditActionMediaArchive = "media.archive"

	AuditActionCommentApprove = "comment.approve"
	AuditActionCommentSpam    = "comment.spam"
	AuditActionCommentDelete  = "comment.delete"
	AuditActionCommentBulk    = "comment.bulk"
)

// AuditService 写 audit_logs。
type AuditService struct{}

// AuditInput 是一条审计记录的业务入参。
type AuditInput struct {
	AdminID    int64
	Action     string
	TargetType string
	TargetID   string
	Meta       map[string]any
	IP         string
}

// Write 尽力写入审计；失败只记日志，不影响主流程返回值。
//
// 需要与业务写入同生共死的场景（如登录成功）请用 WriteTx。
func (AuditService) Write(in AuditInput) {
	if err := global.DB.Transaction(func(tx *gorm.DB) error {
		return AuditService{}.WriteTx(tx, in)
	}); err != nil {
		global.Log.Error("写审计日志失败", "action", in.Action, "err", err)
	}
}

// WriteTx 在调用方事务内写入审计。
func (AuditService) WriteTx(tx *gorm.DB, in AuditInput) error {
	meta := "{}"
	if len(in.Meta) > 0 {
		buf, err := json.Marshal(in.Meta)
		if err != nil {
			return fmt.Errorf("序列化审计元数据失败: %w", err)
		}
		meta = string(buf)
	}
	entry := &model.AuditLog{
		AdminID:    in.AdminID,
		Action:     in.Action,
		TargetType: in.TargetType,
		TargetID:   in.TargetID,
		MetaJSON:   meta,
		IPHash:     utils.HashIP(in.IP),
		CreatedAt:  dbtime.Now(),
	}
	if err := model.CreateAuditLog(tx, entry); err != nil {
		return fmt.Errorf("写入审计日志失败: %w", err)
	}
	return nil
}
