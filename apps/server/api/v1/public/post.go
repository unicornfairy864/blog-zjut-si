package public

import (
	"strings"
	"unicode/utf8"

	"github.com/gin-gonic/gin"

	"github.com/unicornfairy864/blog-zjut-si/apps/server/model"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/model/common/errs"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/model/common/request"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/model/common/response"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/service"
)

// PostApi 处理 8640 的公开文章与搜索接口。
type PostApi struct{}

// List 处理 GET /api/posts：只返回已发布文章，置顶优先。
func (PostApi) List(c *gin.Context) {
	var page request.PageInfo
	if err := c.ShouldBindQuery(&page); err != nil {
		response.FailWithDetailed(c, errs.ErrorValidation, request.ValidationDetails(err), "参数校验失败")
		return
	}
	page.Normalize()

	posts, total, err := service.ServiceGroupApp.Post.PublicList(page.Page, page.PageSize)
	if err != nil {
		response.FailWithError(c, err)
		return
	}

	items := make([]model.PublicPostListItem, 0, len(posts))
	for _, post := range posts {
		items = append(items, post.PublicListItem())
	}
	response.OkWithETag(c, response.Page{
		List:     items,
		Total:    total,
		Page:     page.Page,
		PageSize: page.PageSize,
	})
}

// Detail 处理 GET /api/posts/{slug}：归档/已删除 410，草稿 404。
func (PostApi) Detail(c *gin.Context) {
	post, err := service.ServiceGroupApp.Post.PublicBySlug(strings.TrimSpace(c.Param("slug")))
	if err != nil {
		response.FailWithError(c, err)
		return
	}
	response.OkWithETag(c, post.PublicDetail())
}

// Search 处理 GET /api/search?q=：≥3 字走 FTS5，1–2 字走 LIKE 兜底。
func (PostApi) Search(c *gin.Context) {
	keyword := strings.TrimSpace(c.Query("q"))
	if count := utf8.RuneCountInString(keyword); count == 0 || count > 64 {
		response.FailWithDetailed(c, errs.ErrorValidation,
			[]request.FieldError{{Field: "q", Message: "关键词长度必须在 1–64 个字符之间"}},
			"参数校验失败")
		return
	}

	var page request.PageInfo
	if err := c.ShouldBindQuery(&page); err != nil {
		response.FailWithDetailed(c, errs.ErrorValidation, request.ValidationDetails(err), "参数校验失败")
		return
	}
	page.Normalize()

	posts, total, err := service.ServiceGroupApp.Post.Search(keyword, page.Page, page.PageSize)
	if err != nil {
		response.FailWithError(c, err)
		return
	}

	items := make([]model.PublicPostListItem, 0, len(posts))
	for _, post := range posts {
		items = append(items, post.PublicListItem())
	}
	response.OkWithETag(c, response.Page{
		List:     items,
		Total:    total,
		Page:     page.Page,
		PageSize: page.PageSize,
	})
}
