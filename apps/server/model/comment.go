package model

import (
	"gorm.io/gorm"

	"github.com/unicornfairy864/blog-zjut-si/apps/server/model/common/dbtime"
)

// 评论状态枚举（agent.md §4）；禁止魔法字符串。
const (
	CommentStatusPending  = "pending"
	CommentStatusApproved = "approved"
	CommentStatusSpam     = "spam"
	CommentStatusDeleted  = "deleted"
)

// Comment 对应 comments 表；root_id 为顶层祖先的 id（顶层评论指向自身）。
//
// 公开端绝不输出 author_email / email_hash / ip_hash / ua（§9），故这三项 json:"-"。
type Comment struct {
	ID          int64       `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	PostID      int64       `gorm:"column:post_id" json:"postId"`
	ParentID    *int64      `gorm:"column:parent_id" json:"parentId"`
	RootID      *int64      `gorm:"column:root_id" json:"rootId"`
	AuthorName  string      `gorm:"column:author_name" json:"authorName"`
	AuthorEmail string      `gorm:"column:author_email" json:"-"`
	EmailHash   string      `gorm:"column:email_hash" json:"-"`
	AuthorURL   string      `gorm:"column:author_url" json:"authorUrl"`
	ContentMD   string      `gorm:"column:content_md" json:"contentMd"`
	ContentHTML string      `gorm:"column:content_html" json:"contentHtml"`
	Status      string      `gorm:"column:status" json:"status"`
	IPHash      string      `gorm:"column:ip_hash" json:"-"`
	UA          string      `gorm:"column:ua" json:"-"`
	CreatedAt   dbtime.Unix `gorm:"column:created_at" json:"createdAt"`
	UpdatedAt   dbtime.Unix `gorm:"column:updated_at" json:"updatedAt"`
}

// TableName 固定表名，避免 gorm 复数化。
func (Comment) TableName() string { return "comments" }

// PublicCommentReply 是公开列表里的回复：固定扁平一层，不再嵌套 replies（§5）。
type PublicCommentReply struct {
	ID          int64       `json:"id"`
	ParentID    *int64      `json:"parentId"`
	AuthorName  string      `json:"authorName"`
	AuthorURL   string      `json:"authorUrl"`
	ContentHTML string      `json:"contentHtml"`
	CreatedAt   dbtime.Unix `json:"createdAt"`
}

// PublicComment 是公开列表里的顶层评论；replies 收纳它的全部后代（含回复的回复）。
type PublicComment struct {
	ID          int64                `json:"id"`
	ParentID    *int64               `json:"parentId"`
	AuthorName  string               `json:"authorName"`
	AuthorURL   string               `json:"authorUrl"`
	ContentHTML string               `json:"contentHtml"`
	CreatedAt   dbtime.Unix          `json:"createdAt"`
	Replies     []PublicCommentReply `json:"replies"`
}

// PublicCommentList 是 GET /api/posts/{slug}/comments 的 data；total 含回复。
type PublicCommentList struct {
	Total int64           `json:"total"`
	List  []PublicComment `json:"list"`
}

// PublicTop 把实体裁剪成公开顶层评论；replies 由 service 填成空数组，保证形状稳定。
func (c Comment) PublicTop() PublicComment {
	return PublicComment{
		ID:          c.ID,
		ParentID:    c.ParentID,
		AuthorName:  c.AuthorName,
		AuthorURL:   c.AuthorURL,
		ContentHTML: c.ContentHTML,
		CreatedAt:   c.CreatedAt,
		Replies:     make([]PublicCommentReply, 0),
	}
}

// PublicReply 把实体裁剪成公开回复。
func (c Comment) PublicReply() PublicCommentReply {
	return PublicCommentReply{
		ID:          c.ID,
		ParentID:    c.ParentID,
		AuthorName:  c.AuthorName,
		AuthorURL:   c.AuthorURL,
		ContentHTML: c.ContentHTML,
		CreatedAt:   c.CreatedAt,
	}
}

// AdminCommentItem 是管理端列表与单条审核的输出；含 authorEmail（管理员可见），
// 但不含 ip_hash / ua（§9）。
type AdminCommentItem struct {
	ID          int64       `json:"id"`
	PostID      int64       `json:"postId"`
	PostSlug    string      `json:"postSlug"`
	PostTitle   string      `json:"postTitle"`
	ParentID    *int64      `json:"parentId"`
	AuthorName  string      `json:"authorName"`
	AuthorEmail string      `json:"authorEmail"`
	AuthorURL   string      `json:"authorUrl"`
	ContentMD   string      `json:"contentMd"`
	ContentHTML string      `json:"contentHtml"`
	Status      string      `json:"status"`
	CreatedAt   dbtime.Unix `json:"createdAt"`
}

// AdminCommentRow 是管理端列表的 join 结果：comments 的字段 + posts 的 slug/title。
type AdminCommentRow struct {
	ID          int64       `gorm:"column:id"`
	PostID      int64       `gorm:"column:post_id"`
	PostSlug    string      `gorm:"column:post_slug"`
	PostTitle   string      `gorm:"column:post_title"`
	ParentID    *int64      `gorm:"column:parent_id"`
	AuthorName  string      `gorm:"column:author_name"`
	AuthorEmail string      `gorm:"column:author_email"`
	AuthorURL   string      `gorm:"column:author_url"`
	ContentMD   string      `gorm:"column:content_md"`
	ContentHTML string      `gorm:"column:content_html"`
	Status      string      `gorm:"column:status"`
	CreatedAt   dbtime.Unix `gorm:"column:created_at"`
}

// Item 把 join 行裁剪成管理端输出。
//
// 两个结构体字段一一对应，直接转换即可（staticcheck S1016）。
func (r AdminCommentRow) Item() AdminCommentItem {
	return AdminCommentItem(r)
}

// adminCommentColumns 是管理端列表的显式列；posts 被物理删除时用 COALESCE 兜住 NULL。
const adminCommentColumns = `comments.id, comments.post_id,
	COALESCE(posts.slug, '') AS post_slug, COALESCE(posts.title, '') AS post_title,
	comments.parent_id, comments.author_name, comments.author_email, comments.author_url,
	comments.content_md, comments.content_html, comments.status, comments.created_at`

// adminCommentQuery 组装管理端列表的公共 FROM/WHERE。
//
// 帖子软删（posts.deleted_at 非空）也要能列出评论，故这里不过滤 deleted_at（§5）。
func adminCommentQuery(db *gorm.DB, status string) *gorm.DB {
	query := db.Table("comments").Joins("LEFT JOIN posts ON posts.id = comments.post_id")
	if status != "" {
		query = query.Where("comments.status = ?", status)
	}
	return query
}

// CreateComment 插入评论并回填自增主键。
func CreateComment(db *gorm.DB, comment *Comment) error {
	return db.Create(comment).Error
}

// FindCommentByID 按主键查询评论；不存在返回 ErrNotFound。
func FindCommentByID(db *gorm.DB, id int64) (*Comment, error) {
	var comment Comment
	if err := db.Where("id = ?", id).Take(&comment).Error; err != nil {
		return nil, wrapErr(err)
	}
	return &comment, nil
}

// SetCommentRootID 回填顶层评论的 root_id（顶层评论的 root_id 指向自身）。
func SetCommentRootID(db *gorm.DB, id, rootID int64) error {
	return db.Model(&Comment{}).Where("id = ?", id).Update("root_id", rootID).Error
}

// ListApprovedComments 返回某文章的全部 approved 评论，按 created_at 升序（公开列表用）。
func ListApprovedComments(db *gorm.DB, postID int64) ([]Comment, error) {
	comments := make([]Comment, 0)
	err := db.Where("post_id = ? AND status = ?", postID, CommentStatusApproved).
		Order("created_at ASC, id ASC").
		Find(&comments).Error
	if err != nil {
		return nil, err
	}
	return comments, nil
}

// CountApprovedComments 返回某文章的 approved 评论数（含回复）。
func CountApprovedComments(db *gorm.DB, postID int64) (int64, error) {
	var total int64
	err := db.Model(&Comment{}).
		Where("post_id = ? AND status = ?", postID, CommentStatusApproved).
		Count(&total).Error
	if err != nil {
		return 0, err
	}
	return total, nil
}

// ListCommentsForAdmin 返回一页评论与总数，按 created_at 倒序；status 为空表示不过滤。
func ListCommentsForAdmin(db *gorm.DB, status string, limit, offset int) ([]AdminCommentRow, int64, error) {
	var total int64
	if err := adminCommentQuery(db, status).Count(&total).Error; err != nil {
		return nil, 0, err
	}

	rows := make([]AdminCommentRow, 0)
	query := adminCommentQuery(db, status).
		Select(adminCommentColumns).
		Order("comments.created_at DESC, comments.id DESC")
	if limit > 0 {
		query = query.Limit(limit)
	}
	if offset > 0 {
		query = query.Offset(offset)
	}
	if err := query.Scan(&rows).Error; err != nil {
		return nil, 0, err
	}
	return rows, total, nil
}

// FindAdminCommentRow 按主键取管理端单条（含 posts 的 slug/title）；不存在返回 ErrNotFound。
func FindAdminCommentRow(db *gorm.DB, id int64) (*AdminCommentRow, error) {
	var row AdminCommentRow
	err := adminCommentQuery(db, "").
		Select(adminCommentColumns).
		Where("comments.id = ?", id).
		Take(&row).Error
	if err != nil {
		return nil, wrapErr(err)
	}
	return &row, nil
}

// UpdateCommentStatus 更新单条评论状态，返回受影响行数（0 表示评论不存在）。
func UpdateCommentStatus(db *gorm.DB, id int64, status string, at dbtime.Unix) (int64, error) {
	result := db.Model(&Comment{}).Where("id = ?", id).
		Updates(map[string]any{"status": status, "updated_at": at})
	return result.RowsAffected, result.Error
}

// BulkUpdateCommentStatus 批量更新评论状态，返回实际更新的条数。
func BulkUpdateCommentStatus(db *gorm.DB, ids []int64, status string, at dbtime.Unix) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	result := db.Model(&Comment{}).Where("id IN ?", ids).
		Updates(map[string]any{"status": status, "updated_at": at})
	return result.RowsAffected, result.Error
}

// ListCommentPostIDs 返回这批评论所属的文章 id（去重），用于重算 comment_count。
func ListCommentPostIDs(db *gorm.DB, ids []int64) ([]int64, error) {
	postIDs := make([]int64, 0, len(ids))
	if len(ids) == 0 {
		return postIDs, nil
	}
	if err := db.Model(&Comment{}).Distinct().Where("id IN ?", ids).Pluck("post_id", &postIDs).Error; err != nil {
		return nil, err
	}
	return postIDs, nil
}

// RecountPostCommentCount 重算单篇文章的 comment_count。
func RecountPostCommentCount(db *gorm.DB, postID int64) error {
	return RecountPostCommentCounts(db, []int64{postID})
}

// RecountPostCommentCounts 用一条 SQL 重算这批文章的 posts.comment_count（= approved 评论数）。
//
// 采用"整体重算"而不是增减：并发下不会漂，且与 comments 表天然一致（§5 副作用）。
func RecountPostCommentCounts(db *gorm.DB, postIDs []int64) error {
	if len(postIDs) == 0 {
		return nil
	}
	return db.Exec(`
		UPDATE posts SET comment_count = (
			SELECT COUNT(*) FROM comments
			WHERE comments.post_id = posts.id AND comments.status = ?
		)
		WHERE posts.id IN ?`, CommentStatusApproved, postIDs).Error
}
