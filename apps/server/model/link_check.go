package model

import (
	"gorm.io/gorm"

	"github.com/unicornfairy864/blog-zjut-si/apps/server/model/common/dbtime"
)

// link_checks.kind 的取值（agent.md §4）。
const (
	LinkCheckKindLink  = "link"
	LinkCheckKindImage = "image"
)

// LinkCheck 对应 link_checks 表：外链与正文图片的可用性检查结果（§4 / §13）。
type LinkCheck struct {
	ID         int64       `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	PostID     int64       `gorm:"column:post_id" json:"postId"`
	URL        string      `gorm:"column:url" json:"url"`
	Kind       string      `gorm:"column:kind" json:"kind"`
	StatusCode int         `gorm:"column:status_code" json:"statusCode"`
	OK         bool        `gorm:"column:ok" json:"ok"`
	Error      string      `gorm:"column:error" json:"error"`
	CheckedAt  dbtime.Unix `gorm:"column:checked_at" json:"checkedAt"`
}

// TableName 固定表名。
func (LinkCheck) TableName() string { return "link_checks" }

// ReplaceLinkChecks 用本次结果替换某文章的历史检查记录（先删后插，表不会无限增长）。
func ReplaceLinkChecks(db *gorm.DB, postID int64, items []LinkCheck) error {
	return db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("post_id = ?", postID).Delete(&LinkCheck{}).Error; err != nil {
			return err
		}
		if len(items) == 0 {
			return nil
		}
		return tx.Create(&items).Error
	})
}

// ListPublishedForLinkCheck 返回已发布文章的正文（按最近更新排序），供外链巡检使用。
func ListPublishedForLinkCheck(db *gorm.DB) ([]Post, error) {
	posts := make([]Post, 0)
	err := db.Model(&Post{}).
		Select("id", "slug", "content_html").
		Where("status = ? AND deleted_at IS NULL", PostStatusPublished).
		Order("updated_at DESC, id DESC").
		Find(&posts).Error
	if err != nil {
		return nil, err
	}
	return posts, nil
}
