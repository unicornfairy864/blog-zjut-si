package public

import (
	"strings"
	"unicode/utf8"

	"github.com/gin-gonic/gin"

	"github.com/unicornfairy864/blog-zjut-si/apps/server/middleware"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/model/common/errs"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/model/common/request"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/model/common/response"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/service"
)

// InteractionApi 处理 8640 的点赞与 PV 打点（§5）。
//
// 访客标识统一走 `middleware.VisitorSeed`（`X-Visitor-Id` 头，缺失按 `ip|ua` 兜底）；
// 公开接口不 Set-Cookie（§17.1）。
type InteractionApi struct{}

const (
	// interactionPathMaxLen 是打点 path 的字符上限（§5）。
	interactionPathMaxLen = 200
	// interactionReferrerMaxLen 是打点 referrer 的字符上限（§5）。
	interactionReferrerMaxLen = 500
)

// viewRequest 是 PV 打点的请求体。
type viewRequest struct {
	Path     string `json:"path"`
	Slug     string `json:"slug"`
	Referrer string `json:"referrer"`
}

// Like 处理 POST /api/posts/{slug}/like：幂等点赞（§5）。
func (InteractionApi) Like(c *gin.Context) {
	state, err := service.ServiceGroupApp.Stats.Like(
		strings.TrimSpace(c.Param("slug")), middleware.VisitorSeed(c))
	if err != nil {
		response.FailWithError(c, err)
		return
	}
	response.OkWithData(c, state)
}

// Unlike 处理 DELETE /api/posts/{slug}/like：幂等取消点赞（§5）。
func (InteractionApi) Unlike(c *gin.Context) {
	state, err := service.ServiceGroupApp.Stats.Unlike(
		strings.TrimSpace(c.Param("slug")), middleware.VisitorSeed(c))
	if err != nil {
		response.FailWithError(c, err)
		return
	}
	response.OkWithData(c, state)
}

// Stats 处理 GET /api/posts/{slug}/stats：访客态阅读数与点赞数。
//
// 计数不进 SSR HTML（§17.1），所以公开页由客户端异步取；响应是 no-store。
func (InteractionApi) Stats(c *gin.Context) {
	stats, err := service.ServiceGroupApp.Stats.PostStats(
		strings.TrimSpace(c.Param("slug")), middleware.VisitorSeed(c))
	if err != nil {
		response.FailWithError(c, err)
		return
	}
	response.OkWithNoStore(c, stats)
}

// View 处理 POST /api/events/view：PV 明细与计数（同 visitor 10 分钟去重，§5）。
func (InteractionApi) View(c *gin.Context) {
	var req viewRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.FailWithDetailed(c, errs.ErrorValidation, request.ValidationDetails(err), "参数校验失败")
		return
	}
	if fields := validateView(req); len(fields) > 0 {
		response.FailWithDetailed(c, errs.ErrorValidation, fields, "参数校验失败")
		return
	}

	result, err := service.ServiceGroupApp.Stats.RecordView(
		service.ViewInput{
			Path:     strings.TrimSpace(req.Path),
			Slug:     strings.TrimSpace(req.Slug),
			Referrer: strings.TrimSpace(req.Referrer),
		},
		middleware.VisitorSeed(c),
	)
	if err != nil {
		response.FailWithError(c, err)
		return
	}
	response.OkWithData(c, result)
}

// validateView 按 §5 校验打点字段。
func validateView(req viewRequest) []request.FieldError {
	fields := make([]request.FieldError, 0, 3)

	path := strings.TrimSpace(req.Path)
	switch {
	case path == "":
		fields = append(fields, request.FieldError{Field: "path", Message: "path 不能为空"})
	case !strings.HasPrefix(path, "/"):
		fields = append(fields, request.FieldError{Field: "path", Message: "path 必须以 / 开头"})
	case utf8.RuneCountInString(path) > interactionPathMaxLen:
		fields = append(fields, request.FieldError{Field: "path", Message: "path 不能超过 200 个字符"})
	}

	if utf8.RuneCountInString(strings.TrimSpace(req.Slug)) > 120 {
		fields = append(fields, request.FieldError{Field: "slug", Message: "slug 不能超过 120 个字符"})
	}
	if utf8.RuneCountInString(strings.TrimSpace(req.Referrer)) > interactionReferrerMaxLen {
		fields = append(fields, request.FieldError{Field: "referrer", Message: "referrer 不能超过 500 个字符"})
	}
	return fields
}
