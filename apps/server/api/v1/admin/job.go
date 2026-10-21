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

// JobApi 处理 /api/admin/jobs*：任务队列的查看、手动投递与重试（§5）。
type JobApi struct{}

// jobEnqueueRequest 是手动投递入参；类型白名单在 service 校验。
type jobEnqueueRequest struct {
	Type string `json:"type" binding:"required,max=50"`
}

// List 处理 GET /api/admin/jobs?status=&page=&pageSize=。
func (JobApi) List(c *gin.Context) {
	var page request.PageInfo
	if err := c.ShouldBindQuery(&page); err != nil {
		response.FailWithDetailed(c, errs.ErrorValidation, request.ValidationDetails(err), "参数校验失败")
		return
	}
	page.Normalize()

	jobs, total, err := service.ServiceGroupApp.Ops.Jobs(strings.TrimSpace(c.Query("status")), page.Page, page.PageSize)
	if err != nil {
		response.FailWithError(c, err)
		return
	}
	response.OkWithPage(c, jobs, total, page.Page, page.PageSize)
}

// Enqueue 处理 POST /api/admin/jobs：手动投递 cleanup / backup / link_check / integrity_check。
func (JobApi) Enqueue(c *gin.Context) {
	var req jobEnqueueRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.FailWithDetailed(c, errs.ErrorValidation, request.ValidationDetails(err), "参数校验失败")
		return
	}

	job, err := service.ServiceGroupApp.Ops.EnqueueJob(strings.TrimSpace(req.Type), actorFrom(c))
	if err != nil {
		response.FailWithError(c, err)
		return
	}
	response.OkWithDetailed(c, job, "任务已投递")
}

// Retry 处理 POST /api/admin/jobs/{id}/retry。
func (JobApi) Retry(c *gin.Context) {
	id, ok := adminPathID(c, "id")
	if !ok {
		return
	}

	job, err := service.ServiceGroupApp.Ops.RetryJob(id, actorFrom(c))
	if err != nil {
		response.FailWithError(c, err)
		return
	}
	response.OkWithDetailed(c, job, "任务已重新排队")
}

// adminPathID 解析路径参数里的正整数主键；非法时直接写出 400。
func adminPathID(c *gin.Context, name string) (int64, bool) {
	id, err := strconv.ParseInt(c.Param(name), 10, 64)
	if err != nil || id <= 0 {
		response.FailWithDetailed(c, errs.ErrorValidation,
			[]request.FieldError{{Field: name, Message: "必须是正整数"}}, "参数校验失败")
		return 0, false
	}
	return id, true
}
