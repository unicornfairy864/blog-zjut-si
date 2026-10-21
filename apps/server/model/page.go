package model

import (
	"gorm.io/gorm"

	"github.com/unicornfairy864/blog-zjut-si/apps/server/model/common/dbtime"
)

// Page 对应 pages 表（agent.md §4）；deleted_at 为整数秒，
// gorm 的软删插件不参与，必须显式过滤（与 posts 一致）。
type Page struct {
	ID             int64        `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	Slug           string       `gorm:"column:slug" json:"slug"`
	Title          string       `gorm:"column:title" json:"title"`
	ContentMD      string       `gorm:"column:content_md" json:"contentMd"`
	ContentHTML    string       `gorm:"column:content_html" json:"contentHtml"`
	ContentText    string       `gorm:"column:content_text" json:"-"`
	SEOTitle       string       `gorm:"column:seo_title" json:"seoTitle"`
	SEODescription string       `gorm:"column:seo_description" json:"seoDescription"`
	CreatedAt      dbtime.Unix  `gorm:"column:created_at" json:"createdAt"`
	UpdatedAt      dbtime.Unix  `gorm:"column:updated_at" json:"updatedAt"`
	DeletedAt      *dbtime.Unix `gorm:"column:deleted_at" json:"deletedAt"`
}

// TableName 固定表名。
func (Page) TableName() string { return "pages" }

// PublicPageDetail 是公开静态页详情；只出 HTML，不出 Markdown 源码（agent.md §5）。
type PublicPageDetail struct {
	Slug           string      `json:"slug"`
	Title          string      `json:"title"`
	ContentHTML    string      `json:"contentHtml"`
	SEOTitle       string      `json:"seoTitle"`
	SEODescription string      `json:"seoDescription"`
	UpdatedAt      dbtime.Unix `json:"updatedAt"`
}

// PublicDetail 把实体裁剪成公开详情。
func (p Page) PublicDetail() PublicPageDetail {
	return PublicPageDetail{
		Slug:           p.Slug,
		Title:          p.Title,
		ContentHTML:    p.ContentHTML,
		SEOTitle:       p.SEOTitle,
		SEODescription: p.SEODescription,
		UpdatedAt:      p.UpdatedAt,
	}
}

// PublicPageRef 是公开的静态页清单项（sitemap 用；不出正文）。
type PublicPageRef struct {
	Slug      string      `json:"slug"`
	Title     string      `json:"title"`
	UpdatedAt dbtime.Unix `json:"updatedAt"`
}

// ListPublicPages 返回全部未删除的静态页，按 slug 升序（公开 sitemap 用）。
func ListPublicPages(db *gorm.DB) ([]PublicPageRef, error) {
	items := make([]PublicPageRef, 0)
	err := db.Model(&Page{}).
		Select("slug, title, updated_at").
		Where("deleted_at IS NULL").
		Order("slug ASC").
		Scan(&items).Error
	if err != nil {
		return nil, err
	}
	return items, nil
}

// FindPageBySlug 按 slug 查询静态页；withDeleted 为 true 时包含软删记录（公开端用它返回 410）。
func FindPageBySlug(db *gorm.DB, slug string, withDeleted bool) (*Page, error) {
	query := db.Where("slug = ?", slug)
	if !withDeleted {
		query = query.Where("deleted_at IS NULL")
	}
	var page Page
	if err := query.Take(&page).Error; err != nil {
		return nil, wrapErr(err)
	}
	return &page, nil
}
