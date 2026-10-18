package middleware

import (
	"fmt"
	"io"
	"runtime/debug"

	"github.com/gin-gonic/gin"

	"github.com/unicornfairy864/blog-zjut-si/apps/server/global"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/model/common/errs"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/model/common/response"
)

// Recovery 捕获 panic：记日志 + 统一信封，绝不把堆栈返回前端。
func Recovery() gin.HandlerFunc {
	return gin.CustomRecoveryWithWriter(io.Discard, func(c *gin.Context, recovered any) {
		global.Log.Error("请求处理 panic",
			"request_id", RequestIDFrom(c),
			"method", c.Request.Method,
			"path", c.Request.URL.Path,
			"panic", fmt.Sprint(recovered),
			"stack", string(debug.Stack()),
		)
		response.FailWithDetailed(c, errs.ErrorInternal, nil, "服务器内部错误")
	})
}
