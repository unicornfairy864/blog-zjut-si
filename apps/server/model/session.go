package model

import (
	"github.com/unicornfairy864/blog-zjut-si/apps/server/model/common/dbtime"
	"gorm.io/gorm"
)

// Session 对应 sessions 表；主键是会话令牌的哈希，明文只存在于浏览器 cookie。
type Session struct {
	TokenHash  string      `gorm:"column:token_hash;primaryKey" json:"-"`
	AdminID    int64       `gorm:"column:admin_id" json:"adminId"`
	CSRFHash   string      `gorm:"column:csrf_hash" json:"-"`
	IPHash     string      `gorm:"column:ip_hash" json:"-"`
	UA         string      `gorm:"column:ua" json:"-"`
	CreatedAt  dbtime.Unix `gorm:"column:created_at" json:"createdAt"`
	LastSeenAt dbtime.Unix `gorm:"column:last_seen_at" json:"lastSeenAt"`
	ExpiresAt  dbtime.Unix `gorm:"column:expires_at" json:"expiresAt"`
}

// TableName 固定表名。
func (Session) TableName() string { return "sessions" }

// CreateSession 插入会话。
func CreateSession(db *gorm.DB, s *Session) error {
	return db.Create(s).Error
}

// FindSession 按令牌哈希查询会话。
func FindSession(db *gorm.DB, tokenHash string) (*Session, error) {
	var s Session
	if err := db.Where("token_hash = ?", tokenHash).Take(&s).Error; err != nil {
		return nil, wrapErr(err)
	}
	return &s, nil
}

// DeleteSession 删除单个会话（登出）。
func DeleteSession(db *gorm.DB, tokenHash string) error {
	return db.Where("token_hash = ?", tokenHash).Delete(&Session{}).Error
}

// DeleteSessionsByAdmin 删除某管理员的全部会话（改密即全端失效）。
func DeleteSessionsByAdmin(db *gorm.DB, adminID int64) (int64, error) {
	res := db.Where("admin_id = ?", adminID).Delete(&Session{})
	return res.RowsAffected, res.Error
}

// TouchSession 刷新滑动过期时间。
func TouchSession(db *gorm.DB, tokenHash string, lastSeen, expires dbtime.Unix) error {
	return db.Model(&Session{}).Where("token_hash = ?", tokenHash).
		Updates(map[string]any{"last_seen_at": lastSeen, "expires_at": expires}).Error
}

// UpdateSessionCSRF 更新会话的 CSRF 哈希。
func UpdateSessionCSRF(db *gorm.DB, tokenHash, csrfHash string) error {
	return db.Model(&Session{}).Where("token_hash = ?", tokenHash).
		Update("csrf_hash", csrfHash).Error
}

// DeleteExpiredSessions 清理过期会话（cleanup 任务用）。
func DeleteExpiredSessions(db *gorm.DB, now dbtime.Unix) (int64, error) {
	res := db.Where("expires_at <= ?", now).Delete(&Session{})
	return res.RowsAffected, res.Error
}
