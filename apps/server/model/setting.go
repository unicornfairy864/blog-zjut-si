package model

import (
	"github.com/unicornfairy864/blog-zjut-si/apps/server/model/common/dbtime"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// settings 里必须有键（agent.md §4）。
const (
	SettingContentVersion = "content_version"
	SettingImgBaseURL     = "img_base_url"
	SettingSiteTimezone   = "site_timezone"
	SettingVisitorSalt    = "visitor_salt"
)

// Setting 对应 settings 表：key 为主键，value_json 是合法 JSON 字符串。
type Setting struct {
	Key       string      `gorm:"column:key;primaryKey" json:"key"`
	ValueJSON string      `gorm:"column:value_json" json:"value"`
	UpdatedAt dbtime.Unix `gorm:"column:updated_at" json:"updatedAt"`
}

// TableName 固定表名。
func (Setting) TableName() string { return "settings" }

// GetSetting 读取一条设置。
func GetSetting(db *gorm.DB, key string) (*Setting, error) {
	var s Setting
	if err := db.Where("key = ?", key).Take(&s).Error; err != nil {
		return nil, wrapErr(err)
	}
	return &s, nil
}

// PutSetting 写入或覆盖一条设置。
func PutSetting(db *gorm.DB, key, valueJSON string, at dbtime.Unix) error {
	return db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "key"}},
		DoUpdates: clause.Assignments(map[string]any{"value_json": valueJSON, "updated_at": at}),
	}).Create(&Setting{Key: key, ValueJSON: valueJSON, UpdatedAt: at}).Error
}
