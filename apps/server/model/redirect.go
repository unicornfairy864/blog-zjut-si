package model

import (
	"gorm.io/gorm"

	"github.com/unicornfairy864/blog-zjut-si/apps/server/model/common/dbtime"
)

// Redirect 对应 redirects 表；已发布文章改 slug 时必须同时写入（§6 / 2004）。
type Redirect struct {
	ID        int64       `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	FromPath  string      `gorm:"column:from_path" json:"fromPath"`
	ToPath    string      `gorm:"column:to_path" json:"toPath"`
	Status    int         `gorm:"column:status" json:"status"`
	Hits      int         `gorm:"column:hits" json:"hits"`
	CreatedAt dbtime.Unix `gorm:"column:created_at" json:"createdAt"`
}

// TableName 固定表名。
func (Redirect) TableName() string { return "redirects" }

// EnsureRedirect 写入 from→to 的跳转；from_path 已存在时保持原记录不变。
func EnsureRedirect(db *gorm.DB, from, to string, status int, at dbtime.Unix) error {
	var n int64
	if err := db.Model(&Redirect{}).Where("from_path = ?", from).Count(&n).Error; err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	return db.Create(&Redirect{
		FromPath:  from,
		ToPath:    to,
		Status:    status,
		CreatedAt: at,
	}).Error
}
