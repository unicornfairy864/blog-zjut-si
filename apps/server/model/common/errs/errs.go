// Package errs 定义统一业务错误与数字错误码（agent.md §5）。
//
// 业务代码只允许用本包的常量与构造函数，禁止裸数字、禁止把 err.Error() 直接返回前端。
package errs

import "errors"

// 数字错误码；与 §5 的表格一一对应。
const (
	Success             = 0
	Error               = 7
	ErrorAuth           = 1000
	ErrorForbidden      = 1001
	ErrorNotFound       = 1002
	ErrorValidation     = 1003
	ErrorConflict       = 1004
	ErrorRateLimited    = 1005
	ErrorInternal       = 1006
	ErrorGone           = 1007
	ErrorSlugConflict   = 2001
	ErrorMediaInUse     = 2002
	ErrorImageTooLarge  = 2003
	ErrorSlugImmutable  = 2004
	ErrorLoginLocked    = 2005
	ErrorPreviewExpired = 2006
	ErrorFileRejected   = 2007
	ErrorFileTooLarge   = 2008
)

// httpStatusByCode 是 §5 的错误码 → HTTP 状态映射。
//
// v1.14 起业务错误不再用它决定状态码（业务错误恒为 HTTP 200，前端只看 code）；
// 保留此表是因为健康检查、文档与少量非业务场景仍需要真实状态码。
var httpStatusByCode = map[int]int{
	Success:             200,
	Error:               500,
	ErrorAuth:           401,
	ErrorForbidden:      403,
	ErrorNotFound:       404,
	ErrorValidation:     400,
	ErrorConflict:       409,
	ErrorRateLimited:    429,
	ErrorInternal:       500,
	ErrorGone:           410,
	ErrorSlugConflict:   409,
	ErrorMediaInUse:     409,
	ErrorImageTooLarge:  413,
	ErrorSlugImmutable:  409,
	ErrorLoginLocked:    423,
	ErrorPreviewExpired: 410,
	ErrorFileRejected:   415,
	ErrorFileTooLarge:   413,
}

// HTTPStatus 返回 code 对应的 HTTP 状态码；未知 code 归 500。
//
// v1.14 起业务错误不再用它决定状态码：业务失败一律 HTTP 200 + code。
// 保留给健康检查（/readyz 503）、文档与需要真实状态码的非业务场景。
func HTTPStatus(code int) int {
	if status, ok := httpStatusByCode[code]; ok {
		return status
	}
	return 500
}

// AppError 是可以直接映射为业务错误信封的错误。
//
// v1.14 起 HTTPStatus 不再决定响应状态码（业务错误恒为 HTTP 200），
// 该字段仅作参考，保留以免调用方破坏。
type AppError struct {
	Code       int
	HTTPStatus int
	Message    string
	Details    any
}

// Error 实现 error，仅用于日志与包装。
func (e *AppError) Error() string { return e.Message }

// New 构造业务错误；v1.14 起 HTTP 状态恒为 200，HTTPStatus 仅作参考保留。
func New(code int, message string) *AppError {
	return &AppError{Code: code, HTTPStatus: HTTPStatus(code), Message: message}
}

// WithDetails 构造带附加数据的业务错误（如字段错误、引用清单）。
func WithDetails(code int, message string, details any) *AppError {
	return &AppError{Code: code, HTTPStatus: HTTPStatus(code), Message: message, Details: details}
}

// As 从错误链里取出 AppError。
func As(err error) (*AppError, bool) {
	var appErr *AppError
	if errors.As(err, &appErr) {
		return appErr, true
	}
	return nil, false
}

// Translate 把任意错误归一为 AppError；未知错误统一 1006，不暴露内部信息。
func Translate(err error) *AppError {
	if appErr, ok := As(err); ok {
		return appErr
	}
	return New(ErrorInternal, "服务器内部错误")
}
