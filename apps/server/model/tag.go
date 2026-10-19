package model

import (
	"gorm.io/gorm"

	"github.com/unicornfairy864/blog-zjut-si/apps/server/model/common/dbtime"
)

// Tag 对应 tags 表（agent.md §4）；标签没有状态列，公开端按"是否挂着已发布文章"过滤。
type Tag struct {
	ID          int64       `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	Name        string      `gorm:"column:name" json:"name"`
	Slug        string      `gorm:"column:slug" json:"slug"`
	Description string      `gorm:"column:description" json:"description"`
	CreatedAt   dbtime.Unix `gorm:"column:created_at" json:"createdAt"`
}

// TableName 固定表名，避免 gorm 复数化。
func (Tag) TableName() string { return "tags" }

// PostTag 对应 post_tags 表：文章与标签的多对多关系。
type PostTag struct {
	PostID int64 `gorm:"column:post_id;primaryKey" json:"postId"`
	TagID  int64 `gorm:"column:tag_id;primaryKey" json:"tagId"`
}

// TableName 固定表名。
func (PostTag) TableName() string { return "post_tags" }

// TagItem 是标签的公开基础信息（标签详情用，不含 count）。
type TagItem struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Slug        string `json:"slug"`
	Description string `json:"description"`
}

// TagWithCount 是公开标签列表项：count 为该标签下的已发布文章数。
type TagWithCount struct {
	TagItem
	Count int64 `json:"count"`
}

// Item 把实体裁剪成公开基础信息。
func (t Tag) Item() TagItem {
	return TagItem{ID: t.ID, Name: t.Name, Slug: t.Slug, Description: t.Description}
}

// PostTagRef 是挂在文章上的标签引用；公开列表与详情只出名称与 slug。
type PostTagRef struct {
	Name string `json:"name"`
	Slug string `json:"slug"`
}

// FindTagBySlug 按 slug 查询标签；不存在时返回 ErrNotFound。
func FindTagBySlug(db *gorm.DB, slug string) (*Tag, error) {
	var tag Tag
	if err := db.Where("slug = ?", slug).Take(&tag).Error; err != nil {
		return nil, wrapErr(err)
	}
	return &tag, nil
}

// ListPublicTags 返回公开标签列表：只含至少一篇已发布文章的标签，
// 按文章数降序、名称升序（agent.md §5）。
func ListPublicTags(db *gorm.DB) ([]TagWithCount, error) {
	tags := make([]TagWithCount, 0)
	err := db.Table("tags").
		Select("tags.id AS id, tags.name AS name, tags.slug AS slug, "+
			"tags.description AS description, COUNT(posts.id) AS count").
		Joins("JOIN post_tags ON post_tags.tag_id = tags.id").
		Joins("JOIN posts ON posts.id = post_tags.post_id AND posts.status = ? AND posts.deleted_at IS NULL",
			PostStatusPublished).
		Group("tags.id").
		Order("count DESC, tags.name ASC").
		Scan(&tags).Error
	if err != nil {
		return nil, err
	}
	return tags, nil
}

// CountPublicTags 返回公开标签数，口径与 ListPublicTags 一致（count 为 0 的标签不计）。
func CountPublicTags(db *gorm.DB) (int64, error) {
	var total int64
	err := db.Table("tags").
		Where(`EXISTS (
			SELECT 1 FROM post_tags
			JOIN posts ON posts.id = post_tags.post_id
			WHERE post_tags.tag_id = tags.id AND posts.status = ? AND posts.deleted_at IS NULL
		)`, PostStatusPublished).
		Count(&total).Error
	if err != nil {
		return 0, err
	}
	return total, nil
}

// FindTagsByPostIDs 一次 join 取回一批文章的标签，按名称升序；禁止逐篇查询（§12）。
func FindTagsByPostIDs(db *gorm.DB, postIDs []int64) (map[int64][]PostTagRef, error) {
	result := make(map[int64][]PostTagRef, len(postIDs))
	if len(postIDs) == 0 {
		return result, nil
	}

	type postTagRow struct {
		PostID int64  `gorm:"column:post_id"`
		Name   string `gorm:"column:name"`
		Slug   string `gorm:"column:slug"`
	}

	rows := make([]postTagRow, 0, len(postIDs))
	err := db.Table("post_tags").
		Select("post_tags.post_id AS post_id, tags.name AS name, tags.slug AS slug").
		Joins("JOIN tags ON tags.id = post_tags.tag_id").
		Where("post_tags.post_id IN ?", postIDs).
		Order("tags.name ASC, tags.id ASC").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}

	for _, row := range rows {
		result[row.PostID] = append(result[row.PostID], PostTagRef{Name: row.Name, Slug: row.Slug})
	}
	return result, nil
}
