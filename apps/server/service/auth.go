package service

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"

	"github.com/unicornfairy864/blog-zjut-si/apps/server/global"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/model"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/model/common/dbtime"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/model/common/errs"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/utils"
)

// AuthService 处理登录、登出与会话校验（C8：仅用户名 + 密码）。
type AuthService struct{}

// LoginInput 是登录入参；IP 与 UserAgent 只用于审计与哈希，不落明文。
type LoginInput struct {
	Username  string
	Password  string
	IP        string
	UserAgent string
}

// LoginResult 是登录成功后的会话信息；明文令牌只在这里出现一次，由 api 层写入 cookie。
type LoginResult struct {
	Admin     model.Admin
	Session   model.Session
	Token     string
	ExpiresAt dbtime.Unix
}

// SessionContext 是鉴权中间件拿到的会话上下文。
type SessionContext struct {
	Admin   model.Admin
	Session model.Session
}

// CSRFHash 是 CSRF 令牌的哈希，写库用。
func (c SessionContext) CSRFHash() string { return utils.SHA256Hex(c.CSRFToken()) }

// CSRFToken 由会话令牌哈希派生；服务端不需要保存明文即可复算校验。
func (c SessionContext) CSRFToken() string { return utils.CSRFToken(c.Session.TokenHash) }

// loginLimiter 按用户名限流（agent.md §9：登录 5/min 按用户名）。
var (
	loginLimiterOnce sync.Once
	loginLimiter     *utils.KeyedLimiter
)

func loginRateLimiter() *utils.KeyedLimiter {
	loginLimiterOnce.Do(func() {
		perMinute := global.Config.Auth.LoginPerMinute
		if perMinute < 1 {
			perMinute = 5
		}
		loginLimiter = utils.NewKeyedLimiter(time.Minute/time.Duration(perMinute), perMinute)
	})
	return loginLimiter
}

// dummyPasswordHash 用于用户不存在时仍跑一次 argon2，避免用响应时间枚举用户名。
var dummyPasswordHash = sync.OnceValue(func() string {
	hash, err := utils.HashPassword("timing-equalizer-placeholder")
	if err != nil {
		return ""
	}
	return hash
})

// Login 校验口令并创建会话。
func (s AuthService) Login(in LoginInput) (*LoginResult, error) {
	username := strings.TrimSpace(in.Username)
	if username == "" || in.Password == "" {
		return nil, errs.New(errs.ErrorValidation, "用户名和密码不能为空")
	}

	now := dbtime.Now()

	// 先判锁定、再判限流：账号已锁定时必须回 2005（锁定），不能回 1005（限流），
	// 否则用户看到的是"稍后再试"，怎么等也进不去（§9）。
	attempt, err := model.FindLoginAttempt(global.DB, username, model.AttemptKindLogin)
	if err != nil && !errors.Is(err, model.ErrNotFound) {
		return nil, fmt.Errorf("读取登录计数失败: %w", err)
	}
	if attempt != nil && attempt.LockedUntil > now {
		return nil, errs.WithDetails(errs.ErrorLoginLocked,
			fmt.Sprintf("登录失败次数过多，账号已锁定 %d 分钟", global.Config.Auth.LockMinutes),
			map[string]any{"lockedUntil": attempt.LockedUntil})
	}

	if !loginRateLimiter().Allow(username) {
		return nil, errs.New(errs.ErrorRateLimited, "登录尝试过于频繁，请稍后再试")
	}

	admin, adminErr := model.FindAdminByUsername(global.DB, username)
	verified := false
	switch {
	case adminErr == nil:
		verified = utils.VerifyPassword(admin.PasswordHash, in.Password)
	case errors.Is(adminErr, model.ErrNotFound):
		// 恒定时间：对占位哈希跑一遍，结果必然为 false。
		utils.VerifyPassword(dummyPasswordHash(), in.Password)
	default:
		return nil, fmt.Errorf("查询管理员失败: %w", adminErr)
	}

	if !verified {
		s.recordLoginFailure(username, now)
		ServiceGroupApp.Audit.Write(AuditInput{
			Action:     AuditActionLoginFailed,
			TargetType: "auth",
			TargetID:   username,
			Meta:       map[string]any{"reason": "invalid_credentials"},
			IP:         in.IP,
		})
		return nil, errs.New(errs.ErrorAuth, "用户名或密码错误")
	}

	token, err := utils.RandomToken(32)
	if err != nil {
		return nil, fmt.Errorf("生成会话令牌失败: %w", err)
	}
	tokenHash := utils.SHA256Hex(token)
	csrf := utils.CSRFToken(tokenHash)

	session := model.Session{
		TokenHash:  tokenHash,
		AdminID:    admin.ID,
		CSRFHash:   utils.SHA256Hex(csrf),
		IPHash:     utils.HashIP(in.IP),
		UA:         truncate(in.UserAgent, 255),
		CreatedAt:  now,
		LastSeenAt: now,
		ExpiresAt:  now.Add(global.Config.Auth.SessionTTL()),
	}

	if err := global.DB.Transaction(func(tx *gorm.DB) error {
		if err := model.DeleteLoginAttempt(tx, username, model.AttemptKindLogin); err != nil {
			return err
		}
		if err := model.CreateSession(tx, &session); err != nil {
			return err
		}
		if err := model.UpdateAdminLastLogin(tx, admin.ID, now); err != nil {
			return err
		}
		return ServiceGroupApp.Audit.WriteTx(tx, AuditInput{
			AdminID:    admin.ID,
			Action:     AuditActionLogin,
			TargetType: "auth",
			TargetID:   username,
			IP:         in.IP,
		})
	}); err != nil {
		return nil, fmt.Errorf("创建会话失败: %w", err)
	}

	admin.LastLoginAt = now
	return &LoginResult{Admin: *admin, Session: session, Token: token, ExpiresAt: session.ExpiresAt}, nil
}

// recordLoginFailure 在窗口内累加失败次数，达到阈值即锁定 30 分钟。
func (s AuthService) recordLoginFailure(username string, now dbtime.Unix) {
	window := global.Config.Auth.LockWindow()
	threshold := global.Config.Auth.LockFailures
	if threshold < 1 {
		threshold = 5
	}

	err := global.DB.Transaction(func(tx *gorm.DB) error {
		attempt, err := model.FindLoginAttempt(tx, username, model.AttemptKindLogin)
		if err != nil && !errors.Is(err, model.ErrNotFound) {
			return err
		}

		if attempt == nil || now.Sub(attempt.WindowStart) >= window {
			attempt = &model.LoginAttempt{
				Key:         username,
				Kind:        model.AttemptKindLogin,
				Count:       1,
				WindowStart: now,
			}
			if err := model.CreateLoginAttempt(tx, attempt); err != nil {
				return err
			}
			return nil
		}

		attempt.Count++
		if attempt.Count >= threshold {
			attempt.LockedUntil = now.Add(global.Config.Auth.LockDuration())
			attempt.Count = 0
			attempt.WindowStart = now
			if err := ServiceGroupApp.Audit.WriteTx(tx, AuditInput{
				Action:     AuditActionLoginLocked,
				TargetType: "auth",
				TargetID:   username,
				Meta:       map[string]any{"failures": threshold, "lockedUntil": attempt.LockedUntil},
			}); err != nil {
				return err
			}
		}
		return model.UpdateLoginAttempt(tx, attempt)
	})
	if err != nil {
		global.Log.Error("记录登录失败次数失败", "err", err)
	}
}

// Logout 删除会话；令牌无效时视为已登出。
func (s AuthService) Logout(tokenValue string) error {
	if tokenValue == "" {
		return nil
	}
	tokenHash := utils.SHA256Hex(tokenValue)

	session, err := model.FindSession(global.DB, tokenHash)
	if err != nil {
		if errors.Is(err, model.ErrNotFound) {
			return nil
		}
		return fmt.Errorf("查询会话失败: %w", err)
	}

	if err := global.DB.Transaction(func(tx *gorm.DB) error {
		if err := model.DeleteSession(tx, tokenHash); err != nil {
			return err
		}
		return ServiceGroupApp.Audit.WriteTx(tx, AuditInput{
			AdminID:    session.AdminID,
			Action:     AuditActionLogout,
			TargetType: "auth",
			TargetID:   session.TokenHash[:8],
		})
	}); err != nil {
		return fmt.Errorf("登出失败: %w", err)
	}
	return nil
}

// Authenticate 校验会话令牌并做 7 天滑动续期。
func (s AuthService) Authenticate(tokenValue string) (*SessionContext, error) {
	if tokenValue == "" {
		return nil, errs.New(errs.ErrorAuth, "未登录或会话已失效")
	}
	tokenHash := utils.SHA256Hex(tokenValue)

	session, err := model.FindSession(global.DB, tokenHash)
	if err != nil {
		if errors.Is(err, model.ErrNotFound) {
			return nil, errs.New(errs.ErrorAuth, "未登录或会话已失效")
		}
		return nil, fmt.Errorf("查询会话失败: %w", err)
	}

	now := dbtime.Now()
	if session.ExpiresAt <= now {
		_ = model.DeleteSession(global.DB, tokenHash)
		return nil, errs.New(errs.ErrorAuth, "会话已过期，请重新登录")
	}

	admin, err := model.FindAdminByID(global.DB, session.AdminID)
	if err != nil {
		if errors.Is(err, model.ErrNotFound) {
			_ = model.DeleteSession(global.DB, tokenHash)
			return nil, errs.New(errs.ErrorAuth, "账号不存在，请重新登录")
		}
		return nil, fmt.Errorf("查询管理员失败: %w", err)
	}

	// 滑动过期：最多每分钟刷一次库，避免每个请求都写。
	if now.Sub(session.LastSeenAt) >= time.Minute {
		expires := now.Add(global.Config.Auth.SessionTTL())
		if err := model.TouchSession(global.DB, tokenHash, now, expires); err != nil {
			global.Log.Warn("刷新会话失败", "err", err)
		} else {
			session.LastSeenAt = now
			session.ExpiresAt = expires
		}
	}

	return &SessionContext{Admin: *admin, Session: *session}, nil
}

// truncate 按字节截断，避免超长 UA 撑爆列。
func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max]
}
