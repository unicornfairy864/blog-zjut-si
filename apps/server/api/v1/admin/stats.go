package admin

import (
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/unicornfairy864/blog-zjut-si/apps/server/model/common/errs"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/model/common/request"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/model/common/response"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/service"
)

// StatsApi 处理 /api/admin/stats/*：看板与日粒度（§5）。
type StatsApi struct{}

// statsDateLayout 与 stats_daily.date 一致（上海日期）。
const statsDateLayout = "2006-01-02"

// Overview 处理 GET /api/admin/stats/overview：状态计数 + PV/UV + 点赞 + Top 5 文章。
func (StatsApi) Overview(c *gin.Context) {
	overview, err := service.ServiceGroupApp.Stats.Overview()
	if err != nil {
		response.FailWithError(c, err)
		return
	}
	response.OkWithData(c, overview)
}

// Daily 处理 GET /api/admin/stats/daily?from=&to=：全站日粒度，缺省最近 30 天。
func (StatsApi) Daily(c *gin.Context) {
	from := strings.TrimSpace(c.Query("from"))
	to := strings.TrimSpace(c.Query("to"))
	if fields := validateStatsRange(from, to); len(fields) > 0 {
		response.FailWithDetailed(c, errs.ErrorValidation, fields, "参数校验失败")
		return
	}

	daily, err := service.ServiceGroupApp.Stats.Daily(from, to)
	if err != nil {
		response.FailWithError(c, err)
		return
	}
	response.OkWithData(c, daily)
}

// validateStatsRange 校验日期格式与先后顺序；区间长度由 service 兜底（§5）。
func validateStatsRange(from, to string) []request.FieldError {
	fields := make([]request.FieldError, 0, 2)

	fromTime, fromErr := time.Parse(statsDateLayout, from)
	if from != "" && fromErr != nil {
		fields = append(fields, request.FieldError{Field: "from", Message: "from 必须是 YYYY-MM-DD"})
	}
	toTime, toErr := time.Parse(statsDateLayout, to)
	if to != "" && toErr != nil {
		fields = append(fields, request.FieldError{Field: "to", Message: "to 必须是 YYYY-MM-DD"})
	}
	if fromErr == nil && toErr == nil && to != "" && from != "" && toTime.Before(fromTime) {
		fields = append(fields, request.FieldError{Field: "to", Message: "to 不能早于 from"})
	}
	return fields
}
