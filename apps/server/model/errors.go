// Package model 是唯一出现 gorm 与 SQL 的地方：只做数据访问，不做权限与状态机判断。
package model

import (
	"errors"

	"gorm.io/gorm"
)

// ErrNotFound 表示目标记录不存在；service 用它区分"没查到"与"查询失败"。
var ErrNotFound = errors.New("记录不存在")

// wrapErr 把 gorm 的 ErrRecordNotFound 归一为 ErrNotFound，避免 gorm 类型泄漏到 service。
func wrapErr(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ErrNotFound
	}
	return err
}
