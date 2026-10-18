package middleware

import (
	"crypto/subtle"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/unicornfairy864/blog-zjut-si/apps/server/model/common/errs"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/model/common/response"
)

// CSRFHeader 是双提交令牌的请求头名（agent.md §5）。
const CSRFHeader = "X-CSRF-Token"

// CSRF 校验管理端写操作：GET/HEAD/OPTIONS 直接放行。
func CSRF() gin.HandlerFunc {
	return func(c *gin.Context) {
		switch c.Request.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			c.Next()
			return
		}

		sessionCtx, ok := SessionFrom(c)
		if !ok {
			response.FailWithDetailed(c, errs.ErrorAuth, nil, "未登录或会话已失效")
			return
		}

		provided := c.GetHeader(CSRFHeader)
		expected := sessionCtx.CSRFToken()
		if provided == "" || subtle.ConstantTimeCompare([]byte(provided), []byte(expected)) != 1 {
			response.FailWithDetailed(c, errs.ErrorForbidden, nil, "CSRF 校验失败，请刷新页面后重试")
			return
		}
		c.Next()
	}
}
