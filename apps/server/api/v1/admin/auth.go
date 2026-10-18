package admin

import (
	"github.com/gin-gonic/gin"

	"github.com/unicornfairy864/blog-zjut-si/apps/server/global"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/middleware"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/model/common/errs"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/model/common/request"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/model/common/response"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/service"
)

// loginRequest 是登录入参。
type loginRequest struct {
	Username string `json:"username" binding:"required,min=1,max=64"`
	Password string `json:"password" binding:"required,min=1,max=256"`
}

// AuthApi 处理 /api/admin/auth/*。
type AuthApi struct{}

// Login 处理 POST /api/admin/auth/login：成功后写 sid cookie。
func (AuthApi) Login(c *gin.Context) {
	var req loginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.FailWithDetailed(c, errs.ErrorValidation, request.ValidationDetails(err), "参数校验失败")
		return
	}

	result, err := service.ServiceGroupApp.Auth.Login(service.LoginInput{
		Username:  req.Username,
		Password:  req.Password,
		IP:        c.ClientIP(),
		UserAgent: c.Request.UserAgent(),
	})
	if err != nil {
		response.FailWithError(c, err)
		return
	}

	middleware.SetSessionCookie(c, result.Token, global.Config.Auth.SessionTTL())
	response.OkWithData(c, gin.H{
		"admin":     result.Admin,
		"expiresAt": result.ExpiresAt,
	})
}

// Me 处理 GET /api/admin/auth/me：下发管理员信息与明文 csrfToken。
//
// csrfToken 由会话令牌哈希派生，服务端只存哈希，可随时复算校验（双提交）。
func (AuthApi) Me(c *gin.Context) {
	sessionCtx, ok := middleware.SessionFrom(c)
	if !ok {
		response.FailWithDetailed(c, errs.ErrorAuth, nil, "未登录或会话已失效")
		return
	}
	response.OkWithData(c, gin.H{
		"admin":     sessionCtx.Admin,
		"csrfToken": sessionCtx.CSRFToken(),
		"expiresAt": sessionCtx.Session.ExpiresAt,
	})
}

// Logout 处理 POST /api/admin/auth/logout：删除会话并清 cookie。
func (AuthApi) Logout(c *gin.Context) {
	token, _ := c.Cookie(global.Config.Auth.CookieName)
	if err := service.ServiceGroupApp.Auth.Logout(token); err != nil {
		response.FailWithError(c, err)
		return
	}
	middleware.ClearSessionCookie(c)
	response.OkWithDetailed(c, nil, "已登出")
}
