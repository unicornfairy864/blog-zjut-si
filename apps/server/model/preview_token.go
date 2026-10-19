package model

import (
	"gorm.io/gorm"

	"github.com/unicornfairy864/blog-zjut-si/apps/server/model/common/dbtime"
)

// PreviewToken 对应 preview_tokens 表：一次性预览链接（agent.md §4 / §5）。
//
// 库里只存 token_hash（明文只在生成时回给管理员一次）；24h 过期，used_at 非空即失效。
type PreviewToken struct {
	TokenHash string       `gorm:"column:token_hash;primaryKey" json:"-"`
	PostID    int64        `gorm:"column:post_id" json:"postId"`
	ExpiresAt dbtime.Unix  `gorm:"column:expires_at" json:"expiresAt"`
	UsedAt    *dbtime.Unix `gorm:"column:used_at" json:"usedAt"`
	CreatedAt dbtime.Unix  `gorm:"column:created_at" json:"createdAt"`
}

// TableName 固定表名。
func (PreviewToken) TableName() string { return "preview_tokens" }

// CreatePreviewToken 写入一条预览令牌。
func CreatePreviewToken(db *gorm.DB, token *PreviewToken) error {
	return db.Create(token).Error
}

// FindPreviewToken 按 token_hash 查询；不存在时返回 ErrNotFound。
func FindPreviewToken(db *gorm.DB, tokenHash string) (*PreviewToken, error) {
	var token PreviewToken
	if err := db.Where("token_hash = ?", tokenHash).Take(&token).Error; err != nil {
		return nil, wrapErr(err)
	}
	return &token, nil
}

// MarkPreviewTokenUsed 记下首次使用时间；之后该链接即失效。
func MarkPreviewTokenUsed(db *gorm.DB, tokenHash string, at dbtime.Unix) error {
	return db.Model(&PreviewToken{}).Where("token_hash = ?", tokenHash).
		Update("used_at", at).Error
}

// DeletePreviewToken 立即作废一条令牌；返回是否真的删掉了。
func DeletePreviewToken(db *gorm.DB, tokenHash string) (bool, error) {
	result := db.Where("token_hash = ?", tokenHash).Delete(&PreviewToken{})
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}

// DeleteExpiredPreviewTokens 删除已过期或已使用的令牌（cleanup 用）。
func DeleteExpiredPreviewTokens(db *gorm.DB, now dbtime.Unix) (int64, error) {
	result := db.Where("expires_at <= ? OR used_at IS NOT NULL", now).Delete(&PreviewToken{})
	return result.RowsAffected, result.Error
}

// DeleteUnusedPreviewTokens 撤掉某文章还没用过的令牌（重新生成链接时调用）。
//
// 一次性链接的价值在于"只有一把钥匙"，留着旧链接等于多开了几扇门。
func DeleteUnusedPreviewTokens(db *gorm.DB, postID int64) (int64, error) {
	result := db.Where("post_id = ? AND used_at IS NULL", postID).Delete(&PreviewToken{})
	return result.RowsAffected, result.Error
}
