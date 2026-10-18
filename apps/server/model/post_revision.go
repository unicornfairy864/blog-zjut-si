package model

import (
	"gorm.io/gorm"

	"github.com/unicornfairy864/blog-zjut-si/apps/server/model/common/dbtime"
)

// PostRevision 对应 post_revisions 表；每次发布写一条，最多保留最近 50 条（§6 / §13）。
type PostRevision struct {
	ID          int64       `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	PostID      int64       `gorm:"column:post_id" json:"postId"`
	RevisionNo  int         `gorm:"column:revision_no" json:"revisionNo"`
	Title       string      `gorm:"column:title" json:"title"`
	ContentMD   string      `gorm:"column:content_md" json:"-"`
	ContentHTML string      `gorm:"column:content_html" json:"-"`
	Note        string      `gorm:"column:note" json:"note"`
	CreatedAt   dbtime.Unix `gorm:"column:created_at" json:"createdAt"`
}

// TableName 固定表名。
func (PostRevision) TableName() string { return "post_revisions" }

// CreatePostRevision 写入一条版本快照。
func CreatePostRevision(db *gorm.DB, revision *PostRevision) error {
	return db.Create(revision).Error
}

// ListPostRevisions 按版本号倒序返回文章的版本列表。
func ListPostRevisions(db *gorm.DB, postID int64, limit int) ([]PostRevision, error) {
	query := db.Where("post_id = ?", postID).Order("revision_no DESC")
	if limit > 0 {
		query = query.Limit(limit)
	}
	revisions := make([]PostRevision, 0)
	if err := query.Find(&revisions).Error; err != nil {
		return nil, err
	}
	return revisions, nil
}

// CountPostRevisions 统计文章的版本条数。
func CountPostRevisions(db *gorm.DB, postID int64) (int64, error) {
	var n int64
	err := db.Model(&PostRevision{}).Where("post_id = ?", postID).Count(&n).Error
	return n, err
}

// PrunePostRevisions 只保留最近 keep 条版本，其余物理删除。
func PrunePostRevisions(db *gorm.DB, postID int64, keep int) error {
	if keep < 1 {
		return nil
	}
	sub := db.Model(&PostRevision{}).
		Select("id").
		Where("post_id = ?", postID).
		Order("revision_no DESC").
		Limit(keep)
	return db.Where("post_id = ? AND id NOT IN (?)", postID, sub).
		Delete(&PostRevision{}).Error
}
