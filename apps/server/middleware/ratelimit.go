package middleware

import (
	"time"

	"github.com/gin-gonic/gin"

	"github.com/unicornfairy864/blog-zjut-si/apps/server/model/common/errs"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/model/common/response"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/utils"
)

// RateLimit 按 key 限流；keyFn 为空时按来源 IP。
func RateLimit(limiter *utils.KeyedLimiter, keyFn func(*gin.Context) string) gin.HandlerFunc {
	if keyFn == nil {
		keyFn = func(c *gin.Context) string { return c.ClientIP() }
	}
	return func(c *gin.Context) {
		if !limiter.Allow(keyFn(c)) {
			response.FailWithDetailed(c, errs.ErrorRateLimited, nil, "请求过于频繁，请稍后再试")
			return
		}
		c.Next()
	}
}

// NewPublicAPILimiter 构造公开 API 的 20 r/s 限流器（agent.md §9）。
func NewPublicAPILimiter() *utils.KeyedLimiter {
	return utils.NewKeyedLimiter(time.Second/20, 20)
}

// NewCommentLimiter 构造评论限流器：3 次 / 10 分钟 / IP（agent.md §9）。
func NewCommentLimiter() *utils.KeyedLimiter {
	return utils.NewKeyedLimiter(10*time.Minute/3, 3)
}

// NewLikeLimiter 构造点赞限流器：10 次 / 分钟 / visitor（agent.md §9）。
//
// 键用 middleware.VisitorKey（加盐 visitor_hash），不是 IP。
func NewLikeLimiter() *utils.KeyedLimiter {
	return utils.NewKeyedLimiter(time.Minute/10, 10)
}
