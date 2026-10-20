package admin

import (
	"errors"
	"io"
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

// MediaApi 处理 /api/admin/media/*：上传、媒体库、删除与重试。
type MediaApi struct{}

// Upload 处理 POST /api/admin/media（multipart/form-data）。
//
// 字段 order 约定：alt 必须在 file 之前（浏览器 FormData 按 append 顺序发送），
// 这样 alt 能在流式上传开始前拿到。
func (MediaApi) Upload(c *gin.Context) {
	limit := global.Config.Upload.MaxBytes + (1 << 20)
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, limit)

	reader, err := c.Request.MultipartReader()
	if err != nil {
		response.FailWithDetailed(c, errs.ErrorValidation, nil, "请使用 multipart/form-data 上传")
		return
	}

	var (
		alt      string
		result   *service.MediaUploadResult
		uploaded bool
	)

	for {
		part, partErr := reader.NextPart()
		if errors.Is(partErr, io.EOF) {
			break
		}
		if partErr != nil {
			response.FailWithDetailed(c, errs.ErrorValidation, nil, "读取上传内容失败")
			return
		}

		switch part.FormName() {
		case "alt":
			buf, _ := io.ReadAll(io.LimitReader(part, 1024))
			alt = strings.TrimSpace(string(buf))
			_ = part.Close()
		case "file":
			if part.FileName() == "" || uploaded {
				_ = part.Close()
				continue
			}
			uploaded = true
			result, err = service.ServiceGroupApp.Media.Upload(service.MediaUploadInput{
				File:     part,
				FileName: part.FileName(),
				Alt:      alt,
			}, actorFrom(c))
			_ = part.Close()
			if err != nil {
				response.FailWithError(c, err)
				return
			}
		default:
			_, _ = io.Copy(io.Discard, part)
			_ = part.Close()
		}
	}

	if !uploaded || result == nil {
		response.FailWithDetailed(c, errs.ErrorValidation, nil, "没有收到文件字段 file")
		return
	}

	msg := "图片已上传"
	if !result.Created {
		msg = "图片已存在，直接复用"
	}
	response.OkWithDetailed(c, service.ServiceGroupApp.Media.Item(result.Media), msg)
}

// List 处理 GET /api/admin/media。
func (MediaApi) List(c *gin.Context) {
	var page request.PageInfo
	if err := c.ShouldBindQuery(&page); err != nil {
		response.FailWithDetailed(c, errs.ErrorValidation, request.ValidationDetails(err), "参数校验失败")
		return
	}
	page.Normalize()

	items, total, err := service.ServiceGroupApp.Media.List(page.Page, page.PageSize)
	if err != nil {
		response.FailWithError(c, err)
		return
	}
	response.OkWithPage(c, items, total, page.Page, page.PageSize)
}

// Delete 处理 DELETE /api/admin/media/{id}：被引用时返回 code 2002（HTTP 200）。
func (MediaApi) Delete(c *gin.Context) {
	id, ok := mediaID(c)
	if !ok {
		return
	}
	if err := service.ServiceGroupApp.Media.Delete(id, actorFrom(c)); err != nil {
		response.FailWithError(c, err)
		return
	}
	response.OkWithDetailed(c, nil, "图片已删除")
}

// Retry 处理 POST /api/admin/media/{id}/retry：从本地原图重新生成变体。
func (MediaApi) Retry(c *gin.Context) {
	id, ok := mediaID(c)
	if !ok {
		return
	}
	item, err := service.ServiceGroupApp.Media.Retry(id, actorFrom(c))
	if err != nil {
		response.FailWithError(c, err)
		return
	}
	response.OkWithDetailed(c, item, "已重新处理")
}

// RemoveOriginal 处理 DELETE /api/admin/media/{id}/original：只删原图，保留四档变体（§10）。
func (MediaApi) RemoveOriginal(c *gin.Context) {
	id, ok := mediaID(c)
	if !ok {
		return
	}
	item, err := service.ServiceGroupApp.Media.RemoveOriginal(id, actorFrom(c))
	if err != nil {
		response.FailWithError(c, err)
		return
	}
	response.OkWithDetailed(c, item, "原图已删除，四档变体保留")
}

// mediaID 解析路径里的媒体主键；非法时直接写出 400。
func mediaID(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.FailWithDetailed(c, errs.ErrorValidation,
			[]request.FieldError{{Field: "id", Message: "必须是正整数"}}, "参数校验失败")
		return 0, false
	}
	return id, true
}
