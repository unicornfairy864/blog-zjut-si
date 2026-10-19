package model

import (
	"fmt"

	"gorm.io/gorm"

	"github.com/unicornfairy864/blog-zjut-si/apps/server/model/common/dbtime"
)

// 管理端分类（标签 / 系列）与友链、跳转的 CRUD（agent.md §5「内容管理口径」）。
//
// 这些表都没有外键约束（000001 未声明 REFERENCES），所以删除要显式清理从属行。

// AdminTagItem 是管理端标签列表项：count 为该标签下**未删除**文章数（含草稿）。
type AdminTagItem struct {
	Tag
	Count int64 `gorm:"column:count" json:"count"`
}

// AdminSeriesItem 是管理端系列列表项：count 口径同上。
type AdminSeriesItem struct {
	Series
	Count int64 `gorm:"column:count" json:"count"`
}

// ListAdminTags 返回全部标签（含 count 为 0 的），按名称升序。
func ListAdminTags(db *gorm.DB) ([]AdminTagItem, error) {
	items := make([]AdminTagItem, 0)
	err := db.Table("tags").
		Select("tags.*, COUNT(posts.id) AS count").
		Joins("LEFT JOIN post_tags ON post_tags.tag_id = tags.id").
		Joins("LEFT JOIN posts ON posts.id = post_tags.post_id AND posts.deleted_at IS NULL").
		Group("tags.id").
		Order("tags.name ASC, tags.id ASC").
		Scan(&items).Error
	if err != nil {
		return nil, err
	}
	return items, nil
}

// ListAdminSeries 返回全部系列（含 count 为 0 的），按名称升序。
func ListAdminSeries(db *gorm.DB) ([]AdminSeriesItem, error) {
	items := make([]AdminSeriesItem, 0)
	err := db.Table("series").
		Select("series.*, COUNT(posts.id) AS count").
		Joins("LEFT JOIN posts ON posts.series_id = series.id AND posts.deleted_at IS NULL").
		Group("series.id").
		Order("series.name ASC, series.id ASC").
		Scan(&items).Error
	if err != nil {
		return nil, err
	}
	return items, nil
}

// FindTagByID 按主键查询标签。
func FindTagByID(db *gorm.DB, id int64) (*Tag, error) {
	var tag Tag
	if err := db.Where("id = ?", id).Take(&tag).Error; err != nil {
		return nil, wrapErr(err)
	}
	return &tag, nil
}

// FindSeriesByID 按主键查询系列。
func FindSeriesByID(db *gorm.DB, id int64) (*Series, error) {
	var series Series
	if err := db.Where("id = ?", id).Take(&series).Error; err != nil {
		return nil, wrapErr(err)
	}
	return &series, nil
}

// CreateTag 插入标签并回填主键。
func CreateTag(db *gorm.DB, tag *Tag) error { return db.Create(tag).Error }

// UpdateTagFields 按字段更新标签。
func UpdateTagFields(db *gorm.DB, id int64, fields map[string]any) error {
	return db.Model(&Tag{}).Where("id = ?", id).Updates(fields).Error
}

// DeleteTag 删除标签并清掉它的文章关联（文章本身不动）。
func DeleteTag(db *gorm.DB, id int64) error {
	return db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("tag_id = ?", id).Delete(&PostTag{}).Error; err != nil {
			return err
		}
		return tx.Where("id = ?", id).Delete(&Tag{}).Error
	})
}

// CreateSeries 插入系列并回填主键。
func CreateSeries(db *gorm.DB, series *Series) error { return db.Create(series).Error }

// UpdateSeriesFields 按字段更新系列。
func UpdateSeriesFields(db *gorm.DB, id int64, fields map[string]any) error {
	return db.Model(&Series{}).Where("id = ?", id).Updates(fields).Error
}

// DeleteSeries 删除系列并把文章的 series_id 置空（文章本身不动）。
func DeleteSeries(db *gorm.DB, id int64) error {
	return db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&Post{}).Where("series_id = ?", id).
			UpdateColumns(map[string]any{"series_id": nil, "series_order": 0}).Error; err != nil {
			return err
		}
		return tx.Where("id = ?", id).Delete(&Series{}).Error
	})
}

// NameExistsIn 报告某张表里 name 是否已被占用（excludeID 用于更新时排除自身）。
func NameExistsIn(db *gorm.DB, table, name string, excludeID int64) (bool, error) {
	query := db.Table(table).Where("name = ?", name)
	if excludeID > 0 {
		query = query.Where("id <> ?", excludeID)
	}
	var n int64
	if err := query.Count(&n).Error; err != nil {
		return false, err
	}
	return n > 0, nil
}

// SlugExistsIn 报告某张表里 slug 是否已被占用（excludeID 用于更新时排除自身）。
func SlugExistsIn(db *gorm.DB, table, slug string, excludeID int64) (bool, error) {
	query := db.Table(table).Where("slug = ?", slug)
	if excludeID > 0 {
		query = query.Where("id <> ?", excludeID)
	}
	var n int64
	if err := query.Count(&n).Error; err != nil {
		return false, err
	}
	return n > 0, nil
}

// ListLinks 返回全部友链（含 hidden），按 sort、id 升序（管理端）。
func ListLinks(db *gorm.DB) ([]Link, error) {
	links := make([]Link, 0)
	if err := db.Order("sort ASC, id ASC").Find(&links).Error; err != nil {
		return nil, err
	}
	return links, nil
}

// FindLinkByID 按主键查询友链。
func FindLinkByID(db *gorm.DB, id int64) (*Link, error) {
	var link Link
	if err := db.Where("id = ?", id).Take(&link).Error; err != nil {
		return nil, wrapErr(err)
	}
	return &link, nil
}

// CreateLink 插入友链并回填主键。
func CreateLink(db *gorm.DB, link *Link) error { return db.Create(link).Error }

// UpdateLinkFields 按字段更新友链。
func UpdateLinkFields(db *gorm.DB, id int64, fields map[string]any) error {
	return db.Model(&Link{}).Where("id = ?", id).Updates(fields).Error
}

// DeleteLink 删除友链。
func DeleteLink(db *gorm.DB, id int64) error {
	return db.Where("id = ?", id).Delete(&Link{}).Error
}

// ListRedirects 返回全部跳转，按 id 倒序（管理端）。
func ListRedirects(db *gorm.DB) ([]Redirect, error) {
	items := make([]Redirect, 0)
	if err := db.Order("id DESC").Find(&items).Error; err != nil {
		return nil, err
	}
	return items, nil
}

// FindRedirectByID 按主键查询跳转。
func FindRedirectByID(db *gorm.DB, id int64) (*Redirect, error) {
	var item Redirect
	if err := db.Where("id = ?", id).Take(&item).Error; err != nil {
		return nil, wrapErr(err)
	}
	return &item, nil
}

// FindRedirectByPath 按来源路径查询跳转；公开边缘每个未知路径都会查一次。
func FindRedirectByPath(db *gorm.DB, fromPath string) (*Redirect, error) {
	var item Redirect
	if err := db.Where("from_path = ?", fromPath).Take(&item).Error; err != nil {
		return nil, wrapErr(err)
	}
	return &item, nil
}

// CreateRedirect 插入跳转并回填主键。
func CreateRedirect(db *gorm.DB, item *Redirect) error { return db.Create(item).Error }

// UpdateRedirectFields 按字段更新跳转。
func UpdateRedirectFields(db *gorm.DB, id int64, fields map[string]any) error {
	return db.Model(&Redirect{}).Where("id = ?", id).Updates(fields).Error
}

// DeleteRedirect 删除跳转。
func DeleteRedirect(db *gorm.DB, id int64) error {
	return db.Where("id = ?", id).Delete(&Redirect{}).Error
}

// IncrementRedirectHits 给命中次数 +1。
func IncrementRedirectHits(db *gorm.DB, id int64) error {
	return db.Model(&Redirect{}).Where("id = ?", id).
		UpdateColumn("hits", gorm.Expr("hits + 1")).Error
}

// AuditLogFilter 是管理端审计列表的筛选条件（§5）。
type AuditLogFilter struct {
	Action     string
	TargetType string
	TargetID   string
	Limit      int
	Offset     int
}

// ListAuditLogs 返回审计分页，按创建时间倒序。
func ListAuditLogs(db *gorm.DB, f AuditLogFilter) ([]AuditLog, int64, error) {
	query := func() *gorm.DB {
		q := db.Model(&AuditLog{})
		if f.Action != "" {
			q = q.Where("action = ?", f.Action)
		}
		if f.TargetType != "" {
			q = q.Where("target_type = ?", f.TargetType)
		}
		if f.TargetID != "" {
			q = q.Where("target_id = ?", f.TargetID)
		}
		return q
	}

	var total int64
	if err := query().Count(&total).Error; err != nil {
		return nil, 0, err
	}

	logs := make([]AuditLog, 0)
	q := query().Order("created_at DESC, id DESC")
	if f.Limit > 0 {
		q = q.Limit(f.Limit)
	}
	if f.Offset > 0 {
		q = q.Offset(f.Offset)
	}
	if err := q.Find(&logs).Error; err != nil {
		return nil, 0, err
	}
	return logs, total, nil
}

// CleanupResult 是一次 cleanup 的统计（写日志与自测断言用）。
type CleanupResult struct {
	Posts        int64 `json:"posts"`
	PageViews    int64 `json:"pageViews"`
	Revisions    int64 `json:"revisions"`
	Sessions     int64 `json:"sessions"`
	PreviewLinks int64 `json:"previewLinks"`
	LoginAttempt int64 `json:"loginAttempts"`
	Originals    int64 `json:"originals"`
}

// PurgeDeletedPosts 物理删除回收站里 deleted_at <= before 的文章及其从属行（§13）。
//
// 000001 没写外键，所以逐表清理；posts 上的 FTS 触发器会同步清掉索引。
func PurgeDeletedPosts(db *gorm.DB, before dbtime.Unix) (int64, error) {
	ids := make([]int64, 0)
	if err := db.Model(&Post{}).
		Where("deleted_at IS NOT NULL AND deleted_at <= ?", int64(before)).
		Pluck("id", &ids).Error; err != nil {
		return 0, err
	}
	if len(ids) == 0 {
		return 0, nil
	}

	payloads := make([]string, 0, len(ids))
	for _, id := range ids {
		payloads = append(payloads, fmt.Sprintf(`{"postId":%d}`, id))
	}

	err := db.Transaction(func(tx *gorm.DB) error {
		statements := []string{
			"DELETE FROM post_tags WHERE post_id IN ?",
			"DELETE FROM comments WHERE post_id IN ?",
			"DELETE FROM likes WHERE post_id IN ?",
			"DELETE FROM post_revisions WHERE post_id IN ?",
			"DELETE FROM preview_tokens WHERE post_id IN ?",
			"DELETE FROM post_attachments WHERE post_id IN ?",
			"DELETE FROM page_views WHERE post_id IN ?",
			"DELETE FROM stats_daily WHERE post_id IN ?",
		}
		for _, statement := range statements {
			if err := tx.Exec(statement, ids).Error; err != nil {
				return err
			}
		}
		// 待执行的定时发布任务按 payload 认领（jobs 表没有 post_id 列）。
		if err := tx.Exec("DELETE FROM jobs WHERE payload IN ?", payloads).Error; err != nil {
			return err
		}
		return tx.Where("id IN ?", ids).Delete(&Post{}).Error
	})
	if err != nil {
		return 0, err
	}
	return int64(len(ids)), nil
}

// DeletePageViewsBefore 删除 before 之前的访问明细（§13：保留 90 天）。
func DeletePageViewsBefore(db *gorm.DB, before dbtime.Unix) (int64, error) {
	result := db.Where("created_at < ?", int64(before)).Delete(&PageView{})
	return result.RowsAffected, result.Error
}

// DeleteLoginAttemptsBefore 清理窗口早已过期、且已解锁的登录计数行。
func DeleteLoginAttemptsBefore(db *gorm.DB, before dbtime.Unix) (int64, error) {
	result := db.Where("window_start < ? AND (locked_until = 0 OR locked_until < ?)", int64(before), int64(before)).
		Delete(&LoginAttempt{})
	return result.RowsAffected, result.Error
}

// ListPostsWithManyRevisions 返回版本条数超过 keep 的文章 id（cleanup 裁剪用）。
func ListPostsWithManyRevisions(db *gorm.DB, keep int) ([]int64, error) {
	ids := make([]int64, 0)
	err := db.Model(&PostRevision{}).
		Select("post_id").
		Group("post_id").
		Having("COUNT(*) > ?", keep).
		Pluck("post_id", &ids).Error
	if err != nil {
		return nil, err
	}
	return ids, nil
}

// ListArchivedOriginalsBefore 返回归档完成时间早于 before 的媒体（本地原图可删）。
func ListArchivedOriginalsBefore(db *gorm.DB, before dbtime.Unix) ([]Media, error) {
	items := make([]Media, 0)
	err := db.Where("orig_archived = 1 AND orig_archived_at IS NOT NULL AND orig_archived_at <= ?", int64(before)).
		Find(&items).Error
	if err != nil {
		return nil, err
	}
	return items, nil
}
