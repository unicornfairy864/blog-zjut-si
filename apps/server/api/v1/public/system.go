package public

import (
	"github.com/gin-gonic/gin"

	"github.com/unicornfairy864/blog-zjut-si/apps/server/model/common/dbtime"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/model/common/response"
)

// SystemApi 提供运维探活用的最小公开接口。
type SystemApi struct{}

// Ping 处理 GET /api/system/ping：骨架打通端点（D2），不承载业务数据。
func (SystemApi) Ping(c *gin.Context) {
	response.OkWithData(c, gin.H{"pong": true, "time": dbtime.Now()})
}
