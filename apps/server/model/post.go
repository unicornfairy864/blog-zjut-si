package model

import (
	"errors"
	"strings"
	"unicode/utf8"

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

// FindPostBySlug 按 slug 查询；withDeleted 为 true 时包含回收站记录（公开端用它返回 410）。
func FindPostBySlug(db *gorm.DB, slug string, withDeleted bool) (*Post, error) {
	query := db.Where("slug = ?", slug)
	if !withDeleted {
		query = query.Where("deleted_at IS NULL")
	}
	var post Post
	if err := query.Take(&post).Error; err != nil {
		return nil, wrapErr(err)
	}
	return &post, nil
}

// PublicPostFilter 是公开列表的筛选条件；标签与系列在 D9–D10 再扩展。
type PublicPostFilter struct {
	Limit  int
	Offset int
}

// ListPublishedPosts 返回已发布文章：置顶优先，发布时间倒序（§11 草稿隔离）。
func ListPublishedPosts(db *gorm.DB, f PublicPostFilter) ([]Post, int64, error) {
	published := func() *gorm.DB {
		return db.Model(&Post{}).Where("status = ? AND deleted_at IS NULL", PostStatusPublished)
	}

	var total int64
	if err := published().Count(&total).Error; err != nil {
		return nil, 0, err
	}

	posts := make([]Post, 0)
	query := published().Order("is_pinned DESC, published_at DESC, id DESC")
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

// SearchPublishedPosts 搜索已发布文章：≥3 字走 FTS5 trigram，1–2 字走 LIKE 兜底（§7.1 V1）。
func SearchPublishedPosts(db *gorm.DB, keyword string, limit, offset int) ([]Post, int64, error) {
	if utf8.RuneCountInString(keyword) >= 3 {
		return searchPublishedFTS(db, keyword, limit, offset)
	}
	return searchPublishedLike(db, keyword, limit, offset)
}

// searchPublishedFTS 用 FTS5 的 bm25 排序；查询串整体加引号并转义内部双引号（§9）。
func searchPublishedFTS(db *gorm.DB, keyword string, limit, offset int) ([]Post, int64, error) {
	match := `"` + strings.ReplaceAll(keyword, `"`, `""`) + `"`

	var total int64
	row := db.Raw(`
		SELECT count(*)
		FROM posts_fts
		JOIN posts ON posts.id = posts_fts.rowid
		WHERE posts_fts MATCH ? AND posts.status = ? AND posts.deleted_at IS NULL`,
		match, PostStatusPublished).Row()
	if err := row.Scan(&total); err != nil {
		return nil, 0, err
	}

	posts := make([]Post, 0)
	if err := db.Raw(`
		SELECT posts.*
		FROM posts_fts
		JOIN posts ON posts.id = posts_fts.rowid
		WHERE posts_fts MATCH ? AND posts.status = ? AND posts.deleted_at IS NULL
		ORDER BY bm25(posts_fts), posts.published_at DESC
		LIMIT ? OFFSET ?`,
		match, PostStatusPublished, limit, offset).Scan(&posts).Error; err != nil {
		return nil, 0, err
	}
	return posts, total, nil
}

// searchPublishedLike 覆盖 1–2 字关键词；trigram 分词无法索引这么短的串。
func searchPublishedLike(db *gorm.DB, keyword string, limit, offset int) ([]Post, int64, error) {
	like := "%" + escapeLike(keyword) + "%"
	matching := func() *gorm.DB {
		return db.Model(&Post{}).
			Where("status = ? AND deleted_at IS NULL", PostStatusPublished).
			Where(`(title LIKE ? ESCAPE '\' OR excerpt LIKE ? ESCAPE '\' OR content_text LIKE ? ESCAPE '\')`,
				like, like, like)
	}

	var total int64
	if err := matching().Count(&total).Error; err != nil {
		return nil, 0, err
	}

	posts := make([]Post, 0)
	query := matching().Order("is_pinned DESC, published_at DESC, id DESC")
	if limit > 0 {
		query = query.Limit(limit)
	}
	if offset > 0 {
		query = query.Offset(offset)
	}
	if err := query.Find(&posts).Error; err != nil {
		return nil, 0, err
	}
	return posts, total, nil
}

// CountPublishedPosts 返回已发布文章数（/api/site 的 posts 计数）。
func CountPublishedPosts(db *gorm.DB) (int64, error) {
	var total int64
	err := db.Model(&Post{}).
		Where("status = ? AND deleted_at IS NULL", PostStatusPublished).
		Count(&total).Error
	if err != nil {
		return 0, err
	}
	return total, nil
}

// ListPublishedForArchive 返回归档所需的已发布文章字段，按发布时间倒序（§5 归档）。
func ListPublishedForArchive(db *gorm.DB) ([]Post, error) {
	posts := make([]Post, 0)
	err := db.Model(&Post{}).
		Select("id", "slug", "title", "published_at").
		Where("status = ? AND deleted_at IS NULL AND published_at IS NOT NULL", PostStatusPublished).
		Order("published_at DESC, id DESC").
		Find(&posts).Error
	if err != nil {
		return nil, err
	}
	return posts, nil
}

// ListTagPublishedPosts 返回某标签下已发布文章的一页，按发布时间倒序。
func ListTagPublishedPosts(db *gorm.DB, tagID int64, limit, offset int) ([]Post, int64, error) {
	filter := func() *gorm.DB {
		return db.Model(&Post{}).
			Joins("JOIN post_tags ON post_tags.post_id = posts.id").
			Where("post_tags.tag_id = ? AND posts.status = ? AND posts.deleted_at IS NULL",
				tagID, PostStatusPublished)
	}

	// post_tags 的主键是 (post_id, tag_id)，固定 tag_id 后每篇文章只会命中一次。
	var total int64
	if err := filter().Count(&total).Error; err != nil {
		return nil, 0, err
	}

	posts := make([]Post, 0)
	query := filter().Select("posts.*").Order("posts.published_at DESC, posts.id DESC")
	if limit > 0 {
		query = query.Limit(limit)
	}
	if offset > 0 {
		query = query.Offset(offset)
	}
	if err := query.Find(&posts).Error; err != nil {
		return nil, 0, err
	}
	return posts, total, nil
}

// ListRelatedPublishedPosts 返回相关文章：同标签**或**同系列、排除自身、仅已发布，
// 按发布时间倒序取前 limit 篇（§5）；没有关联时返回空切片。
func ListRelatedPublishedPosts(db *gorm.DB, post *Post, limit int) ([]Post, error) {
	posts := make([]Post, 0)
	if limit <= 0 {
		return posts, nil
	}

	// series_id 为 NULL 时 `posts.series_id = NULL` 恒为 NULL，OR 自动退化为只看标签。
	var seriesID any
	if post.SeriesID != nil {
		seriesID = *post.SeriesID
	}

	err := db.Raw(`
		SELECT DISTINCT posts.*
		FROM posts
		WHERE posts.status = ? AND posts.deleted_at IS NULL AND posts.id <> ?
		  AND (
			posts.series_id = ?
			OR EXISTS (
				SELECT 1
				FROM post_tags AS mine
				JOIN post_tags AS other ON other.tag_id = mine.tag_id
				WHERE mine.post_id = ? AND other.post_id = posts.id
			)
		  )
		ORDER BY posts.published_at DESC, posts.id DESC
		LIMIT ?`, PostStatusPublished, post.ID, seriesID, post.ID, limit).Scan(&posts).Error
	if err != nil {
		return nil, err
	}
	return posts, nil
}

// FindAdjacentPublishedPosts 返回按 published_at 相邻的上一篇与下一篇；
// 仅同状态（published），边界返回 nil（§11）。
func FindAdjacentPublishedPosts(db *gorm.DB, post *Post) (prev, next *Post, err error) {
	if post.PublishedAt == nil {
		return nil, nil, nil
	}
	at := *post.PublishedAt

	var before Post
	err = db.Where("status = ? AND deleted_at IS NULL AND (published_at < ? OR (published_at = ? AND id < ?))",
		PostStatusPublished, at, at, post.ID).
		Order("published_at DESC, id DESC").
		Take(&before).Error
	switch {
	case err == nil:
		prev = &before
	case errors.Is(err, gorm.ErrRecordNotFound):
	default:
		return nil, nil, err
	}

	var after Post
	err = db.Where("status = ? AND deleted_at IS NULL AND (published_at > ? OR (published_at = ? AND id > ?))",
		PostStatusPublished, at, at, post.ID).
		Order("published_at ASC, id ASC").
		Take(&after).Error
	switch {
	case err == nil:
		next = &after
	case errors.Is(err, gorm.ErrRecordNotFound):
	default:
		return nil, nil, err
	}
	return prev, next, nil
}

// PublicCover 是公开列表项与详情的封面；URL 规则与媒体库一致（§10）。
//
// 没有封面时整个字段为 null；变体未生成时 variants 为空对象、url 给出 1600 的预期地址。
type PublicCover struct {
	SHA256   string            `json:"sha256"`
	Ext      string            `json:"ext"`
	Width    int               `json:"width"`
	Height   int               `json:"height"`
	Alt      string            `json:"alt"`
	Blurhash string            `json:"blurhash"`
	URL      string            `json:"url"`
	OrigURL  string            `json:"origUrl"`
	Variants map[string]string `json:"variants"`
}

// PostNeighbor 是详情里的上一篇 / 下一篇引用（§11）。
type PostNeighbor struct {
	Slug  string `json:"slug"`
	Title string `json:"title"`
}

// PublicPostListItem 是公开列表项；不包含正文（§12 列表与详情分离）。
type PublicPostListItem struct {
	ID           int64        `json:"id"`
	Slug         string       `json:"slug"`
	Title        string       `json:"title"`
	Excerpt      string       `json:"excerpt"`
	CoverMediaID *int64       `json:"coverMediaId"`
	Cover        *PublicCover `json:"cover"`
	Tags         []PostTagRef `json:"tags"`
	PublishedAt  *dbtime.Unix `json:"publishedAt"`
	UpdatedAt    dbtime.Unix  `json:"updatedAt"`
	WordCount    int          `json:"wordCount"`
	ReadingTime  int          `json:"readingTime"`
	IsPinned     bool         `json:"isPinned"`
}

// PublicListItem 把实体裁剪成公开列表项；封面与标签由 service 批量补齐。
func (p Post) PublicListItem() PublicPostListItem {
	return PublicPostListItem{
		ID:           p.ID,
		Slug:         p.Slug,
		Title:        p.Title,
		Excerpt:      p.Excerpt,
		CoverMediaID: p.CoverMediaID,
		Tags:         make([]PostTagRef, 0),
		PublishedAt:  p.PublishedAt,
		UpdatedAt:    p.UpdatedAt,
		WordCount:    p.WordCount,
		ReadingTime:  p.ReadingTime,
		IsPinned:     p.IsPinned,
	}
}

// PublicPostDetail 是公开文章详情；只出 HTML，不出 Markdown 源码。
type PublicPostDetail struct {
	PublicPostListItem
	ContentHTML    string        `json:"contentHtml"`
	SEOTitle       string        `json:"seoTitle"`
	SEODescription string        `json:"seoDescription"`
	CanonicalURL   string        `json:"canonicalUrl"`
	Lang           string        `json:"lang"`
	Prev           *PostNeighbor `json:"prev"`
	Next           *PostNeighbor `json:"next"`
}

// PublicDetail 把实体裁剪成公开详情；封面、标签与相邻文章由 service 补齐。
func (p Post) PublicDetail() PublicPostDetail {
	return PublicPostDetail{
		PublicPostListItem: p.PublicListItem(),
		ContentHTML:        p.ContentHTML,
		SEOTitle:           p.SEOTitle,
		SEODescription:     p.SEODescription,
		CanonicalURL:       p.CanonicalURL,
		Lang:               p.Lang,
	}
}
