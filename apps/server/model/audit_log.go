package model

import (
	"github.com/unicornfairy864/blog-zjut-si/apps/server/model/common/dbtime"
	"gorm.io/gorm"
)

// AuditLog 对应 audit_logs 表：所有管理端写操作都写一条（agent.md §9）。
type AuditLog struct {
	ID         int64       `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	AdminID    int64       `gorm:"column:admin_id" json:"adminId"`
	Action     string      `gorm:"column:action" json:"action"`
	TargetType string      `gorm:"column:target_type" json:"targetType"`
	TargetID   string      `gorm:"column:target_id" json:"targetId"`
	MetaJSON   string      `gorm:"column:meta_json" json:"meta"`
	IPHash     string      `gorm:"column:ip_hash" json:"-"`
	CreatedAt  dbtime.Unix `gorm:"column:created_at" json:"createdAt"`
}

// TableName 固定表名。
func (AuditLog) TableName() string { return "audit_logs" }

// CreateAuditLog 写入一条审计记录。
func CreateAuditLog(db *gorm.DB, log *AuditLog) error {
	return db.Create(log).Error
}
