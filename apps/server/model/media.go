package model

import (
	"gorm.io/gorm"

	"github.com/unicornfairy864/blog-zjut-si/apps/server/model/common/dbtime"
)

// 媒体状态（agent.md §4）；uploading 是内存态，不落库。
const (
	MediaStatusProcessing = "processing"
	MediaStatusReady      = "ready"
	MediaStatusFailed     = "failed"
)

// 存储驱动（agent.md §4）。
const (
	StorageDriverLocal     = "local"
	StorageDriverCloudreve = "cloudreve"
)

// Media 对应 media 表；永不存 URL，只存 storage_key 与 variants_json（§10）。
type Media struct {
	ID             int64        `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	SHA256         string       `gorm:"column:sha256" json:"sha256"`
	StorageDriver  string       `gorm:"column:storage_driver" json:"storageDriver"`
	StorageKey     string       `gorm:"column:storage_key" json:"storageKey"`
	OriginalName   string       `gorm:"column:original_name" json:"originalName"`
	Mime           string       `gorm:"column:mime" json:"mime"`
	Ext            string       `gorm:"column:ext" json:"ext"`
	Bytes          int64        `gorm:"column:bytes" json:"bytes"`
	Width          int          `gorm:"column:width" json:"width"`
	Height         int          `gorm:"column:height" json:"height"`
	Pixels         int64        `gorm:"column:pixels" json:"pixels"`
	VariantsJSON   string       `gorm:"column:variants_json" json:"-"`
	Blurhash       string       `gorm:"column:blurhash" json:"blurhash"`
	Alt            string       `gorm:"column:alt" json:"alt"`
	Status         string       `gorm:"column:status" json:"status"`
	Error          string       `gorm:"column:error" json:"error"`
	OrigArchived   bool         `gorm:"column:orig_archived" json:"origArchived"`
	OrigArchivedAt *dbtime.Unix `gorm:"column:orig_archived_at" json:"origArchivedAt"`
	CreatedAt      dbtime.Unix  `gorm:"column:created_at" json:"createdAt"`
	UpdatedAt      dbtime.Unix  `gorm:"column:updated_at" json:"updatedAt"`
}

// TableName 固定表名。
func (Media) TableName() string { return "media" }

// CreateMedia 插入媒体记录并回填主键。
func CreateMedia(db *gorm.DB, media *Media) error {
	return db.Create(media).Error
}

// FindMediaByID 按主键查询媒体。
func FindMediaByID(db *gorm.DB, id int64) (*Media, error) {
	var media Media
	if err := db.Where("id = ?", id).Take(&media).Error; err != nil {
		return nil, wrapErr(err)
	}
	return &media, nil
}

// FindMediaBySHA 按内容哈希查询媒体（上传去重）。
func FindMediaBySHA(db *gorm.DB, sha string) (*Media, error) {
	var media Media
	if err := db.Where("sha256 = ?", sha).Take(&media).Error; err != nil {
		return nil, wrapErr(err)
	}
	return &media, nil
}

// FindMediaByKeys 按 storage_key 批量查询媒体（正文图片装饰用）。
func FindMediaByKeys(db *gorm.DB, keys []string) ([]Media, error) {
	if len(keys) == 0 {
		return nil, nil
	}
	items := make([]Media, 0, len(keys))
	if err := db.Where("storage_key IN ?", keys).Find(&items).Error; err != nil {
		return nil, err
	}
	return items, nil
}

// ListMedia 按创建时间倒序返回一页媒体。
func ListMedia(db *gorm.DB, limit, offset int) ([]Media, int64, error) {
	var total int64
	if err := db.Model(&Media{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}

	items := make([]Media, 0)
	query := db.Model(&Media{}).Order("created_at DESC, id DESC")
	if limit > 0 {
		query = query.Limit(limit)
	}
	if offset > 0 {
		query = query.Offset(offset)
	}
	if err := query.Find(&items).Error; err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

// UpdateMediaFields 按字段更新媒体。
func UpdateMediaFields(db *gorm.DB, id int64, fields map[string]any) error {
	return db.Model(&Media{}).Where("id = ?", id).Updates(fields).Error
}

// DeleteMedia 物理删除媒体记录（文件由 service 清理）。
func DeleteMedia(db *gorm.DB, id int64) error {
	return db.Where("id = ?", id).Delete(&Media{}).Error
}

// MediaRef 是"图片仍被引用"的清单项（§10 删除前检查三处）。
type MediaRef struct {
	PostID int64  `gorm:"column:post_id" json:"postId"`
	Title  string `gorm:"column:title" json:"title"`
	Slug   string `gorm:"column:slug" json:"slug"`
	Kind   string `gorm:"column:kind" json:"kind"`
}

// FindMediaRefs 检查封面的三种引用：cover_media_id、og_media_id、正文含 storage_key。
func FindMediaRefs(db *gorm.DB, mediaID int64, storageKey string) ([]MediaRef, error) {
	refs := make([]MediaRef, 0)
	err := db.Raw(`
		SELECT id AS post_id, title, slug, 'cover' AS kind
		FROM posts WHERE cover_media_id = ? AND deleted_at IS NULL
		UNION ALL
		SELECT id, title, slug, 'og'
		FROM posts WHERE og_media_id = ? AND deleted_at IS NULL
		UNION ALL
		SELECT id, title, slug, 'content'
		FROM posts WHERE instr(content_md, ?) > 0 AND deleted_at IS NULL`,
		mediaID, mediaID, storageKey).Scan(&refs).Error
	if err != nil {
		return nil, err
	}
	return refs, nil
}
