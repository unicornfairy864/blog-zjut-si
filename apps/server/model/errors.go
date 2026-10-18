// Package model 是唯一出现 gorm 与 SQL 的地方：只做数据访问，不做权限与状态机判断。
package model

import (
	"errors"
	"strings"

	"gorm.io/gorm"
)

// ErrNotFound 表示目标记录不存在；service 用它区分"没查到"与"查询失败"。
var ErrNotFound = errors.New("记录不存在")

// IsUniqueViolation 报告错误是否为唯一索引冲突；service 用它把 slug 撞车翻成 2001。
func IsUniqueViolation(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}

// wrapErr 把 gorm 的 ErrRecordNotFound 归一为 ErrNotFound，避免 gorm 类型泄漏到 service。
func wrapErr(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ErrNotFound
	}
	return err
}
