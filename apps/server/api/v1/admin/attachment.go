package admin

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/unicornfairy864/blog-zjut-si/apps/server/global"
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

// Create 处理 POST /api/admin/attachments：登记一条外链附件（本地上传走 /attachments/upload）。
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

// Upload 处理 POST /api/admin/attachments/upload（multipart/form-data）。
//
// 字段：file（必填）、postId（必填）、title（可空，默认取文件名）、version、sort。
// 附件体积有限（默认 20MB），所以直接走 gin 的表单解析，不用流式读 part。
func (AttachmentApi) Upload(c *gin.Context) {
	limit := global.Config.File.MaxBytes + (1 << 20)
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, limit)

	fileHeader, err := c.FormFile("file")
	if err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			response.FailWithError(c, errs.New(errs.ErrorFileTooLarge,
				fmt.Sprintf("附件超过 %d MB 上限", global.Config.File.MaxBytes>>20)))
			return
		}
		response.FailWithDetailed(c, errs.ErrorValidation, nil, "没有收到文件字段 file")
		return
	}

	postID, parseErr := strconv.ParseInt(strings.TrimSpace(c.PostForm("postId")), 10, 64)
	if parseErr != nil || postID <= 0 {
		response.FailWithDetailed(c, errs.ErrorValidation,
			[]request.FieldError{{Field: "postId", Message: "必须是正整数"}}, "参数校验失败")
		return
	}

	sort := 0
	if raw := strings.TrimSpace(c.PostForm("sort")); raw != "" {
		parsed, sortErr := strconv.Atoi(raw)
		if sortErr != nil {
			response.FailWithDetailed(c, errs.ErrorValidation,
				[]request.FieldError{{Field: "sort", Message: "必须是整数"}}, "参数校验失败")
			return
		}
		sort = parsed
	}

	file, err := fileHeader.Open()
	if err != nil {
		response.FailWithDetailed(c, errs.ErrorValidation, nil, "读取上传内容失败")
		return
	}
	defer func() { _ = file.Close() }()

	item, err := service.ServiceGroupApp.Content.UploadAttachment(service.AttachmentUploadInput{
		File:     file,
		FileName: fileHeader.Filename,
		PostID:   postID,
		Title:    c.PostForm("title"),
		Version:  c.PostForm("version"),
		Sort:     &sort,
	}, actorFrom(c))
	if err != nil {
		response.FailWithError(c, err)
		return
	}
	response.OkWithDetailed(c, item, "附件已上传")
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

// bindAttachment 绑定附件请求体；绑定失败即写出 code 1003（HTTP 200）。
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
