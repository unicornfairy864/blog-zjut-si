// Package response 是唯一的响应出口：gin-vue-admin 风格 {code,data,msg} 信封。
//
// 规则（agent.md §5，v1.14 起）：成功 200 + code=0；**业务错误一律 HTTP 200**，
// 成功/失败只由信封里的 code 区分。真实 HTTP 状态码只保留在少数非业务错误场景
// （健康检查 503、静态直出与文件流、301/302 跳转、ETag 304、NoRoute 404），
// 详见 agent.md §5 的例外清单。
package response

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"

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

// OkWithETag 输出带强 ETag 的公开读响应（agent.md §5 / §12）。
//
// ETag 取响应体哈希；命中 If-None-Match 时返回 304，不重复传输正文。
func OkWithETag(c *gin.Context, data any) {
	body, err := json.Marshal(Body{Code: errs.Success, Data: data, Msg: okMessage})
	if err != nil {
		FailWithDetailed(c, errs.ErrorInternal, nil, "服务器内部错误")
		return
	}

	sum := sha256.Sum256(body)
	etag := `"` + hex.EncodeToString(sum[:16]) + `"`

	c.Header("Cache-Control", "no-cache")
	c.Header("ETag", etag)

	if matchETag(c.GetHeader("If-None-Match"), etag) {
		c.Status(http.StatusNotModified)
		return
	}
	c.Data(http.StatusOK, "application/json; charset=utf-8", body)
}

// OkWithNoStore 输出禁止任何中间层缓存的成功响应（agent.md §5）。
//
// 用于依赖访客态的公开读接口（如 `GET /posts/{slug}/stats` 的 `liked`）：
// 这类响应不能参与 ETag 协商，否则会把甲的点赞状态发给乙。
func OkWithNoStore(c *gin.Context, data any) {
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, Body{Code: errs.Success, Data: data, Msg: okMessage})
}

// matchETag 判断 If-None-Match 是否命中（支持逗号分隔列表与 *）。
func matchETag(header, etag string) bool {
	header = strings.TrimSpace(header)
	if header == "" {
		return false
	}
	if header == "*" {
		return true
	}
	for _, candidate := range strings.Split(header, ",") {
		if strings.TrimSpace(candidate) == etag {
			return true
		}
	}
	return false
}

// FailWithDetailed 返回失败响应。
//
// 业务错误一律 HTTP 200：前端只看信封里的 code，不解析 HTTP 状态。
// 失败仍然 Abort，后续 handler 不再执行。
func FailWithDetailed(c *gin.Context, code int, data any, msg string) {
	if msg == "" {
		msg = "请求失败"
	}
	c.AbortWithStatusJSON(http.StatusOK, Body{Code: code, Data: data, Msg: msg})
}

// FailWithHTTPStatus 返回带真实 HTTP 状态码的失败响应。
//
// 只给「路由不存在」（NoRoute 404 + code 1002）这类必须保留真实状态码的场景用：
// 契约规定 /api/* 的业务错误恒为 200，但路由本身不存在不是业务错误，
// 验收断言（curl /api/admin/posts 必须 404）依赖真实 404。业务代码请用 FailWithDetailed。
func FailWithHTTPStatus(c *gin.Context, status, code int, data any, msg string) {
	if msg == "" {
		msg = "请求失败"
	}
	c.AbortWithStatusJSON(status, Body{Code: code, Data: data, Msg: msg})
}

// FailWithError 归一化错误并输出；非 AppError 一律 code 1006（HTTP 200）。
func FailWithError(c *gin.Context, err error) {
	appErr := errs.Translate(err)
	FailWithDetailed(c, appErr.Code, appErr.Details, appErr.Message)
}
