package model

import (
	"gorm.io/gorm"

	"github.com/unicornfairy864/blog-zjut-si/apps/server/model/common/dbtime"
)

// PostAttachment 对应 post_attachments 表：文章附带的可下载资源（§4 / §5）。
//
// 两种来源（§10）：本地上传的附件存 storage_key（物理文件 {file.dir}/{storage_key}{ext}），
// 公开端经 /go 直接下发；历史的外链附件 storage_key 为空，公开端按 share_url 302 跳转。
type PostAttachment struct {
	ID         int64       `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	PostID     int64       `gorm:"column:post_id" json:"postId"`
	Title      string      `gorm:"column:title" json:"title"`
	ShareURL   string      `gorm:"column:share_url" json:"shareUrl"`
	FileName   string      `gorm:"column:file_name" json:"fileName"`
	Bytes      int64       `gorm:"column:bytes" json:"bytes"`
	SHA256     string      `gorm:"column:sha256" json:"sha256"`
	Version    string      `gorm:"column:version" json:"version"`
	Sort       int         `gorm:"column:sort" json:"sort"`
	Clicks     int64       `gorm:"column:clicks" json:"clicks"`
	StorageKey string      `gorm:"column:storage_key" json:"storageKey"`
	Mime       string      `gorm:"column:mime" json:"mime"`
	CreatedAt  dbtime.Unix `gorm:"column:created_at" json:"createdAt"`
}

// TableName 固定表名。
func (PostAttachment) TableName() string { return "post_attachments" }

// ListAttachments 返回附件分页；postID 为 0 表示不过滤。
func ListAttachments(db *gorm.DB, postID int64, limit, offset int) ([]PostAttachment, int64, error) {
	query := func() *gorm.DB {
		q := db.Model(&PostAttachment{})
		if postID > 0 {
			q = q.Where("post_id = ?", postID)
		}
		return q
	}

	var total int64
	if err := query().Count(&total).Error; err != nil {
		return nil, 0, err
	}

	items := make([]PostAttachment, 0)
	q := query().Order("sort ASC, id ASC")
	if limit > 0 {
		q = q.Limit(limit)
	}
	if offset > 0 {
		q = q.Offset(offset)
	}
	if err := q.Find(&items).Error; err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

// FindAttachmentByID 按主键查询附件；不存在时返回 ErrNotFound。
func FindAttachmentByID(db *gorm.DB, id int64) (*PostAttachment, error) {
	var item PostAttachment
	if err := db.Where("id = ?", id).Take(&item).Error; err != nil {
		return nil, wrapErr(err)
	}
	return &item, nil
}

// CreateAttachment 写入附件并回填主键。
func CreateAttachment(db *gorm.DB, item *PostAttachment) error {
	return db.Create(item).Error
}

// UpdateAttachmentFields 按字段更新附件。
func UpdateAttachmentFields(db *gorm.DB, id int64, fields map[string]any) error {
	return db.Model(&PostAttachment{}).Where("id = ?", id).Updates(fields).Error
}

// DeleteAttachment 删除附件。
func DeleteAttachment(db *gorm.DB, id int64) error {
	return db.Where("id = ?", id).Delete(&PostAttachment{}).Error
}

// IncrementAttachmentClicks 给下载计数 +1（公开端 /go 用）。
func IncrementAttachmentClicks(db *gorm.DB, id int64) error {
	return db.Model(&PostAttachment{}).Where("id = ?", id).
		UpdateColumn("clicks", gorm.Expr("clicks + 1")).Error
}

// DeleteAttachmentsByPost 删除某文章的全部附件（物理删除文章时用）。
func DeleteAttachmentsByPost(db *gorm.DB, postID int64) error {
	return db.Where("post_id = ?", postID).Delete(&PostAttachment{}).Error
}

// CountAttachmentsByStorageKey 统计还有多少条附件记录指向同一个 storage_key（不含 excludeID）。
//
// 同一份文件可能被多篇文章复用（按 sha256 去重），删文件前必须先确认没有别的记录在用。
func CountAttachmentsByStorageKey(db *gorm.DB, storageKey string, excludeID int64) (int64, error) {
	var n int64
	err := db.Model(&PostAttachment{}).
		Where("storage_key = ? AND id <> ?", storageKey, excludeID).
		Count(&n).Error
	return n, err
}

// ListAttachmentStorageKeys 返回所有本地上传附件的 storage_key（存储管理扫孤儿用）。
func ListAttachmentStorageKeys(db *gorm.DB) ([]string, error) {
	keys := make([]string, 0)
	err := db.Model(&PostAttachment{}).
		Where("storage_key <> ''").
		Distinct().Pluck("storage_key", &keys).Error
	return keys, err
}

// ListAllAttachments 返回全部附件（存储管理对账用；本站附件量级很小）。
func ListAllAttachments(db *gorm.DB) ([]PostAttachment, error) {
	items := make([]PostAttachment, 0)
	if err := db.Order("id ASC").Find(&items).Error; err != nil {
		return nil, err
	}
	return items, nil
}
