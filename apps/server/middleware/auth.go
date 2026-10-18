package middleware

import (
	"github.com/gin-gonic/gin"

	"github.com/unicornfairy864/blog-zjut-si/apps/server/global"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/model/common/errs"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/model/common/response"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/service"
)

// gin.Context 里会话上下文的键。
const (
	ctxKeySession = "auth.session"
	ctxKeyAdmin   = "auth.admin"
)

// Auth 校验 sid cookie；失败一律 401/1000 并中止请求。
func Auth() gin.HandlerFunc {
	return func(c *gin.Context) {
		token, err := c.Cookie(global.Config.Auth.CookieName)
		if err != nil || token == "" {
			response.FailWithDetailed(c, errs.ErrorAuth, nil, "未登录或会话已失效")
			return
		}

		sessionCtx, err := service.ServiceGroupApp.Auth.Authenticate(token)
		if err != nil {
			if appErr, ok := errs.As(err); ok && appErr.Code == errs.ErrorAuth {
				ClearSessionCookie(c)
			}
			response.FailWithError(c, err)
			return
		}

		c.Set(ctxKeySession, sessionCtx)
		c.Set(ctxKeyAdmin, sessionCtx.Admin)
		c.Next()
	}
}

// SessionFrom 取出鉴权中间件写入的会话上下文。
func SessionFrom(c *gin.Context) (*service.SessionContext, bool) {
	value, ok := c.Get(ctxKeySession)
	if !ok {
		return nil, false
	}
	sessionCtx, ok := value.(*service.SessionContext)
	return sessionCtx, ok
}
