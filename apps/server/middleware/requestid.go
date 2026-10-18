// Package middleware 实现 agent.md §7 的中间件链：
// request_id → panic 恢复 → 访问日志 → 限流 → 鉴权 → CSRF → handler。
package middleware

import (
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// ctxKeyRequestID 是 gin.Context 里请求 id 的键。
const ctxKeyRequestID = "request_id"

// RequestID 为每个请求分配 id；合法长度的上游 X-Request-Id 直接沿用。
func RequestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.GetHeader("X-Request-Id")
		if id == "" || len(id) > 64 {
			id = uuid.NewString()
		}
		c.Set(ctxKeyRequestID, id)
		c.Header("X-Request-Id", id)
		c.Next()
	}
}

// RequestIDFrom 取出当前请求 id；不存在时返回空串。
func RequestIDFrom(c *gin.Context) string {
	if value, ok := c.Get(ctxKeyRequestID); ok {
		if s, ok := value.(string); ok {
			return s
		}
	}
	return ""
}
