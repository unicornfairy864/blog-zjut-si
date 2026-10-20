// Package request 提供入参的通用结构体与校验错误转换。
package request

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"

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

// ValidationDetails 把 binding 校验错误转成稳定的字段错误数组。
//
// 除了字段级校验，还要解释"请求体根本没解析成功"这一类问题：JSON 语法错误、
// 字段类型不匹配（比如数字字段传了字符串）、请求体为空。不处理的话前端只能看到
// 一句「参数校验失败」而 data 是 null，无从下手。
func ValidationDetails(err error) any {
	var verr validator.ValidationErrors
	if !errors.As(err, &verr) {
		return bodyErrorDetails(err)
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

// bodyErrorDetails 解释 JSON 解析失败的原因；认识的错误返回单元素字段数组，其余返回 nil。
//
// Field 用 JSON 里的字段名（如 coverMediaId），比 Go 结构体字段名更贴近前端。
func bodyErrorDetails(err error) any {
	var typeErr *json.UnmarshalTypeError
	if errors.As(err, &typeErr) {
		field := typeErr.Field
		if field == "" {
			field = "body"
		}
		return []FieldError{{
			Field:   field,
			Message: "类型不匹配（应为 " + typeErr.Type.String() + "）",
		}}
	}

	var syntaxErr *json.SyntaxError
	if errors.As(err, &syntaxErr) {
		return []FieldError{{
			Field:   "body",
			Message: fmt.Sprintf("JSON 格式错误（第 %d 字节附近），检查引号、逗号与括号", syntaxErr.Offset),
		}}
	}

	switch {
	case errors.Is(err, io.EOF), errors.Is(err, io.ErrUnexpectedEOF):
		return []FieldError{{Field: "body", Message: "请求体为空或不是合法 JSON"}}
	}
	return nil
}
