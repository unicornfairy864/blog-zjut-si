package model

import (
	"github.com/unicornfairy864/blog-zjut-si/apps/server/model/common/dbtime"
	"gorm.io/gorm"
)

// login_attempts.kind 的取值。
const (
	// AttemptKindLogin 是登录失败计数。
	AttemptKindLogin = "login"
)

// LoginAttempt 对应 login_attempts 表：按 (key, kind) 唯一，锁定键是用户名（agent.md §9）。
type LoginAttempt struct {
	ID          int64       `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	Key         string      `gorm:"column:key" json:"key"`
	Kind        string      `gorm:"column:kind" json:"kind"`
	Count       int         `gorm:"column:count" json:"count"`
	WindowStart dbtime.Unix `gorm:"column:window_start" json:"windowStart"`
	LockedUntil dbtime.Unix `gorm:"column:locked_until" json:"lockedUntil"`
}

// TableName 固定表名。
func (LoginAttempt) TableName() string { return "login_attempts" }

// FindLoginAttempt 按 (key, kind) 查询计数行。
func FindLoginAttempt(db *gorm.DB, key, kind string) (*LoginAttempt, error) {
	var a LoginAttempt
	if err := db.Where("key = ? AND kind = ?", key, kind).Take(&a).Error; err != nil {
		return nil, wrapErr(err)
	}
	return &a, nil
}

// CreateLoginAttempt 插入计数行。
func CreateLoginAttempt(db *gorm.DB, a *LoginAttempt) error {
	return db.Create(a).Error
}

// UpdateLoginAttempt 覆盖计数行。
func UpdateLoginAttempt(db *gorm.DB, a *LoginAttempt) error {
	return db.Model(&LoginAttempt{}).Where("id = ?", a.ID).
		Updates(map[string]any{
			"count":        a.Count,
			"window_start": a.WindowStart,
			"locked_until": a.LockedUntil,
		}).Error
}

// DeleteLoginAttempt 清除计数行（登录成功或解锁后）。
func DeleteLoginAttempt(db *gorm.DB, key, kind string) error {
	return db.Where("key = ? AND kind = ?", key, kind).Delete(&LoginAttempt{}).Error
}
