package middleware

import (
	"github.com/gin-gonic/gin"
)

// SecurityHeaders 设置 §9 要求的三个最小响应头。
//
// CSP 由 Next 的 middleware 按 nonce 注入（CSP 只对 HTML 有意义）；
// API 响应不带 CSP，避免与前端策略混淆。
func SecurityHeaders() gin.HandlerFunc {
	return func(c *gin.Context) {
		header := c.Writer.Header()
		header.Set("X-Content-Type-Options", "nosniff")
		header.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		header.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		c.Next()
	}
}
