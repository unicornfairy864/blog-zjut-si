package public

import (
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/gin-gonic/gin"

	"github.com/unicornfairy864/blog-zjut-si/apps/server/model/common/errs"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/model/common/request"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/model/common/response"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/service"
)

// CommentApi 处理 8640 的公开评论接口（§5）。
type CommentApi struct{}

const (
	// commentAuthorNameMaxLen 是昵称的字符上限（§5）。
	commentAuthorNameMaxLen = 50
	// commentAuthorEmailMaxLen 是邮箱的字符上限。
	commentAuthorEmailMaxLen = 200
	// commentAuthorURLMaxLen 是个人网址的字符上限。
	commentAuthorURLMaxLen = 200
	// commentContentMaxLen 是评论正文的字符上限。
	commentContentMaxLen = 2000
)

// commentEmailPattern 是"像邮箱"的宽松判断：本地部分 + 带点的域名。
var commentEmailPattern = regexp.MustCompile(`^[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}$`)

// commentCreateRequest 是提交评论的请求体；parentId 为空表示顶层评论。
type commentCreateRequest struct {
	AuthorName  string `json:"authorName"`
	AuthorEmail string `json:"authorEmail"`
	AuthorURL   string `json:"authorUrl"`
	ContentMD   string `json:"contentMd"`
	ParentID    *int64 `json:"parentId"`
}

// List 处理 GET /api/posts/{slug}/comments：只出 approved，嵌套固定两层。
func (CommentApi) List(c *gin.Context) {
	list, err := service.ServiceGroupApp.Comment.PublicList(strings.TrimSpace(c.Param("slug")))
	if err != nil {
		response.FailWithError(c, err)
		return
	}
	response.OkWithETag(c, list)
}

// Create 处理 POST /api/posts/{slug}/comments：新评论一律 pending，等审核（§5 / §9）。
func (CommentApi) Create(c *gin.Context) {
	var req commentCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.FailWithDetailed(c, errs.ErrorValidation, request.ValidationDetails(err), "参数校验失败")
		return
	}
	if fields := validateCommentCreate(req); len(fields) > 0 {
		response.FailWithDetailed(c, errs.ErrorValidation, fields, "参数校验失败")
		return
	}

	result, err := service.ServiceGroupApp.Comment.Create(
		strings.TrimSpace(c.Param("slug")),
		service.CommentCreateInput{
			AuthorName:  req.AuthorName,
			AuthorEmail: req.AuthorEmail,
			AuthorURL:   req.AuthorURL,
			ContentMD:   req.ContentMD,
			ParentID:    req.ParentID,
		},
		c.ClientIP(),
		c.Request.UserAgent(),
	)
	if err != nil {
		response.FailWithError(c, err)
		return
	}
	response.OkWithData(c, gin.H{
		"id":        result.ID,
		"status":    result.Status,
		"createdAt": result.CreatedAt,
	})
}

// validateCommentCreate 按 §5 做字段校验；错误形状与 request.FieldError 一致。
func validateCommentCreate(req commentCreateRequest) []request.FieldError {
	fields := make([]request.FieldError, 0, 4)

	if n := utf8.RuneCountInString(strings.TrimSpace(req.AuthorName)); n == 0 || n > commentAuthorNameMaxLen {
		fields = append(fields, request.FieldError{Field: "authorName", Message: "昵称长度必须在 1–50 个字符之间"})
	}

	email := strings.TrimSpace(req.AuthorEmail)
	switch {
	case email == "":
	case utf8.RuneCountInString(email) > commentAuthorEmailMaxLen:
		fields = append(fields, request.FieldError{Field: "authorEmail", Message: "邮箱长度不能超过 200 个字符"})
	case !commentEmailPattern.MatchString(email):
		fields = append(fields, request.FieldError{Field: "authorEmail", Message: "邮箱格式不正确"})
	}

	authorURL := strings.TrimSpace(req.AuthorURL)
	switch {
	case authorURL == "":
	case utf8.RuneCountInString(authorURL) > commentAuthorURLMaxLen:
		fields = append(fields, request.FieldError{Field: "authorUrl", Message: "网址长度不能超过 200 个字符"})
	case !strings.HasPrefix(authorURL, "http://") && !strings.HasPrefix(authorURL, "https://"):
		fields = append(fields, request.FieldError{Field: "authorUrl", Message: "网址必须以 http:// 或 https:// 开头"})
	}

	if n := utf8.RuneCountInString(strings.TrimSpace(req.ContentMD)); n == 0 || n > commentContentMaxLen {
		fields = append(fields, request.FieldError{Field: "contentMd", Message: "评论内容长度必须在 1–2000 个字符之间"})
	}

	if req.ParentID != nil && *req.ParentID <= 0 {
		fields = append(fields, request.FieldError{Field: "parentId", Message: "必须是正整数"})
	}
	return fields
}
