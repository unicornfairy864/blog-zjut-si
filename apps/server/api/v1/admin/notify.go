package admin

import (
	"github.com/gin-gonic/gin"

	"github.com/unicornfairy864/blog-zjut-si/apps/server/model/common/response"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/service"
)

// NotifyApi 处理 /api/admin/notify：告警通道自检（§13 / §14）。
type NotifyApi struct{}

// Test 处理 POST /api/admin/notify/test：立刻按当前配置投递一条测试告警。
//
// 只看通道配没配齐，不看 `enabled`，也不受静默期影响——这个接口就是用来"先证明能收到"的。
func (NotifyApi) Test(c *gin.Context) {
	if err := service.ServiceGroupApp.Notify.Test(c.Request.Context(), actorFrom(c)); err != nil {
		response.FailWithError(c, err)
		return
	}
	response.OkWithDetailed(c, nil, "测试告警已发送")
}
