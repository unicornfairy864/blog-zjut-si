package model

import (
	"gorm.io/gorm"

	"github.com/unicornfairy864/blog-zjut-si/apps/server/model/common/dbtime"
)

// Series 对应 series 表（agent.md §4）；系列没有状态列，公开端按已发布文章计数。
type Series struct {
	ID          int64       `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	Name        string      `gorm:"column:name" json:"name"`
	Slug        string      `gorm:"column:slug" json:"slug"`
	Description string      `gorm:"column:description" json:"description"`
	CreatedAt   dbtime.Unix `gorm:"column:created_at" json:"createdAt"`
}

// TableName 固定表名。
func (Series) TableName() string { return "series" }

// SeriesItem 是系列的公开基础信息（系列详情用，不含 count）。
type SeriesItem struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Slug        string `json:"slug"`
	Description string `json:"description"`
}

// SeriesWithCount 是公开系列列表项：count 为该系列下的已发布文章数。
type SeriesWithCount struct {
	SeriesItem
	Count int64 `json:"count"`
}

// Item 把实体裁剪成公开基础信息。
func (s Series) Item() SeriesItem {
	return SeriesItem{ID: s.ID, Name: s.Name, Slug: s.Slug, Description: s.Description}
}

// FindSeriesBySlug 按 slug 查询系列；不存在时返回 ErrNotFound。
func FindSeriesBySlug(db *gorm.DB, slug string) (*Series, error) {
	var series Series
	if err := db.Where("slug = ?", slug).Take(&series).Error; err != nil {
		return nil, wrapErr(err)
	}
	return &series, nil
}

// ListPublicSeries 返回公开系列列表：暂无已发布文章的系列也保留（count 为 0），
// 按文章数降序、名称升序（agent.md §5）。
func ListPublicSeries(db *gorm.DB) ([]SeriesWithCount, error) {
	items := make([]SeriesWithCount, 0)
	err := db.Table("series").
		Select("series.id AS id, series.name AS name, series.slug AS slug, "+
			"series.description AS description, COUNT(posts.id) AS count").
		Joins("LEFT JOIN posts ON posts.series_id = series.id AND posts.status = ? AND posts.deleted_at IS NULL",
			PostStatusPublished).
		Group("series.id").
		Order("count DESC, series.name ASC").
		Scan(&items).Error
	if err != nil {
		return nil, err
	}
	return items, nil
}

// CountSeries 返回系列总数，口径与 ListPublicSeries 一致。
func CountSeries(db *gorm.DB) (int64, error) {
	var total int64
	if err := db.Model(&Series{}).Count(&total).Error; err != nil {
		return 0, err
	}
	return total, nil
}

// FindSeriesNames 一次取回一批系列的名称，键为 id（管理端列表回显用，禁止逐篇查询）。
func FindSeriesNames(db *gorm.DB, ids []int64) (map[int64]string, error) {
	names := make(map[int64]string, len(ids))
	if len(ids) == 0 {
		return names, nil
	}

	items := make([]Series, 0, len(ids))
	if err := db.Where("id IN ?", ids).Find(&items).Error; err != nil {
		return nil, err
	}
	for _, item := range items {
		names[item.ID] = item.Name
	}
	return names, nil
}

// ListSeriesPublishedPosts 返回某系列下全部已发布文章：
// 按 series_order 升序、published_at 升序（agent.md §5）。
func ListSeriesPublishedPosts(db *gorm.DB, seriesID int64) ([]Post, error) {
	posts := make([]Post, 0)
	err := db.Where("series_id = ? AND status = ? AND deleted_at IS NULL", seriesID, PostStatusPublished).
		Order("series_order ASC, published_at ASC, id ASC").
		Find(&posts).Error
	if err != nil {
		return nil, err
	}
	return posts, nil
}
