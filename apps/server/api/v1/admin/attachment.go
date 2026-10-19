package admin

import (
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/unicornfairy864/blog-zjut-si/apps/server/model/common/errs"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/model/common/request"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/model/common/response"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/service"
)

// AttachmentApi 处理 /api/admin/attachments*：绑定 → 调 service → 输出（§3.2）。
type AttachmentApi struct{}

// attachmentRequest 是附件请求体；指针字段区分"没传"与"传空"（PUT 语义）。
type attachmentRequest struct {
	PostID   *int64  `json:"postId"`
	Title    *string `json:"title"`
	ShareURL *string `json:"shareUrl"`
	FileName *string `json:"fileName"`
	Bytes    *int64  `json:"bytes"`
	SHA256   *string `json:"sha256"`
	Version  *string `json:"version"`
	Sort     *int    `json:"sort"`
}

// List 处理 GET /api/admin/attachments?postId=&page=&pageSize=：按 sort、id 升序分页。
func (AttachmentApi) List(c *gin.Context) {
	var page request.PageInfo
	if err := c.ShouldBindQuery(&page); err != nil {
		response.FailWithDetailed(c, errs.ErrorValidation, request.ValidationDetails(err), "参数校验失败")
		return
	}
	page.Normalize()

	postID := int64(0)
	if raw := strings.TrimSpace(c.Query("postId")); raw != "" {
		parsed, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || parsed <= 0 {
			response.FailWithDetailed(c, errs.ErrorValidation,
				[]request.FieldError{{Field: "postId", Message: "必须是正整数"}}, "参数校验失败")
			return
		}
		postID = parsed
	}

	items, total, err := service.ServiceGroupApp.Content.Attachments(postID, page.Page, page.PageSize)
	if err != nil {
		response.FailWithError(c, err)
		return
	}
	response.OkWithPage(c, items, total, page.Page, page.PageSize)
}

// Create 处理 POST /api/admin/attachments。
func (AttachmentApi) Create(c *gin.Context) {
	in, ok := bindAttachment(c)
	if !ok {
		return
	}
	item, err := service.ServiceGroupApp.Content.CreateAttachment(in, actorFrom(c))
	if err != nil {
		response.FailWithError(c, err)
		return
	}
	response.OkWithData(c, item)
}

// Update 处理 PUT /api/admin/attachments/{id}。
func (AttachmentApi) Update(c *gin.Context) {
	id, ok := adminPathID(c, "id")
	if !ok {
		return
	}
	in, ok := bindAttachment(c)
	if !ok {
		return
	}
	item, err := service.ServiceGroupApp.Content.UpdateAttachment(id, in, actorFrom(c))
	if err != nil {
		response.FailWithError(c, err)
		return
	}
	response.OkWithData(c, item)
}

// Delete 处理 DELETE /api/admin/attachments/{id}。
func (AttachmentApi) Delete(c *gin.Context) {
	id, ok := adminPathID(c, "id")
	if !ok {
		return
	}
	if err := service.ServiceGroupApp.Content.DeleteAttachment(id, actorFrom(c)); err != nil {
		response.FailWithError(c, err)
		return
	}
	response.OkWithData(c, gin.H{"deleted": true})
}

// bindAttachment 绑定附件请求体；绑定失败即写出 400/1003。
func bindAttachment(c *gin.Context) (service.AttachmentInput, bool) {
	var req attachmentRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.FailWithDetailed(c, errs.ErrorValidation, request.ValidationDetails(err), "参数校验失败")
		return service.AttachmentInput{}, false
	}
	return service.AttachmentInput{
		PostID:   req.PostID,
		Title:    req.Title,
		ShareURL: req.ShareURL,
		FileName: req.FileName,
		Bytes:    req.Bytes,
		SHA256:   req.SHA256,
		Version:  req.Version,
		Sort:     req.Sort,
	}, true
}
