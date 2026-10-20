package admin

import (
	"github.com/gin-gonic/gin"

	"github.com/unicornfairy864/blog-zjut-si/apps/server/model/common/errs"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/model/common/request"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/model/common/response"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/service"
)

// ContentApi 处理 /api/admin/{tags|series|links|redirects}：
// 绑定 → 调 service（业务与字段校验在 service）→ 输出（§3.2）。
type ContentApi struct{}

// taxonomyRequest 是标签 / 系列的请求体；指针字段区分"没传"与"传空"（PUT 语义）。
type taxonomyRequest struct {
	Name        *string `json:"name"`
	Slug        *string `json:"slug"`
	Description *string `json:"description"`
}

// linkRequest 是友链请求体；字段名与 api.md 一致。
type linkRequest struct {
	Name        *string `json:"name"`
	URL         *string `json:"url"`
	Avatar      *string `json:"avatar"`
	Description *string `json:"description"`
	Sort        *int    `json:"sort"`
	Status      *string `json:"status"`
}

// redirectRequest 是跳转请求体。
type redirectRequest struct {
	FromPath *string `json:"fromPath"`
	ToPath   *string `json:"toPath"`
	Status   *int    `json:"status"`
}

// Tags 处理 GET /api/admin/tags：全量列表，带未删除文章数。
func (ContentApi) Tags(c *gin.Context) {
	items, err := service.ServiceGroupApp.Content.Tags()
	if err != nil {
		response.FailWithError(c, err)
		return
	}
	response.OkWithData(c, gin.H{"list": items})
}

// CreateTag 处理 POST /api/admin/tags。
func (ContentApi) CreateTag(c *gin.Context) {
	in, ok := bindTaxonomy(c)
	if !ok {
		return
	}
	tag, err := service.ServiceGroupApp.Content.CreateTag(in, actorFrom(c))
	if err != nil {
		response.FailWithError(c, err)
		return
	}
	response.OkWithData(c, tag)
}

// UpdateTag 处理 PUT /api/admin/tags/{id}。
func (ContentApi) UpdateTag(c *gin.Context) {
	id, ok := adminPathID(c, "id")
	if !ok {
		return
	}
	in, ok := bindTaxonomy(c)
	if !ok {
		return
	}
	tag, err := service.ServiceGroupApp.Content.UpdateTag(id, in, actorFrom(c))
	if err != nil {
		response.FailWithError(c, err)
		return
	}
	response.OkWithData(c, tag)
}

// DeleteTag 处理 DELETE /api/admin/tags/{id}：连带清 post_tags，不删文章。
func (ContentApi) DeleteTag(c *gin.Context) {
	id, ok := adminPathID(c, "id")
	if !ok {
		return
	}
	if err := service.ServiceGroupApp.Content.DeleteTag(id, actorFrom(c)); err != nil {
		response.FailWithError(c, err)
		return
	}
	response.OkWithData(c, gin.H{"deleted": true})
}

// Series 处理 GET /api/admin/series：全量列表，带未删除文章数。
func (ContentApi) Series(c *gin.Context) {
	items, err := service.ServiceGroupApp.Content.Series()
	if err != nil {
		response.FailWithError(c, err)
		return
	}
	response.OkWithData(c, gin.H{"list": items})
}

// CreateSeries 处理 POST /api/admin/series。
func (ContentApi) CreateSeries(c *gin.Context) {
	in, ok := bindTaxonomy(c)
	if !ok {
		return
	}
	series, err := service.ServiceGroupApp.Content.CreateSeries(in, actorFrom(c))
	if err != nil {
		response.FailWithError(c, err)
		return
	}
	response.OkWithData(c, series)
}

// UpdateSeries 处理 PUT /api/admin/series/{id}。
func (ContentApi) UpdateSeries(c *gin.Context) {
	id, ok := adminPathID(c, "id")
	if !ok {
		return
	}
	in, ok := bindTaxonomy(c)
	if !ok {
		return
	}
	series, err := service.ServiceGroupApp.Content.UpdateSeries(id, in, actorFrom(c))
	if err != nil {
		response.FailWithError(c, err)
		return
	}
	response.OkWithData(c, series)
}

// DeleteSeries 处理 DELETE /api/admin/series/{id}：把 posts.series_id 置空，不删文章。
func (ContentApi) DeleteSeries(c *gin.Context) {
	id, ok := adminPathID(c, "id")
	if !ok {
		return
	}
	if err := service.ServiceGroupApp.Content.DeleteSeries(id, actorFrom(c)); err != nil {
		response.FailWithError(c, err)
		return
	}
	response.OkWithData(c, gin.H{"deleted": true})
}

// Links 处理 GET /api/admin/links：全量（含 hidden），按 sort、id 升序。
func (ContentApi) Links(c *gin.Context) {
	items, err := service.ServiceGroupApp.Content.Links()
	if err != nil {
		response.FailWithError(c, err)
		return
	}
	response.OkWithData(c, gin.H{"list": items})
}

// CreateLink 处理 POST /api/admin/links。
func (ContentApi) CreateLink(c *gin.Context) {
	in, ok := bindLink(c)
	if !ok {
		return
	}
	link, err := service.ServiceGroupApp.Content.CreateLink(in, actorFrom(c))
	if err != nil {
		response.FailWithError(c, err)
		return
	}
	response.OkWithData(c, link)
}

// UpdateLink 处理 PUT /api/admin/links/{id}。
func (ContentApi) UpdateLink(c *gin.Context) {
	id, ok := adminPathID(c, "id")
	if !ok {
		return
	}
	in, ok := bindLink(c)
	if !ok {
		return
	}
	link, err := service.ServiceGroupApp.Content.UpdateLink(id, in, actorFrom(c))
	if err != nil {
		response.FailWithError(c, err)
		return
	}
	response.OkWithData(c, link)
}

// DeleteLink 处理 DELETE /api/admin/links/{id}。
func (ContentApi) DeleteLink(c *gin.Context) {
	id, ok := adminPathID(c, "id")
	if !ok {
		return
	}
	if err := service.ServiceGroupApp.Content.DeleteLink(id, actorFrom(c)); err != nil {
		response.FailWithError(c, err)
		return
	}
	response.OkWithData(c, gin.H{"deleted": true})
}

// Redirects 处理 GET /api/admin/redirects：全量，按 id 倒序。
func (ContentApi) Redirects(c *gin.Context) {
	items, err := service.ServiceGroupApp.Content.Redirects()
	if err != nil {
		response.FailWithError(c, err)
		return
	}
	response.OkWithData(c, gin.H{"list": items})
}

// CreateRedirect 处理 POST /api/admin/redirects。
func (ContentApi) CreateRedirect(c *gin.Context) {
	in, ok := bindRedirect(c)
	if !ok {
		return
	}
	item, err := service.ServiceGroupApp.Content.CreateRedirect(in, actorFrom(c))
	if err != nil {
		response.FailWithError(c, err)
		return
	}
	response.OkWithData(c, item)
}

// UpdateRedirect 处理 PUT /api/admin/redirects/{id}。
func (ContentApi) UpdateRedirect(c *gin.Context) {
	id, ok := adminPathID(c, "id")
	if !ok {
		return
	}
	in, ok := bindRedirect(c)
	if !ok {
		return
	}
	item, err := service.ServiceGroupApp.Content.UpdateRedirect(id, in, actorFrom(c))
	if err != nil {
		response.FailWithError(c, err)
		return
	}
	response.OkWithData(c, item)
}

// DeleteRedirect 处理 DELETE /api/admin/redirects/{id}。
func (ContentApi) DeleteRedirect(c *gin.Context) {
	id, ok := adminPathID(c, "id")
	if !ok {
		return
	}
	if err := service.ServiceGroupApp.Content.DeleteRedirect(id, actorFrom(c)); err != nil {
		response.FailWithError(c, err)
		return
	}
	response.OkWithData(c, gin.H{"deleted": true})
}

// bindTaxonomy 绑定标签 / 系列请求体；绑定失败即写出 code 1003（HTTP 200）。
func bindTaxonomy(c *gin.Context) (service.TaxonomyInput, bool) {
	var req taxonomyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.FailWithDetailed(c, errs.ErrorValidation, request.ValidationDetails(err), "参数校验失败")
		return service.TaxonomyInput{}, false
	}
	return service.TaxonomyInput{Name: req.Name, Slug: req.Slug, Description: req.Description}, true
}

// bindLink 绑定友链请求体。
func bindLink(c *gin.Context) (service.LinkInput, bool) {
	var req linkRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.FailWithDetailed(c, errs.ErrorValidation, request.ValidationDetails(err), "参数校验失败")
		return service.LinkInput{}, false
	}
	return service.LinkInput{
		Name:        req.Name,
		URL:         req.URL,
		Avatar:      req.Avatar,
		Description: req.Description,
		Sort:        req.Sort,
		Status:      req.Status,
	}, true
}

// bindRedirect 绑定跳转请求体。
func bindRedirect(c *gin.Context) (service.RedirectInput, bool) {
	var req redirectRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.FailWithDetailed(c, errs.ErrorValidation, request.ValidationDetails(err), "参数校验失败")
		return service.RedirectInput{}, false
	}
	return service.RedirectInput{FromPath: req.FromPath, ToPath: req.ToPath, Status: req.Status}, true
}
