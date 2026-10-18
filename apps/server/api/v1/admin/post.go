package admin

import (
	"errors"
	"io"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/unicornfairy864/blog-zjut-si/apps/server/middleware"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/model"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/model/common/errs"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/model/common/request"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/model/common/response"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/service"
)

// PostApi 处理 /api/admin/posts/*：绑定 → 校验 → 调 service → 输出（§3.2）。
type PostApi struct{}

// postCreateRequest 是新建文章入参；新建一律落草稿，发布走 /publish。
type postCreateRequest struct {
	Slug           string `json:"slug" binding:"omitempty,max=120"`
	Title          string `json:"title" binding:"required,min=1,max=200"`
	Excerpt        string `json:"excerpt" binding:"max=300"`
	ContentMD      string `json:"contentMd" binding:"max=2000000"`
	CoverMediaID   *int64 `json:"coverMediaId"`
	OGMediaID      *int64 `json:"ogMediaId"`
	IsPinned       bool   `json:"isPinned"`
	AllowComments  *bool  `json:"allowComments"`
	SeriesID       *int64 `json:"seriesId"`
	SeriesOrder    int    `json:"seriesOrder" binding:"gte=0"`
	SEOTitle       string `json:"seoTitle" binding:"max=200"`
	SEODescription string `json:"seoDescription" binding:"max=500"`
	CanonicalURL   string `json:"canonicalUrl" binding:"omitempty,url,max=500"`
	Lang           string `json:"lang" binding:"omitempty,max=20"`
}

// postUpdateRequest 是更新入参；nil 字段表示不修改。
type postUpdateRequest struct {
	Slug           *string `json:"slug" binding:"omitempty,max=120"`
	Title          *string `json:"title" binding:"omitempty,min=1,max=200"`
	Excerpt        *string `json:"excerpt" binding:"omitempty,max=300"`
	ContentMD      *string `json:"contentMd" binding:"max=2000000"`
	CoverMediaID   *int64  `json:"coverMediaId"`
	OGMediaID      *int64  `json:"ogMediaId"`
	IsPinned       *bool   `json:"isPinned"`
	AllowComments  *bool   `json:"allowComments"`
	SeriesID       *int64  `json:"seriesId"`
	SeriesOrder    *int    `json:"seriesOrder" binding:"omitempty,gte=0"`
	SEOTitle       *string `json:"seoTitle" binding:"omitempty,max=200"`
	SEODescription *string `json:"seoDescription" binding:"omitempty,max=500"`
	CanonicalURL   *string `json:"canonicalUrl" binding:"omitempty,url,max=500"`
	Lang           *string `json:"lang" binding:"omitempty,max=20"`
	Status         *string `json:"status"`
	CreateRedirect bool    `json:"createRedirect"`
}

// postPublishRequest 是发布入参；note 写入 post_revisions.note。
type postPublishRequest struct {
	Note string `json:"note" binding:"max=200"`
}

// postPreviewRequest 是编辑器预览入参；不落库。
type postPreviewRequest struct {
	ContentMD string `json:"contentMd" binding:"max=2000000"`
	Excerpt   string `json:"excerpt" binding:"max=300"`
}

// List 处理 GET /api/admin/posts。
func (PostApi) List(c *gin.Context) {
	var page request.PageInfo
	if err := c.ShouldBindQuery(&page); err != nil {
		response.FailWithDetailed(c, errs.ErrorValidation, request.ValidationDetails(err), "参数校验失败")
		return
	}
	page.Normalize()

	posts, total, err := service.ServiceGroupApp.Post.List(c.Query("status"), page.Keyword, page.Page, page.PageSize)
	if err != nil {
		response.FailWithError(c, err)
		return
	}

	items := make([]model.PostListItem, 0, len(posts))
	for _, post := range posts {
		items = append(items, post.ListItem())
	}
	response.OkWithPage(c, items, total, page.Page, page.PageSize)
}

// Preview 处理 POST /api/admin/posts/preview：渲染未保存正文，结果与发布一致。
func (PostApi) Preview(c *gin.Context) {
	var req postPreviewRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.FailWithDetailed(c, errs.ErrorValidation, request.ValidationDetails(err), "参数校验失败")
		return
	}

	result := service.ServiceGroupApp.Post.Preview(req.ContentMD, req.Excerpt)
	response.OkWithData(c, gin.H{
		"html":        result.HTML,
		"text":        result.Text,
		"wordCount":   result.WordCount,
		"readingTime": result.ReadingTime,
		"excerpt":     result.Excerpt,
	})
}

// Create 处理 POST /api/admin/posts。
func (PostApi) Create(c *gin.Context) {
	var req postCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.FailWithDetailed(c, errs.ErrorValidation, request.ValidationDetails(err), "参数校验失败")
		return
	}

	allowComments := true
	if req.AllowComments != nil {
		allowComments = *req.AllowComments
	}

	post, err := service.ServiceGroupApp.Post.Create(service.PostCreateInput{
		Slug:           req.Slug,
		Title:          req.Title,
		Excerpt:        req.Excerpt,
		ContentMD:      req.ContentMD,
		CoverMediaID:   req.CoverMediaID,
		OGMediaID:      req.OGMediaID,
		IsPinned:       req.IsPinned,
		AllowComments:  allowComments,
		SeriesID:       req.SeriesID,
		SeriesOrder:    req.SeriesOrder,
		SEOTitle:       req.SEOTitle,
		SEODescription: req.SEODescription,
		CanonicalURL:   req.CanonicalURL,
		Lang:           req.Lang,
	}, actorFrom(c))
	if err != nil {
		response.FailWithError(c, err)
		return
	}
	response.OkWithDetailed(c, post, "草稿已创建")
}

// Detail 处理 GET /api/admin/posts/{id}。
func (PostApi) Detail(c *gin.Context) {
	id, ok := postID(c)
	if !ok {
		return
	}
	post, err := service.ServiceGroupApp.Post.Get(id)
	if err != nil {
		response.FailWithError(c, err)
		return
	}
	response.OkWithData(c, post)
}

// Update 处理 PATCH /api/admin/posts/{id}：nil 字段保持不变。
func (PostApi) Update(c *gin.Context) {
	PostApi{}.update(c, false)
}

// Replace 处理 PUT /api/admin/posts/{id}：必须带标题与正文，其余字段可选。
func (PostApi) Replace(c *gin.Context) {
	PostApi{}.update(c, true)
}

// update 是 PUT 与 PATCH 的共用实现；full 为 true 时要求标题与正文必填。
func (PostApi) update(c *gin.Context, full bool) {
	id, ok := postID(c)
	if !ok {
		return
	}

	var req postUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.FailWithDetailed(c, errs.ErrorValidation, request.ValidationDetails(err), "参数校验失败")
		return
	}
	if full {
		fields := make([]request.FieldError, 0, 2)
		if req.Title == nil {
			fields = append(fields, request.FieldError{Field: "title", Message: "必须提供标题"})
		}
		if req.ContentMD == nil {
			fields = append(fields, request.FieldError{Field: "contentMd", Message: "必须提供正文"})
		}
		if len(fields) > 0 {
			response.FailWithDetailed(c, errs.ErrorValidation, fields, "参数校验失败")
			return
		}
	}

	post, err := service.ServiceGroupApp.Post.Update(id, service.PostUpdateInput{
		Slug:           req.Slug,
		Title:          req.Title,
		Excerpt:        req.Excerpt,
		ContentMD:      req.ContentMD,
		CoverMediaID:   req.CoverMediaID,
		OGMediaID:      req.OGMediaID,
		IsPinned:       req.IsPinned,
		AllowComments:  req.AllowComments,
		SeriesID:       req.SeriesID,
		SeriesOrder:    req.SeriesOrder,
		SEOTitle:       req.SEOTitle,
		SEODescription: req.SEODescription,
		CanonicalURL:   req.CanonicalURL,
		Lang:           req.Lang,
		Status:         req.Status,
		CreateRedirect: req.CreateRedirect,
	}, actorFrom(c))
	if err != nil {
		response.FailWithError(c, err)
		return
	}
	response.OkWithDetailed(c, post, "已保存")
}

// Delete 处理 DELETE /api/admin/posts/{id}：软删，30 天后物理删除。
func (PostApi) Delete(c *gin.Context) {
	id, ok := postID(c)
	if !ok {
		return
	}
	if err := service.ServiceGroupApp.Post.Delete(id, actorFrom(c)); err != nil {
		response.FailWithError(c, err)
		return
	}
	response.OkWithDetailed(c, nil, "已移入回收站")
}

// Publish 处理 POST /api/admin/posts/{id}/publish。
func (PostApi) Publish(c *gin.Context) {
	id, ok := postID(c)
	if !ok {
		return
	}

	var req postPublishRequest
	if err := c.ShouldBindJSON(&req); err != nil && !errors.Is(err, io.EOF) {
		response.FailWithDetailed(c, errs.ErrorValidation, request.ValidationDetails(err), "参数校验失败")
		return
	}

	post, err := service.ServiceGroupApp.Post.Publish(id, strings.TrimSpace(req.Note), actorFrom(c))
	if err != nil {
		response.FailWithError(c, err)
		return
	}
	response.OkWithDetailed(c, post, "文章已发布")
}

// Unpublish 处理 POST /api/admin/posts/{id}/unpublish。
func (PostApi) Unpublish(c *gin.Context) {
	id, ok := postID(c)
	if !ok {
		return
	}
	post, err := service.ServiceGroupApp.Post.Unpublish(id, actorFrom(c))
	if err != nil {
		response.FailWithError(c, err)
		return
	}
	response.OkWithDetailed(c, post, "文章已退回草稿")
}

// Revisions 处理 GET /api/admin/posts/{id}/revisions。
func (PostApi) Revisions(c *gin.Context) {
	id, ok := postID(c)
	if !ok {
		return
	}
	revisions, err := service.ServiceGroupApp.Post.Revisions(id)
	if err != nil {
		response.FailWithError(c, err)
		return
	}
	response.OkWithData(c, revisions)
}

// postID 解析路径里的文章主键；非法时直接写出 400。
func postID(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.FailWithDetailed(c, errs.ErrorValidation,
			[]request.FieldError{{Field: "id", Message: "必须是正整数"}}, "参数校验失败")
		return 0, false
	}
	return id, true
}

// actorFrom 取审计上下文；Auth 中间件保证会话存在。
func actorFrom(c *gin.Context) service.PostActor {
	sessionCtx, ok := middleware.SessionFrom(c)
	if !ok {
		return service.PostActor{IP: c.ClientIP()}
	}
	return service.PostActor{AdminID: sessionCtx.Admin.ID, IP: c.ClientIP()}
}
