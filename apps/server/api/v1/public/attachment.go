package public

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/unicornfairy864/blog-zjut-si/apps/server/model/common/errs"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/model/common/request"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/model/common/response"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/service"
)

// AttachmentApi 处理公开端的附件下载（§5）。
type AttachmentApi struct{}

// Go 处理 GET /api/attachments/{id}/go：查附件 → clicks+1 → 本地下发或 302 跳外链。
//
// 本地上传的附件（storage_key 非空）由本站直接下发，并且：
//   - Content-Type 固定 application/octet-stream、Content-Disposition: attachment，
//     配合 nosniff，保证任何扩展名都不会被浏览器当页面渲染（§10）；
//   - 支持 Range（http.ServeFile），断点续传与下载器都能用；
//   - 响应必须 no-store：同一路径每次都会改变计数（§5 / §12）。
//
// 历史的外链附件（只有 share_url）保持 302 跳转。计数失败不影响下载：访客要的是文件。
func (AttachmentApi) Go(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.FailWithDetailed(c, errs.ErrorValidation,
			[]request.FieldError{{Field: "id", Message: "必须是正整数"}}, "参数校验失败")
		return
	}

	item, err := service.ServiceGroupApp.Content.AttachmentByID(id)
	if err != nil {
		response.FailWithError(c, err)
		return
	}

	service.ServiceGroupApp.Content.TrackAttachmentClick(item.ID)

	c.Header("Cache-Control", "no-store")

	if item.StorageKey == "" {
		if item.ShareURL == "" {
			response.FailWithError(c, errs.New(errs.ErrorNotFound, "附件不可用"))
			return
		}
		c.Redirect(http.StatusFound, item.ShareURL)
		return
	}

	path, err := service.AttachmentPath(item)
	if err != nil {
		response.FailWithError(c, errs.New(errs.ErrorNotFound, "附件不可用"))
		return
	}
	if _, statErr := os.Stat(path); statErr != nil {
		if errors.Is(statErr, os.ErrNotExist) {
			response.FailWithError(c, errs.New(errs.ErrorGone, "附件文件已丢失"))
			return
		}
		response.FailWithError(c, fmt.Errorf("读取附件失败: %w", statErr))
		return
	}

	c.Header("Content-Type", "application/octet-stream")
	c.Header("Content-Disposition", contentDisposition(item.FileName))
	c.Header("X-Content-Type-Options", "nosniff")
	http.ServeFile(c.Writer, c.Request, path)
}

// contentDisposition 生成同时兼容 ASCII 与非 ASCII 文件名的 Content-Disposition。
//
// 老客户端只看 filename=，现代浏览器优先 filename*=UTF-8”（RFC 5987）。
func contentDisposition(fileName string) string {
	name := strings.TrimSpace(filepath.Base(fileName))
	if name == "" || name == "." || name == string(filepath.Separator) {
		name = "attachment"
	}

	ascii := strings.Map(func(r rune) rune {
		if r < 0x20 || r > 0x7e || r == '"' || r == '\\' {
			return '_'
		}
		return r
	}, name)

	return "attachment; filename=\"" + ascii + "\"; filename*=UTF-8''" + url.PathEscape(name)
}
