package model

import (
	"github.com/unicornfairy864/blog-zjut-si/apps/server/model/common/dbtime"
	"gorm.io/gorm"
)

// Admin 对应 admins 表；C7 规定全站只有一条记录。
type Admin struct {
	ID           int64       `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	Username     string      `gorm:"column:username" json:"username"`
	PasswordHash string      `gorm:"column:password_hash" json:"-"`
	Email        string      `gorm:"column:email" json:"email"`
	LastLoginAt  dbtime.Unix `gorm:"column:last_login_at" json:"lastLoginAt"`
	CreatedAt    dbtime.Unix `gorm:"column:created_at" json:"createdAt"`
	UpdatedAt    dbtime.Unix `gorm:"column:updated_at" json:"updatedAt"`
}

// TableName 固定表名，避免 gorm 复数化。
func (Admin) TableName() string { return "admins" }

// FindAdminByUsername 按用户名查询管理员。
func FindAdminByUsername(db *gorm.DB, username string) (*Admin, error) {
	var admin Admin
	if err := db.Where("username = ?", username).Take(&admin).Error; err != nil {
		return nil, wrapErr(err)
	}
	return &admin, nil
}

// FindAdminByID 按主键查询管理员。
func FindAdminByID(db *gorm.DB, id int64) (*Admin, error) {
	var admin Admin
	if err := db.Where("id = ?", id).Take(&admin).Error; err != nil {
		return nil, wrapErr(err)
	}
	return &admin, nil
}

// CountAdmins 统计管理员条数（引导流程用它做幂等判断）。
func CountAdmins(db *gorm.DB) (int64, error) {
	var n int64
	err := db.Model(&Admin{}).Count(&n).Error
	return n, err
}

// CreateAdmin 插入管理员。
func CreateAdmin(db *gorm.DB, admin *Admin) error {
	return db.Create(admin).Error
}

// UpdateAdminLastLogin 记录最近登录时间。
func UpdateAdminLastLogin(db *gorm.DB, id int64, at dbtime.Unix) error {
	return db.Model(&Admin{}).Where("id = ?", id).
		Updates(map[string]any{"last_login_at": at, "updated_at": at}).Error
}
