package model

import (
	"gorm.io/gorm"

	"github.com/unicornfairy864/blog-zjut-si/apps/server/model/common/dbtime"
)

// StatsSitePostID 是 stats_daily 里全站聚合行的 post_id（agent.md §4：PK 不允许 NULL）。
const StatsSitePostID int64 = 0

// Like 对应 likes 表；主键 (post_id, visitor_hash)，重复点赞靠 INSERT OR IGNORE 幂等（§4 / §5）。
type Like struct {
	PostID      int64       `gorm:"column:post_id;primaryKey" json:"postId"`
	VisitorHash string      `gorm:"column:visitor_hash;primaryKey" json:"-"`
	CreatedAt   dbtime.Unix `gorm:"column:created_at" json:"createdAt"`
}

// TableName 固定表名，避免 gorm 复数化。
func (Like) TableName() string { return "likes" }

// PageView 对应 page_views 表；post_id 为 NULL 表示非文章页（§4）。
//
// 明细保留 90 天，由 cleanup 任务裁剪。
type PageView struct {
	ID           int64       `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	PostID       *int64      `gorm:"column:post_id" json:"postId"`
	Path         string      `gorm:"column:path" json:"path"`
	VisitorHash  string      `gorm:"column:visitor_hash" json:"-"`
	ReferrerHost string      `gorm:"column:referrer_host" json:"referrerHost"`
	CreatedAt    dbtime.Unix `gorm:"column:created_at" json:"createdAt"`
}

// TableName 固定表名。
func (PageView) TableName() string { return "page_views" }

// StatsDaily 对应 stats_daily 表；date 是 Asia/Shanghai 的 YYYY-MM-DD（§4）。
type StatsDaily struct {
	Date   string `gorm:"column:date;primaryKey" json:"date"`
	PostID int64  `gorm:"column:post_id;primaryKey" json:"postId"`
	PV     int64  `gorm:"column:pv" json:"pv"`
	UV     int64  `gorm:"column:uv" json:"uv"`
}

// TableName 固定表名。
func (StatsDaily) TableName() string { return "stats_daily" }

// StatsStatusCount 是一个状态分组的计数（管理端看板用）。
type StatsStatusCount struct {
	Status string `gorm:"column:status" json:"status"`
	Total  int64  `gorm:"column:total" json:"total"`
}

// TopViewedPost 是看板里的热门文章行。
type TopViewedPost struct {
	ID        int64  `gorm:"column:id" json:"id"`
	Slug      string `gorm:"column:slug" json:"slug"`
	Title     string `gorm:"column:title" json:"title"`
	ViewCount int64  `gorm:"column:view_count" json:"viewCount"`
	LikeCount int64  `gorm:"column:like_count" json:"likeCount"`
}

// InsertLike 插入点赞；已点过时返回 false（幂等，不报错）。
func InsertLike(db *gorm.DB, postID int64, visitorHash string, at dbtime.Unix) (bool, error) {
	result := db.Exec(
		`INSERT OR IGNORE INTO likes (post_id, visitor_hash, created_at) VALUES (?, ?, ?)`,
		postID, visitorHash, int64(at))
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}

// DeleteLike 取消点赞；没点过时返回 false（幂等，不报错）。
func DeleteLike(db *gorm.DB, postID int64, visitorHash string) (bool, error) {
	result := db.Exec(`DELETE FROM likes WHERE post_id = ? AND visitor_hash = ?`, postID, visitorHash)
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}

// HasLiked 报告该访客是否点过赞。
func HasLiked(db *gorm.DB, postID int64, visitorHash string) (bool, error) {
	var total int64
	err := db.Model(&Like{}).
		Where("post_id = ? AND visitor_hash = ?", postID, visitorHash).
		Count(&total).Error
	if err != nil {
		return false, err
	}
	return total > 0, nil
}

// CountLikes 统计某文章的点赞数。
func CountLikes(db *gorm.DB, postID int64) (int64, error) {
	var total int64
	if err := db.Model(&Like{}).Where("post_id = ?", postID).Count(&total).Error; err != nil {
		return 0, err
	}
	return total, nil
}

// CountLikesSince 统计 since（含）之后的点赞数（看板的「今日点赞」）。
func CountLikesSince(db *gorm.DB, since dbtime.Unix) (int64, error) {
	var total int64
	err := db.Model(&Like{}).Where("created_at >= ?", int64(since)).Count(&total).Error
	if err != nil {
		return 0, err
	}
	return total, nil
}

// SyncPostLikeCount 按 likes 表重算并写回 posts.like_count，返回最新值。
//
// 用 UpdateColumn 而不是 Update：点赞不该动 posts.updated_at（会影响 sitemap 与编辑器的"已保存"提示）。
func SyncPostLikeCount(db *gorm.DB, postID int64) (int64, error) {
	count, err := CountLikes(db, postID)
	if err != nil {
		return 0, err
	}
	err = db.Model(&Post{}).Where("id = ?", postID).UpdateColumn("like_count", count).Error
	if err != nil {
		return 0, err
	}
	return count, nil
}

// InsertPageView 写一条访问明细（每次打点都写，PV 计数是否 +1 由 service 决定）。
func InsertPageView(db *gorm.DB, view PageView) error {
	return db.Create(&view).Error
}

// HasRecentView 报告该访客在 since（含）之后是否已访问过同一目标：
// 文章页按 post_id 判定，非文章页按 path 判定（§5「同 visitor 10 分钟内 PV 只 +1」）。
func HasRecentView(db *gorm.DB, postID *int64, path, visitorHash string, since dbtime.Unix) (bool, error) {
	query := db.Model(&PageView{}).Where("visitor_hash = ? AND created_at >= ?", visitorHash, int64(since))
	if postID != nil {
		query = query.Where("post_id = ?", *postID)
	} else {
		query = query.Where("post_id IS NULL AND path = ?", path)
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return false, err
	}
	return total > 0, nil
}

// CountDistinctVisitors 统计 [from, to) 内的去重访客数；postID 为 nil 时统计全站（含非文章页）。
func CountDistinctVisitors(db *gorm.DB, postID *int64, from, to dbtime.Unix) (int64, error) {
	query := db.Model(&PageView{}).Where("created_at >= ? AND created_at < ?", int64(from), int64(to))
	if postID != nil {
		query = query.Where("post_id = ?", *postID)
	}

	var total int64
	if err := query.Distinct("visitor_hash").Count(&total).Error; err != nil {
		return 0, err
	}
	return total, nil
}

// UpsertStatsDailyPV 给 (date, postID) 行加 pv；行不存在时插入。
func UpsertStatsDailyPV(db *gorm.DB, date string, postID, pv int64) error {
	return db.Exec(`
		INSERT INTO stats_daily (date, post_id, pv, uv) VALUES (?, ?, ?, 0)
		ON CONFLICT(date, post_id) DO UPDATE SET pv = pv + excluded.pv`,
		date, postID, pv).Error
}

// UpdateStatsDailyUV 覆盖 (date, postID) 行的 uv；行不存在时插入。
func UpdateStatsDailyUV(db *gorm.DB, date string, postID, uv int64) error {
	return db.Exec(`
		INSERT INTO stats_daily (date, post_id, pv, uv) VALUES (?, ?, 0, ?)
		ON CONFLICT(date, post_id) DO UPDATE SET uv = excluded.uv`,
		date, postID, uv).Error
}

// FindStatsDaily 读取某行；行不存在时返回 ErrNotFound。
func FindStatsDaily(db *gorm.DB, date string, postID int64) (*StatsDaily, error) {
	var row StatsDaily
	err := db.Where("date = ? AND post_id = ?", date, postID).Take(&row).Error
	if err != nil {
		return nil, wrapErr(err)
	}
	return &row, nil
}

// ListStatsDaily 返回 [from, to] 的全站日粒度行，按日期升序；没有数据的日期不补零行。
func ListStatsDaily(db *gorm.DB, from, to string) ([]StatsDaily, error) {
	rows := make([]StatsDaily, 0)
	err := db.Where("post_id = ? AND date >= ? AND date <= ?", StatsSitePostID, from, to).
		Order("date ASC").
		Find(&rows).Error
	if err != nil {
		return nil, err
	}
	return rows, nil
}

// SumStatsDaily 汇总 [from, to] 全站行的 pv 与 uv 合计。
func SumStatsDaily(db *gorm.DB, from, to string) (pv, uv int64, err error) {
	var row struct {
		PV int64 `gorm:"column:pv"`
		UV int64 `gorm:"column:uv"`
	}
	err = db.Model(&StatsDaily{}).
		Select("COALESCE(SUM(pv), 0) AS pv, COALESCE(SUM(uv), 0) AS uv").
		Where("post_id = ? AND date >= ? AND date <= ?", StatsSitePostID, from, to).
		Take(&row).Error
	if err != nil {
		return 0, 0, err
	}
	return row.PV, row.UV, nil
}

// CountPostsByStatus 按状态统计未删除文章数（看板用）。
func CountPostsByStatus(db *gorm.DB) ([]StatsStatusCount, error) {
	rows := make([]StatsStatusCount, 0, 4)
	err := db.Model(&Post{}).
		Select("status, COUNT(*) AS total").
		Where("deleted_at IS NULL").
		Group("status").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	return rows, nil
}

// CountDeletedPosts 统计回收站里的文章数。
func CountDeletedPosts(db *gorm.DB) (int64, error) {
	var total int64
	err := db.Model(&Post{}).Where("deleted_at IS NOT NULL").Count(&total).Error
	if err != nil {
		return 0, err
	}
	return total, nil
}

// CountCommentsByStatus 按状态统计评论数（看板用）。
func CountCommentsByStatus(db *gorm.DB) ([]StatsStatusCount, error) {
	rows := make([]StatsStatusCount, 0, 4)
	err := db.Model(&Comment{}).
		Select("status, COUNT(*) AS total").
		Group("status").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	return rows, nil
}

// CountMediaByStatus 按状态统计媒体数（看板用）。
func CountMediaByStatus(db *gorm.DB) ([]StatsStatusCount, error) {
	rows := make([]StatsStatusCount, 0, 3)
	err := db.Model(&Media{}).
		Select("status, COUNT(*) AS total").
		Group("status").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	return rows, nil
}

// ListTopViewedPosts 返回阅读数最高的已发布文章（看板 Top 榜）。
func ListTopViewedPosts(db *gorm.DB, limit int) ([]TopViewedPost, error) {
	rows := make([]TopViewedPost, 0, limit)
	err := db.Model(&Post{}).
		Select("id, slug, title, view_count, like_count").
		Where("status = ? AND deleted_at IS NULL", PostStatusPublished).
		Order("view_count DESC, id ASC").
		Limit(limit).
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	return rows, nil
}

// IncrementPostViewCount 给 posts.view_count 加一（UpdateColumn：不动 updated_at）。
func IncrementPostViewCount(db *gorm.DB, postID int64) error {
	return db.Model(&Post{}).Where("id = ?", postID).
		UpdateColumn("view_count", gorm.Expr("view_count + 1")).Error
}
