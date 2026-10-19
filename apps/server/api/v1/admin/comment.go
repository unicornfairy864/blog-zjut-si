package admin

import (
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/unicornfairy864/blog-zjut-si/apps/server/middleware"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/model/common/errs"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/model/common/request"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/model/common/response"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/service"
)

// CommentApi 处理 /api/admin/comments：绑定 → 校验 → 调 service → 输出（§3.2）。
type CommentApi struct{}

// commentBulkMaxIDs 是批量审核的最大条数（§5）。
const commentBulkMaxIDs = 200

// commentPatchRequest 是单条审核入参。
type commentPatchRequest struct {
	ID     int64  `json:"id"`
	Status string `json:"status"`
}

// commentBulkRequest 是批量审核入参。
type commentBulkRequest struct {
	IDs    []int64 `json:"ids"`
	Status string  `json:"status"`
}

// List 处理 GET /api/admin/comments?status=&page=&pageSize=。
func (CommentApi) List(c *gin.Context) {
	var page request.PageInfo
	if err := c.ShouldBindQuery(&page); err != nil {
		response.FailWithDetailed(c, errs.ErrorValidation, request.ValidationDetails(err), "参数校验失败")
		return
	}
	page.Normalize()

	items, total, err := service.ServiceGroupApp.Comment.AdminList(c.Query("status"), page.Page, page.PageSize)
	if err != nil {
		response.FailWithError(c, err)
		return
	}
	response.OkWithPage(c, items, total, page.Page, page.PageSize)
}

// Patch 处理 PATCH /api/admin/comments：单条审核，返回更新后的单条。
func (CommentApi) Patch(c *gin.Context) {
	var req commentPatchRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.FailWithDetailed(c, errs.ErrorValidation, request.ValidationDetails(err), "参数校验失败")
		return
	}
	if fields := validateCommentPatch(req); len(fields) > 0 {
		response.FailWithDetailed(c, errs.ErrorValidation, fields, "参数校验失败")
		return
	}

	item, err := service.ServiceGroupApp.Comment.Patch(req.ID, req.Status, commentActorFrom(c))
	if err != nil {
		response.FailWithError(c, err)
		return
	}
	response.OkWithData(c, item)
}

// Bulk 处理 POST /api/admin/comments/bulk：批量审核，返回实际更新的条数。
func (CommentApi) Bulk(c *gin.Context) {
	var req commentBulkRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.FailWithDetailed(c, errs.ErrorValidation, request.ValidationDetails(err), "参数校验失败")
		return
	}

	ids, fields := normalizeCommentIDs(req.IDs)
	if len(fields) > 0 {
		response.FailWithDetailed(c, errs.ErrorValidation, fields, "参数校验失败")
		return
	}

	updated, err := service.ServiceGroupApp.Comment.Bulk(ids, req.Status, commentActorFrom(c))
	if err != nil {
		response.FailWithError(c, err)
		return
	}
	response.OkWithData(c, gin.H{"updated": updated})
}

// validateCommentPatch 校验单条审核入参；状态取值交给 service 判断。
func validateCommentPatch(req commentPatchRequest) []request.FieldError {
	fields := make([]request.FieldError, 0, 2)
	if req.ID <= 0 {
		fields = append(fields, request.FieldError{Field: "id", Message: "必须是正整数"})
	}
	if strings.TrimSpace(req.Status) == "" {
		fields = append(fields, request.FieldError{Field: "status", Message: "不能为空"})
	}
	return fields
}

// normalizeCommentIDs 校验并去重批量 id：1–200 个、必须为正整数（§5）。
func normalizeCommentIDs(raw []int64) ([]int64, []request.FieldError) {
	if len(raw) == 0 || len(raw) > commentBulkMaxIDs {
		return nil, []request.FieldError{{Field: "ids", Message: "数量必须在 1–200 之间"}}
	}

	ids := make([]int64, 0, len(raw))
	seen := make(map[int64]struct{}, len(raw))
	for _, id := range raw {
		if id <= 0 {
			return nil, []request.FieldError{{Field: "ids", Message: "必须全是正整数"}}
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	return ids, nil
}

// commentActorFrom 取审计上下文；Auth 中间件保证会话存在。
func commentActorFrom(c *gin.Context) service.CommentActor {
	sessionCtx, ok := middleware.SessionFrom(c)
	if !ok {
		return service.CommentActor{IP: c.ClientIP()}
	}
	return service.CommentActor{AdminID: sessionCtx.Admin.ID, IP: c.ClientIP()}
}
