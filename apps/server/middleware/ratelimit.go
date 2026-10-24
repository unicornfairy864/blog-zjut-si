package middleware

import (
	"net"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/unicornfairy864/blog-zjut-si/apps/server/model/common/errs"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/model/common/response"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/utils"
)

// IsInternalCaller 判断请求是否来自同容器内的服务端调用（Next 的 SSR 抓取）。
//
// 判据是「回环来源 + 完全没有转发头」：容器里的 Next 直连 `127.0.0.1:8640`，不带
// `X-Forwarded-For`；而外部访客一定经宿主 nginx 进来，nginx 会写上转发头，此时
// `RemoteAddr` 是 docker 网关（属于受信代理）。两种情况不会混淆。
func IsInternalCaller(c *gin.Context) bool {
	if c.Request.Header.Get("X-Forwarded-For") != "" || c.Request.Header.Get("X-Real-IP") != "" {
		return false
	}
	host, _, err := net.SplitHostPort(c.Request.RemoteAddr)
	if err != nil {
		// RemoteAddr 不带端口时按原样解析；解析不出来就当外部调用（保守）。
		host = c.Request.RemoteAddr
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// RateLimit 按 key 限流；keyFn 为空时按来源 IP。
//
// 同容器内的服务端调用（见 IsInternalCaller）**不参与限流**：公开 API 的 20 r/s
// 是给外部访客的额度，而 Next 每渲染一个页面要发 2–3 次公开 API 调用，全部来自
// 回环、共用一个桶——不豁免的话，站点在约 10 页/秒时就会把自己的页面渲染打成
// `code 1005`，前端 `apiFetch` 抛出后整页 500（§9）。
func RateLimit(limiter *utils.KeyedLimiter, keyFn func(*gin.Context) string) gin.HandlerFunc {
	if keyFn == nil {
		keyFn = func(c *gin.Context) string { return c.ClientIP() }
	}
	return func(c *gin.Context) {
		if IsInternalCaller(c) {
			c.Next()
			return
		}
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
