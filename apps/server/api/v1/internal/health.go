package internal

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/unicornfairy864/blog-zjut-si/apps/server/model/common/dbtime"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/model/common/response"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/service"
)

// HealthApi 提供存活与就绪检查；只注册在 8641。
type HealthApi struct{}

// Healthz 处理 GET /healthz：只表示进程活着，不查磁盘与 DB。
func (HealthApi) Healthz(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok", "time": dbtime.Now()})
}

// Ready 处理 GET /readyz：DB 可查 + 磁盘 <85% + uploads 可写 + 队列积压 <100。
func (HealthApi) Ready(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 3*time.Second)
	defer cancel()

	ready, checks := service.ServiceGroupApp.Health.Ready(ctx)
	status := http.StatusOK
	if !ready {
		status = http.StatusServiceUnavailable
	}
	c.JSON(status, gin.H{"ready": ready, "checks": checks})
}

// InternalHealth 处理 GET /api/internal/health，走统一信封（D15 起给 Next 用）。
func (HealthApi) InternalHealth(c *gin.Context) {
	response.OkWithData(c, gin.H{"status": "ok", "time": dbtime.Now()})
}
