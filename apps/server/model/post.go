package model

import (
	"strings"

	"gorm.io/gorm"

	"github.com/unicornfairy864/blog-zjut-si/apps/server/model/common/dbtime"
)

// 文章状态枚举（agent.md §4）；禁止魔法字符串。
const (
	PostStatusDraft     = "draft"
	PostStatusScheduled = "scheduled"
	PostStatusPublished = "published"
	PostStatusArchived  = "archived"
)

// DefaultPostLang 是文章的默认语言。
const DefaultPostLang = "zh-CN"

// Post 对应 posts 表；deleted_at 为整数秒，gorm 的软删插件不参与，必须显式过滤。
type Post struct {
	ID             int64        `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	Slug           string       `gorm:"column:slug" json:"slug"`
	Title          string       `gorm:"column:title" json:"title"`
	Excerpt        string       `gorm:"column:excerpt" json:"excerpt"`
	ContentMD      string       `gorm:"column:content_md" json:"contentMd"`
	ContentHTML    string       `gorm:"column:content_html" json:"contentHtml"`
	ContentText    string       `gorm:"column:content_text" json:"-"`
	CoverMediaID   *int64       `gorm:"column:cover_media_id" json:"coverMediaId"`
	Status         string       `gorm:"column:status" json:"status"`
	PublishedAt    *dbtime.Unix `gorm:"column:published_at" json:"publishedAt"`
	CreatedAt      dbtime.Unix  `gorm:"column:created_at" json:"createdAt"`
	UpdatedAt      dbtime.Unix  `gorm:"column:updated_at" json:"updatedAt"`
	DeletedAt      *dbtime.Unix `gorm:"column:deleted_at" json:"deletedAt"`
	WordCount      int          `gorm:"column:word_count" json:"wordCount"`
	ReadingTime    int          `gorm:"column:reading_time" json:"readingTime"`
	IsPinned       bool         `gorm:"column:is_pinned" json:"isPinned"`
	AllowComments  bool         `gorm:"column:allow_comments" json:"allowComments"`
	SeriesID       *int64       `gorm:"column:series_id" json:"seriesId"`
	SeriesOrder    int          `gorm:"column:series_order" json:"seriesOrder"`
	SEOTitle       string       `gorm:"column:seo_title" json:"seoTitle"`
	SEODescription string       `gorm:"column:seo_description" json:"seoDescription"`
	CanonicalURL   string       `gorm:"column:canonical_url" json:"canonicalUrl"`
	OGMediaID      *int64       `gorm:"column:og_media_id" json:"ogMediaId"`
	ViewCount      int          `gorm:"column:view_count" json:"viewCount"`
	LikeCount      int          `gorm:"column:like_count" json:"likeCount"`
	CommentCount   int          `gorm:"column:comment_count" json:"commentCount"`
	Revision       int          `gorm:"column:revision" json:"revision"`
	Lang           string       `gorm:"column:lang" json:"lang"`
}

// TableName 固定表名，避免 gorm 复数化。
func (Post) TableName() string { return "posts" }

// PostListItem 是列表接口的输出；不包含正文，避免列表响应过大。
type PostListItem struct {
	ID            int64        `json:"id"`
	Slug          string       `json:"slug"`
	Title         string       `json:"title"`
	Excerpt       string       `json:"excerpt"`
	Status        string       `json:"status"`
	IsPinned      bool         `json:"isPinned"`
	AllowComments bool         `json:"allowComments"`
	WordCount     int          `json:"wordCount"`
	ReadingTime   int          `json:"readingTime"`
	Revision      int          `json:"revision"`
	Lang          string       `json:"lang"`
	PublishedAt   *dbtime.Unix `json:"publishedAt"`
	CreatedAt     dbtime.Unix  `json:"createdAt"`
	UpdatedAt     dbtime.Unix  `json:"updatedAt"`
}

// ListItem 把实体裁剪成列表项。
func (p Post) ListItem() PostListItem {
	return PostListItem{
		ID:            p.ID,
		Slug:          p.Slug,
		Title:         p.Title,
		Excerpt:       p.Excerpt,
		Status:        p.Status,
		IsPinned:      p.IsPinned,
		AllowComments: p.AllowComments,
		WordCount:     p.WordCount,
		ReadingTime:   p.ReadingTime,
		Revision:      p.Revision,
		Lang:          p.Lang,
		PublishedAt:   p.PublishedAt,
		CreatedAt:     p.CreatedAt,
		UpdatedAt:     p.UpdatedAt,
	}
}

// PostListFilter 是管理端列表的筛选条件。
type PostListFilter struct {
	Status  string
	Keyword string
	Limit   int
	Offset  int
}

// postListQuery 组装列表的公共 WHERE；调用方各自追加排序与分页，避免复用同一个 *gorm.DB。
func postListQuery(db *gorm.DB, f PostListFilter) *gorm.DB {
	query := db.Model(&Post{}).Where("deleted_at IS NULL")
	if f.Status != "" {
		query = query.Where("status = ?", f.Status)
	}
	if f.Keyword != "" {
		like := "%" + escapeLike(f.Keyword) + "%"
		query = query.Where(`(title LIKE ? ESCAPE '\' OR slug LIKE ? ESCAPE '\')`, like, like)
	}
	return query
}

// ListPosts 返回一页文章与总数。
func ListPosts(db *gorm.DB, f PostListFilter) ([]Post, int64, error) {
	var total int64
	if err := postListQuery(db, f).Count(&total).Error; err != nil {
		return nil, 0, err
	}

	posts := make([]Post, 0)
	query := postListQuery(db, f).Order("updated_at DESC, id DESC")
	if f.Limit > 0 {
		query = query.Limit(f.Limit)
	}
	if f.Offset > 0 {
		query = query.Offset(f.Offset)
	}
	if err := query.Find(&posts).Error; err != nil {
		return nil, 0, err
	}
	return posts, total, nil
}

// FindPostByID 按主键查询文章；withDeleted 为 false 时忽略回收站记录。
func FindPostByID(db *gorm.DB, id int64, withDeleted bool) (*Post, error) {
	query := db.Where("id = ?", id)
	if !withDeleted {
		query = query.Where("deleted_at IS NULL")
	}
	var post Post
	if err := query.Take(&post).Error; err != nil {
		return nil, wrapErr(err)
	}
	return &post, nil
}

// PostSlugExists 报告 slug 是否已被占用；excludeID 用于更新场景排除自身。
func PostSlugExists(db *gorm.DB, slug string, excludeID int64) (bool, error) {
	query := db.Model(&Post{}).Where("slug = ?", slug)
	if excludeID > 0 {
		query = query.Where("id <> ?", excludeID)
	}
	var n int64
	if err := query.Count(&n).Error; err != nil {
		return false, err
	}
	return n > 0, nil
}

// CreatePost 插入文章并回填自增主键。
func CreatePost(db *gorm.DB, post *Post) error {
	return db.Create(post).Error
}

// UpdatePostFields 按字段更新文章；fields 里的 nil 会写成 NULL。
func UpdatePostFields(db *gorm.DB, id int64, fields map[string]any) error {
	return db.Model(&Post{}).Where("id = ?", id).Updates(fields).Error
}

// SoftDeletePost 把文章移入回收站（30 天后由 cleanup 物理删除）。
func SoftDeletePost(db *gorm.DB, id int64, at dbtime.Unix) error {
	return db.Model(&Post{}).Where("id = ? AND deleted_at IS NULL", id).
		Updates(map[string]any{"deleted_at": at, "updated_at": at}).Error
}

// escapeLike 转义 LIKE 的通配符；配合 ESCAPE '\' 使用，避免用户输入把整表扫出来。
func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}
