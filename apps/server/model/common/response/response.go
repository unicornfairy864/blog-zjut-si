// Package response 是唯一的响应出口：gin-vue-admin 风格 {code,data,msg} 信封。
//
// 规则（agent.md §5）：成功 200 + code=0；失败 HTTP 状态由 code 查表得出。
package response

import (
	"github.com/gin-gonic/gin"

	"github.com/unicornfairy864/blog-zjut-si/apps/server/model/common/errs"
)

// Body 是响应信封。
type Body struct {
	Code int    `json:"code"`
	Data any    `json:"data"`
	Msg  string `json:"msg"`
}

// Page 是分页数据的固定形状。
type Page struct {
	List     any   `json:"list"`
	Total    int64 `json:"total"`
	Page     int   `json:"page"`
	PageSize int   `json:"pageSize"`
}

const okMessage = "操作成功"

// Ok 返回空数据的成功响应。
func Ok(c *gin.Context) {
	OkWithDetailed(c, nil, okMessage)
}

// OkWithData 返回带数据的成功响应。
func OkWithData(c *gin.Context, data any) {
	OkWithDetailed(c, data, okMessage)
}

// OkWithDetailed 返回带数据与自定义提示的成功响应。
func OkWithDetailed(c *gin.Context, data any, msg string) {
	c.JSON(200, Body{Code: errs.Success, Data: data, Msg: msg})
}

// OkWithPage 返回分页成功响应。
func OkWithPage(c *gin.Context, list any, total int64, page, pageSize int) {
	OkWithData(c, Page{List: list, Total: total, Page: page, PageSize: pageSize})
}

// FailWithDetailed 返回失败响应；HTTP 状态由 code 查表。
func FailWithDetailed(c *gin.Context, code int, data any, msg string) {
	if msg == "" {
		msg = "请求失败"
	}
	c.AbortWithStatusJSON(errs.HTTPStatus(code), Body{Code: code, Data: data, Msg: msg})
}

// FailWithError 归一化错误并输出；非 AppError 一律 500/1006。
func FailWithError(c *gin.Context, err error) {
	appErr := errs.Translate(err)
	FailWithDetailed(c, appErr.Code, appErr.Details, appErr.Message)
}
