package middleware

import (
	"time"

	"github.com/gin-gonic/gin"

	"github.com/unicornfairy864/blog-zjut-si/apps/server/global"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/utils"
)

// AccessLog 按契约字段输出访问日志；IP 只记哈希。
func AccessLog() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()

		global.Log.Info("http",
			"request_id", RequestIDFrom(c),
			"method", c.Request.Method,
			"path", c.Request.URL.Path,
			"status", c.Writer.Status(),
			"dur_ms", time.Since(start).Milliseconds(),
			"ip_hash", utils.HashIP(c.ClientIP()),
		)
	}
}
