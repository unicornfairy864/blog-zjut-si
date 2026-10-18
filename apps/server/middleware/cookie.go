package middleware

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/unicornfairy864/blog-zjut-si/apps/server/global"
)

// SetSessionCookie 写入会话 cookie：HttpOnly + SameSite=Strict + Path=/。
//
// 管理端经 SSH 隧道访问 http://localhost:8641，因此不设 Secure（agent.md §9）。
func SetSessionCookie(c *gin.Context, token string, maxAge time.Duration) {
	c.SetSameSite(http.SameSiteStrictMode)
	c.SetCookie(
		global.Config.Auth.CookieName,
		token,
		int(maxAge.Seconds()),
		"/",
		"",
		global.Config.Auth.CookieSecure,
		true,
	)
}

// ClearSessionCookie 立刻让浏览器丢弃会话 cookie。
func ClearSessionCookie(c *gin.Context) {
	c.SetSameSite(http.SameSiteStrictMode)
	c.SetCookie(
		global.Config.Auth.CookieName,
		"",
		-1,
		"/",
		"",
		global.Config.Auth.CookieSecure,
		true,
	)
}
