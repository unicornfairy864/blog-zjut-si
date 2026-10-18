// Package request 提供入参的通用结构体与校验错误转换。
package request

import (
	"errors"
	"fmt"

	"github.com/go-playground/validator/v10"
)

const (
	// DefaultPageSize 是分页默认条数。
	DefaultPageSize = 20
	// MaxPageSize 是分页上限（agent.md §5）。
	MaxPageSize = 50
)

// PageInfo 是分页入参：page ≥1，pageSize 1–50，keyword 可选。
type PageInfo struct {
	Page     int    `json:"page" form:"page"`
	PageSize int    `json:"pageSize" form:"pageSize"`
	Keyword  string `json:"keyword" form:"keyword"`
}

// Normalize 把越界的分页参数收敛到契约允许的范围。
func (p *PageInfo) Normalize() {
	if p.Page < 1 {
		p.Page = 1
	}
	if p.PageSize < 1 {
		p.PageSize = DefaultPageSize
	}
	if p.PageSize > MaxPageSize {
		p.PageSize = MaxPageSize
	}
}

// Offset 返回 LIMIT/OFFSET 的偏移量。
func (p PageInfo) Offset() int { return (p.Page - 1) * p.PageSize }

// FieldError 是字段级校验错误，放在响应的 data 中。
type FieldError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

// validatorMessages 把 validator 的 tag 翻成中文提示。
var validatorMessages = map[string]string{
	"required": "不能为空",
	"min":      "长度或数值过小",
	"max":      "长度或数值过大",
	"gte":      "数值过小",
	"lte":      "数值过大",
	"gt":       "数值过小",
	"lt":       "数值过大",
	"email":    "邮箱格式不正确",
	"url":      "链接格式不正确",
	"oneof":    "取值不在允许范围内",
	"len":      "长度不符合要求",
	"alphanum": "只能包含字母和数字",
	"number":   "必须是数字",
}

// ValidationDetails 把 binding 校验错误转成稳定的字段错误数组；非校验错误返回 nil。
func ValidationDetails(err error) any {
	var verr validator.ValidationErrors
	if !errors.As(err, &verr) {
		return nil
	}
	fields := make([]FieldError, 0, len(verr))
	for _, fe := range verr {
		msg, ok := validatorMessages[fe.Tag()]
		if !ok {
			msg = fmt.Sprintf("校验失败（%s）", fe.Tag())
		}
		fields = append(fields, FieldError{Field: fe.Field(), Message: msg})
	}
	return fields
}
