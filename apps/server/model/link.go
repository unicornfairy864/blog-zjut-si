package model

import (
	"gorm.io/gorm"

	"github.com/unicornfairy864/blog-zjut-si/apps/server/model/common/dbtime"
)

// 友链状态枚举（agent.md §4）；公开端只出 active，禁止魔法字符串。
//
// 取值与 migrations/000001 的 `status TEXT NOT NULL DEFAULT 'active'` 对齐，
// 所以直接插入的友链默认就是可见的。
const (
	LinkStatusActive = "active"
	LinkStatusHidden = "hidden"
)

// Link 对应 links 表（agent.md §4）。
type Link struct {
	ID          int64       `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	Name        string      `gorm:"column:name" json:"name"`
	URL         string      `gorm:"column:url" json:"url"`
	Avatar      string      `gorm:"column:avatar" json:"avatar"`
	Description string      `gorm:"column:description" json:"description"`
	Sort        int         `gorm:"column:sort" json:"sort"`
	Status      string      `gorm:"column:status" json:"status"`
	CreatedAt   dbtime.Unix `gorm:"column:created_at" json:"createdAt"`
}

// TableName 固定表名。
func (Link) TableName() string { return "links" }

// PublicLinkItem 是公开友链项；不暴露 sort / status。
type PublicLinkItem struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	URL         string `json:"url"`
	Avatar      string `json:"avatar"`
	Description string `json:"description"`
}

// PublicItem 把实体裁剪成公开友链项。
func (l Link) PublicItem() PublicLinkItem {
	return PublicLinkItem{
		ID:          l.ID,
		Name:        l.Name,
		URL:         l.URL,
		Avatar:      l.Avatar,
		Description: l.Description,
	}
}

// ListActiveLinks 返回可见友链，按 sort 升序、id 升序（agent.md §5）。
func ListActiveLinks(db *gorm.DB) ([]Link, error) {
	links := make([]Link, 0)
	err := db.Where("status = ?", LinkStatusActive).
		Order("sort ASC, id ASC").
		Find(&links).Error
	if err != nil {
		return nil, err
	}
	return links, nil
}

// CountActiveLinks 返回可见友链数（/api/site 的 links 计数）。
func CountActiveLinks(db *gorm.DB) (int64, error) {
	var total int64
	if err := db.Model(&Link{}).Where("status = ?", LinkStatusActive).Count(&total).Error; err != nil {
		return 0, err
	}
	return total, nil
}
