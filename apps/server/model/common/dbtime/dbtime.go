// Package dbtime 定义落库用的 Unix 秒时间类型。
//
// 契约（agent.md §7）：内存里用 time.Time，落库前统一转 Unix 秒；展示层再转站点时区。
// 需要区分"未设置"的列（如 posts.published_at、preview_tokens.used_at）请使用 *Unix。
package dbtime

import (
	"database/sql/driver"
	"fmt"
	"strconv"
	"time"
)

// Unix 是以秒为单位的 UTC 时间戳；零值 0 表示 Unix 纪元而不是"未设置"。
type Unix int64

// Now 返回当前时刻（秒）。
func Now() Unix { return Unix(time.Now().Unix()) }

// FromTime 把 time.Time 截断到秒。
func FromTime(t time.Time) Unix { return Unix(t.Unix()) }

// FromUnix 由秒级时间戳构造。
func FromUnix(v int64) Unix { return Unix(v) }

// Time 转回 time.Time（UTC）。
func (u Unix) Time() time.Time { return time.Unix(int64(u), 0).UTC() }

// IsZero 报告是否为纪元 0。
func (u Unix) IsZero() bool { return u == 0 }

// Add 返回偏移后的时间。
func (u Unix) Add(d time.Duration) Unix { return Unix(int64(u) + int64(d/time.Second)) }

// Before 报告 u 是否早于 other。
func (u Unix) Before(other Unix) bool { return u < other }

// After 报告 u 是否晚于 other。
func (u Unix) After(other Unix) bool { return u > other }

// Sub 返回 u - other 的时长。
func (u Unix) Sub(other Unix) time.Duration {
	return time.Duration(int64(u)-int64(other)) * time.Second
}

// GormDataType 让 gorm 把该类型映射为 INTEGER。
func (Unix) GormDataType() string { return "integer" }

// Value 实现 driver.Valuer。
func (u Unix) Value() (driver.Value, error) { return int64(u), nil }

// Scan 实现 sql.Scanner。
func (u *Unix) Scan(src any) error {
	switch v := src.(type) {
	case nil:
		*u = 0
	case int64:
		*u = Unix(v)
	case int:
		*u = Unix(v)
	case float64:
		*u = Unix(int64(v))
	case []byte:
		n, err := strconv.ParseInt(string(v), 10, 64)
		if err != nil {
			return fmt.Errorf("解析 Unix 秒失败: %w", err)
		}
		*u = Unix(n)
	case string:
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return fmt.Errorf("解析 Unix 秒失败: %w", err)
		}
		*u = Unix(n)
	default:
		return fmt.Errorf("dbtime.Unix 不支持的类型 %T", src)
	}
	return nil
}

// MarshalJSON 输出整数秒（agent.md §5：时间出参全为 Unix 秒整数）。
func (u Unix) MarshalJSON() ([]byte, error) {
	return []byte(strconv.FormatInt(int64(u), 10)), nil
}

// UnmarshalJSON 接受整数秒或 null。
func (u *Unix) UnmarshalJSON(b []byte) error {
	if string(b) == "null" {
		*u = 0
		return nil
	}
	n, err := strconv.ParseInt(string(b), 10, 64)
	if err != nil {
		return fmt.Errorf("dbtime.Unix 只接受整数秒: %w", err)
	}
	*u = Unix(n)
	return nil
}
