package service

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/unicornfairy864/blog-zjut-si/apps/server/global"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/model"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/model/common/dbtime"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/model/common/errs"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/utils"
)

// testLoginPassword 与 main_test.go 里 createAdmin 使用的口令保持一致。
const testLoginPassword = "test-password-0123456789"

// 为什么每个用例都用独立用户名：loginRateLimiter 是 sync.Once 造出来的进程级单例
// （按用户名计数的令牌桶），用例之间没法重置，只有换个 key 才互不干扰。
func loginInput(username, password string) LoginInput {
	return LoginInput{
		Username:  username,
		Password:  password,
		IP:        "198.51.100.9",
		UserAgent: "unit-test-agent",
	}
}

// appErrorAny 断言 err 是业务错误并把它取出来（不限定 code，便于逐条断言 Code / Details）。
//
// 与 main_test.go 的 requireAppError / appErrorOf 区分开：那两个要指定预期 code。
func appErrorAny(t *testing.T, err error) *errs.AppError {
	t.Helper()

	require.Error(t, err)
	appErr, ok := errs.As(err)
	require.True(t, ok, "期望业务错误，实际是 %T：%v", err, err)
	return appErr
}

// TestAuthService_LoginLimiterFallsBackForNonPositiveRate 覆盖限流器的兜底分支：
// LoginPerMinute < 1 时必须回落到 5 次/分钟，而不是拿 0 做除数 panic。
//
// loginRateLimiter 是 sync.Once 单例，只有"进程内第一次登录"才真正构造它；本用例声明在
// auth_test.go 最前面（全量跑时它就是第一次）。即便单例已经建好，下面的断言依然成立，
// 所以这条用例不会因为执行顺序变化而变脆。
func TestAuthService_LoginLimiterFallsBackForNonPositiveRate(t *testing.T) {
	setupEnv(t)

	createAdmin(t, "limiter-fallback")
	global.Config.Auth.LoginPerMinute = 0

	_, err := AuthService{}.Login(loginInput("limiter-fallback", "wrong-password"))
	assert.Equal(t, errs.ErrorAuth, appErrorAny(t, err).Code,
		"限流器要有可用的突发额度，不能把第一次尝试就挡下")
}

func TestAuthService_LoginRejectsEmptyCredentials(t *testing.T) {
	setupEnv(t)

	// 空用户名 / 纯空白用户名 / 空口令都必须在碰数据库之前被挡下（1003）。
	for _, in := range []LoginInput{
		{Username: "", Password: "x"},
		{Username: "   ", Password: "x"},
		{Username: "someone", Password: ""},
	} {
		_, err := AuthService{}.Login(in)
		appErr := appErrorAny(t, err)
		assert.Equal(t, errs.ErrorValidation, appErr.Code, "入参 %+v", in)
	}
}

func TestAuthService_LoginSuccess(t *testing.T) {
	setupEnv(t)

	adminID := createAdmin(t, "login-ok")
	// 预置一条失败计数：成功登录必须把它清掉（§9 的"成功即清零"）。
	require.NoError(t, model.CreateLoginAttempt(global.DB, &model.LoginAttempt{
		Key: "login-ok", Kind: model.AttemptKindLogin, Count: 2, WindowStart: dbtime.Now(),
	}))

	result, err := AuthService{}.Login(LoginInput{
		Username:  "login-ok",
		Password:  testLoginPassword,
		IP:        "198.51.100.9",
		UserAgent: strings.Repeat("u", 300),
	})
	require.NoError(t, err)

	assert.Equal(t, adminID, result.Admin.ID)
	assert.Equal(t, "login-ok", result.Admin.Username)
	assert.Greater(t, int64(result.Admin.LastLoginAt), int64(0), "登录成功要更新 last_login_at")
	assert.NotEmpty(t, result.Token)

	// 库里只落令牌哈希；明文令牌只出现在返回值里（由 api 层写 cookie）。
	assert.Equal(t, utils.SHA256Hex(result.Token), result.Session.TokenHash)
	assert.NotEqual(t, result.Token, result.Session.TokenHash)
	assert.Equal(t, result.Session.ExpiresAt, result.ExpiresAt)
	assert.Greater(t, int64(result.ExpiresAt), int64(dbtime.Now()), "过期时间应在未来")

	// IP 只落加盐哈希，UA 按字节截断到 255。
	assert.Equal(t, utils.HashIP("198.51.100.9"), result.Session.IPHash)
	assert.NotEqual(t, "198.51.100.9", result.Session.IPHash)
	assert.Len(t, result.Session.UA, 255)

	// 副作用：失败计数清空、会话落库、审计写入、管理员最近登录时间落库。
	assert.Zero(t, countRows(t, "login_attempts", "key = ? AND kind = ?", "login-ok", model.AttemptKindLogin))
	assert.Equal(t, int64(1), countRows(t, "sessions", "token_hash = ?", result.Session.TokenHash))

	entry := auditLogByAction(t, AuditActionLogin)
	assert.Equal(t, adminID, entry.AdminID)
	assert.Equal(t, "auth", entry.TargetType)
	assert.Equal(t, "login-ok", entry.TargetID)
	assert.Equal(t, utils.HashIP("198.51.100.9"), entry.IPHash)

	var stored model.Admin
	require.NoError(t, global.DB.Where("id = ?", adminID).Take(&stored).Error)
	assert.Equal(t, result.Admin.LastLoginAt, stored.LastLoginAt)
}

func TestAuthService_LoginWrongPassword(t *testing.T) {
	setupEnv(t)

	createAdmin(t, "wrong-pw")

	_, err := AuthService{}.Login(loginInput("wrong-pw", "definitely-not-the-password"))
	appErr := appErrorAny(t, err)
	assert.Equal(t, errs.ErrorAuth, appErr.Code)
	assert.Equal(t, "用户名或密码错误", appErr.Message)

	// 失败要计数，并写一条 auth.login_failed 审计（meta 说明原因，IP 只落哈希）。
	attempt, err := model.FindLoginAttempt(global.DB, "wrong-pw", model.AttemptKindLogin)
	require.NoError(t, err)
	assert.Equal(t, 1, attempt.Count)
	assert.Zero(t, int64(attempt.LockedUntil), "第一次失败不该锁定")

	entry := auditLogByAction(t, AuditActionLoginFailed)
	assert.Equal(t, "auth", entry.TargetType)
	assert.Equal(t, "wrong-pw", entry.TargetID)
	assert.Equal(t, `{"reason":"invalid_credentials"}`, entry.MetaJSON)
	assert.Equal(t, utils.HashIP("198.51.100.9"), entry.IPHash)
	assert.Zero(t, countRows(t, "sessions", ""), "失败不该建会话")
}

func TestAuthService_LoginUnknownUserIsIndistinguishable(t *testing.T) {
	setupEnv(t)

	createAdmin(t, "known-user")

	_, errKnown := AuthService{}.Login(loginInput("known-user", "wrong-password"))
	_, errUnknown := AuthService{}.Login(loginInput("ghost-user", "wrong-password"))

	known := appErrorAny(t, errKnown)
	unknown := appErrorAny(t, errUnknown)

	// 不泄漏账号是否存在：错误码与文案必须完全一致。
	assert.Equal(t, known.Code, unknown.Code)
	assert.Equal(t, known.Message, unknown.Message)
	assert.Equal(t, errs.ErrorAuth, unknown.Code)

	// 未知用户名同样要计数与写审计，否则计数节奏就成了枚举信号。
	assert.Equal(t, int64(1), countRows(t, "login_attempts", "key = ?", "ghost-user"))
	assert.Equal(t, int64(2), countRows(t, "audit_logs", "action = ?", AuditActionLoginFailed))
}

func TestAuthService_LoginTrimsUsername(t *testing.T) {
	setupEnv(t)

	createAdmin(t, "trim-user")

	result, err := AuthService{}.Login(loginInput("  trim-user  ", testLoginPassword))
	require.NoError(t, err)
	assert.Equal(t, "trim-user", result.Admin.Username)
	assert.Equal(t, "trim-user", auditLogByAction(t, AuditActionLogin).TargetID)
}

func TestAuthService_LoginLocksAfterThresholdFailures(t *testing.T) {
	setupEnv(t)

	createAdmin(t, "lock-me")

	// 前 5 次（LockFailures=5）都是凭据错误，并且每次都计数。
	for i := 0; i < 5; i++ {
		_, err := AuthService{}.Login(loginInput("lock-me", "wrong-password"))
		appErr := appErrorAny(t, err)
		require.Equal(t, errs.ErrorAuth, appErr.Code, "第 %d 次失败应是凭据错误", i+1)
	}

	// 第 6 次：锁定优先于限流，必须是 2005 而不是 1005（契约明确"先判锁定再判限流"）。
	_, err := AuthService{}.Login(loginInput("lock-me", "wrong-password"))
	appErr := appErrorAny(t, err)
	assert.Equal(t, errs.ErrorLoginLocked, appErr.Code)
	assert.Contains(t, appErr.Message, "锁定")

	details, ok := appErr.Details.(map[string]any)
	require.True(t, ok, "锁定错误应带 lockedUntil，实际 %T", appErr.Details)
	lockedUntil, ok := details["lockedUntil"].(dbtime.Unix)
	require.True(t, ok, "lockedUntil 应是 dbtime.Unix，实际 %T", details["lockedUntil"])
	assert.Greater(t, int64(lockedUntil), int64(dbtime.Now()))

	// 锁定期内即使口令正确，也只能拿到 2005。
	_, err = AuthService{}.Login(loginInput("lock-me", testLoginPassword))
	assert.Equal(t, errs.ErrorLoginLocked, appErrorAny(t, err).Code)

	assert.Equal(t, int64(5), countRows(t, "audit_logs", "action = ?", AuditActionLoginFailed))
	assert.Equal(t, int64(1), countRows(t, "audit_logs", "action = ?", AuditActionLoginLocked))
	assert.Zero(t, countRows(t, "sessions", ""), "锁定期间不该建会话")
}

func TestAuthService_LoginRateLimitedOnlyWhenNotLocked(t *testing.T) {
	setupEnv(t)

	createAdmin(t, "rate-limited")
	// 把锁定阈值抬高，让第 6 次落在限流分支（1005）而不是锁定分支（2005）。
	global.Config.Auth.LockFailures = 100

	for i := 0; i < 5; i++ {
		_, err := AuthService{}.Login(loginInput("rate-limited", "wrong-password"))
		require.Equal(t, errs.ErrorAuth, appErrorAny(t, err).Code, "第 %d 次", i+1)
	}

	_, err := AuthService{}.Login(loginInput("rate-limited", "wrong-password"))
	appErr := appErrorAny(t, err)
	assert.Equal(t, errs.ErrorRateLimited, appErr.Code)
	assert.Contains(t, appErr.Message, "频繁")
}

func TestAuthService_RecordLoginFailureAccumulatesWithinWindow(t *testing.T) {
	setupEnv(t)

	const username = "window-accumulate"
	require.NoError(t, model.CreateLoginAttempt(global.DB, &model.LoginAttempt{
		Key: username, Kind: model.AttemptKindLogin, Count: 1, WindowStart: dbtime.Now(),
	}))

	AuthService{}.recordLoginFailure(username, dbtime.Now())

	attempt, err := model.FindLoginAttempt(global.DB, username, model.AttemptKindLogin)
	require.NoError(t, err)
	assert.Equal(t, 2, attempt.Count, "窗口内应累加")
	assert.Zero(t, int64(attempt.LockedUntil))
}

func TestAuthService_RecordLoginFailureLocksAtThreshold(t *testing.T) {
	setupEnv(t)

	const username = "window-threshold"
	require.NoError(t, model.CreateLoginAttempt(global.DB, &model.LoginAttempt{
		Key: username, Kind: model.AttemptKindLogin,
		Count: global.Config.Auth.LockFailures - 1, WindowStart: dbtime.Now(),
	}))

	AuthService{}.recordLoginFailure(username, dbtime.Now())

	attempt, err := model.FindLoginAttempt(global.DB, username, model.AttemptKindLogin)
	require.NoError(t, err)
	assert.Zero(t, attempt.Count, "触发锁定后计数归零")
	assert.Greater(t, int64(attempt.LockedUntil), int64(dbtime.Now()))
	assert.Equal(t, int64(1), countRows(t, "audit_logs", "action = ?", AuditActionLoginLocked))
}

func TestAuthService_RecordLoginFailureExpiredWindow(t *testing.T) {
	setupEnv(t)

	const username = "window-expired"
	oldWindow := dbtime.Now().Add(-11 * time.Minute) // 超出 LockWindowMinutes=10
	require.NoError(t, model.CreateLoginAttempt(global.DB, &model.LoginAttempt{
		Key: username, Kind: model.AttemptKindLogin, Count: 2, WindowStart: oldWindow,
	}))

	// 契约：窗口外的旧失败不计入，本次失败重开一个窗口（count 从 1 起）。
	// D18 修复：原先走"新建计数行"分支，撞上 (key,kind) 唯一索引后整笔回滚，
	// 计数既不重置也不累加；现在是在同一行上重新计数。
	AuthService{}.recordLoginFailure(username, dbtime.Now())

	attempt, err := model.FindLoginAttempt(global.DB, username, model.AttemptKindLogin)
	require.NoError(t, err)

	assert.Equal(t, 1, attempt.Count, "窗口过期后计数应从 1 重新开始")
	assert.Greater(t, int64(attempt.WindowStart), int64(oldWindow), "窗口起点应刷新到本次失败时间")
	assert.Zero(t, int64(attempt.LockedUntil), "重开窗口时旧锁应被清掉")
	assert.Equal(t, int64(1), countRows(t, "login_attempts", "key = ?", username), "只应有一行计数")
}

func TestAuthService_LoginLocksAgainAfterWindowExpired(t *testing.T) {
	setupEnv(t)

	const username = "window-stuck"
	createAdmin(t, username)
	global.Config.Auth.LockFailures = 3
	oldWindow := dbtime.Now().Add(-11 * time.Minute)
	require.NoError(t, model.CreateLoginAttempt(global.DB, &model.LoginAttempt{
		Key: username, Kind: model.AttemptKindLogin, Count: 2, WindowStart: oldWindow,
	}))

	// 阈值 3：窗口过期后重新计数，第 3 次失败达到阈值并锁定。
	// D18 修复前计数卡在旧窗口上，失败永远累加不上去，账号再也锁不上——
	// 等于暴力破解在第一个窗口之后就不再受保护。
	for i := 0; i < 3; i++ {
		_, err := AuthService{}.Login(loginInput(username, "wrong-password"))
		require.Equal(t, errs.ErrorAuth, appErrorAny(t, err).Code, "第 %d 次失败", i+1)
	}

	attempt, err := model.FindLoginAttempt(global.DB, username, model.AttemptKindLogin)
	require.NoError(t, err)
	assert.NotZero(t, int64(attempt.LockedUntil), "达到阈值后应写入锁定时间")
	assert.Greater(t, int64(attempt.LockedUntil), int64(dbtime.Now()), "锁定时间应在未来")

	// 锁定期内即使口令正确也回 2005，且带上解锁时间。
	_, err = AuthService{}.Login(loginInput(username, testLoginPassword))
	appErr := appErrorAny(t, err)
	assert.Equal(t, errs.ErrorLoginLocked, appErr.Code)
	assert.NotNil(t, appErr.Details)
}

func TestAuthService_AuthenticateValidToken(t *testing.T) {
	setupEnv(t)

	adminID := createAdmin(t, "auth-ok")
	result, err := AuthService{}.Login(loginInput("auth-ok", testLoginPassword))
	require.NoError(t, err)

	ctx, err := AuthService{}.Authenticate(result.Token)
	require.NoError(t, err)
	assert.Equal(t, adminID, ctx.Admin.ID)
	assert.Equal(t, "auth-ok", ctx.Admin.Username)
	assert.Equal(t, result.Session.TokenHash, ctx.Session.TokenHash)
	assert.Equal(t, result.Session.ExpiresAt, ctx.Session.ExpiresAt, "刚建的会话不该被滑动续期写库")
}

func TestAuthService_AuthenticateRejectsBadTokens(t *testing.T) {
	setupEnv(t)

	for _, token := range []string{"", "not-a-token", strings.Repeat("f", 64)} {
		_, err := AuthService{}.Authenticate(token)
		appErr := appErrorAny(t, err)
		assert.Equal(t, errs.ErrorAuth, appErr.Code, "token=%q", token)
	}
}

func TestAuthService_AuthenticateExpiredSession(t *testing.T) {
	setupEnv(t)

	adminID := createAdmin(t, "expired-session")
	now := dbtime.Now()
	session := model.Session{
		TokenHash:  utils.SHA256Hex("expired-token"),
		AdminID:    adminID,
		CSRFHash:   "irrelevant",
		CreatedAt:  now.Add(-time.Hour),
		LastSeenAt: now.Add(-time.Hour),
		ExpiresAt:  now.Add(-time.Minute),
	}
	require.NoError(t, model.CreateSession(global.DB, &session))

	_, err := AuthService{}.Authenticate("expired-token")
	appErr := appErrorAny(t, err)
	assert.Equal(t, errs.ErrorAuth, appErr.Code)
	assert.Contains(t, appErr.Message, "过期")

	// 过期会话顺手清掉，不留垃圾行。
	assert.Zero(t, countRows(t, "sessions", "token_hash = ?", session.TokenHash))
}

func TestAuthService_AuthenticateDeletesSessionWhenAdminGone(t *testing.T) {
	setupEnv(t)

	adminID := createAdmin(t, "gone-admin")
	result, err := AuthService{}.Login(loginInput("gone-admin", testLoginPassword))
	require.NoError(t, err)

	require.NoError(t, global.DB.Exec("DELETE FROM admins WHERE id = ?", adminID).Error)

	_, err = AuthService{}.Authenticate(result.Token)
	appErr := appErrorAny(t, err)
	assert.Equal(t, errs.ErrorAuth, appErr.Code)
	assert.Contains(t, appErr.Message, "账号不存在")
	assert.Zero(t, countRows(t, "sessions", "token_hash = ?", result.Session.TokenHash))
}

func TestAuthService_AuthenticateSlidesExpiry(t *testing.T) {
	setupEnv(t)

	adminID := createAdmin(t, "slide-admin")
	now := dbtime.Now()
	session := model.Session{
		TokenHash:  utils.SHA256Hex("slide-token"),
		AdminID:    adminID,
		CSRFHash:   "irrelevant",
		CreatedAt:  now.Add(-time.Hour),
		LastSeenAt: now.Add(-2 * time.Minute), // 超过 1 分钟才会触发滑动续期
		ExpiresAt:  now.Add(time.Hour),
	}
	require.NoError(t, model.CreateSession(global.DB, &session))

	ctx, err := AuthService{}.Authenticate("slide-token")
	require.NoError(t, err)
	assert.Greater(t, int64(ctx.Session.ExpiresAt), int64(session.ExpiresAt))
	assert.Greater(t, int64(ctx.Session.LastSeenAt), int64(session.LastSeenAt))

	stored, err := model.FindSession(global.DB, session.TokenHash)
	require.NoError(t, err)
	assert.Equal(t, ctx.Session.ExpiresAt, stored.ExpiresAt)
	assert.Equal(t, ctx.Session.LastSeenAt, stored.LastSeenAt)
}

func TestAuthService_LogoutInvalidatesToken(t *testing.T) {
	setupEnv(t)

	createAdmin(t, "logout-admin")
	result, err := AuthService{}.Login(loginInput("logout-admin", testLoginPassword))
	require.NoError(t, err)

	require.NoError(t, AuthService{}.Logout(result.Token))

	_, err = AuthService{}.Authenticate(result.Token)
	assert.Equal(t, errs.ErrorAuth, appErrorAny(t, err).Code, "登出后同一个 token 不能再用")
	assert.Zero(t, countRows(t, "sessions", "token_hash = ?", result.Session.TokenHash))

	entry := auditLogByAction(t, AuditActionLogout)
	assert.Equal(t, result.Session.TokenHash[:8], entry.TargetID)

	// 重复登出、空令牌都视为"已经登出"，不报错也不重复写审计。
	require.NoError(t, AuthService{}.Logout(result.Token))
	require.NoError(t, AuthService{}.Logout(""))
	assert.Equal(t, int64(1), countRows(t, "audit_logs", "action = ?", AuditActionLogout))
}

func TestSessionContext_CSRFIsStableAndSessionScoped(t *testing.T) {
	setupEnv(t)

	createAdmin(t, "csrf-admin")

	first, err := AuthService{}.Login(loginInput("csrf-admin", testLoginPassword))
	require.NoError(t, err)
	second, err := AuthService{}.Login(loginInput("csrf-admin", testLoginPassword))
	require.NoError(t, err)

	ctxFirst := SessionContext{Admin: first.Admin, Session: first.Session}
	ctxSecond := SessionContext{Admin: second.Admin, Session: second.Session}

	// 同一个会话稳定。
	assert.Equal(t, ctxFirst.CSRFToken(), ctxFirst.CSRFToken())
	assert.Equal(t, ctxFirst.CSRFHash(), ctxFirst.CSRFHash())

	// 由令牌哈希派生，且与令牌明文/哈希都不同（不能拿 token 当 CSRF 用）。
	assert.Equal(t, utils.CSRFToken(first.Session.TokenHash), ctxFirst.CSRFToken())
	assert.NotEqual(t, first.Token, ctxFirst.CSRFToken())
	assert.NotEqual(t, first.Session.TokenHash, ctxFirst.CSRFToken())

	// 落库的是 CSRF 令牌的哈希。
	assert.Equal(t, utils.SHA256Hex(ctxFirst.CSRFToken()), ctxFirst.CSRFHash())
	assert.Equal(t, first.Session.CSRFHash, ctxFirst.CSRFHash())

	// 不同会话不同。
	assert.NotEqual(t, ctxFirst.CSRFToken(), ctxSecond.CSRFToken())
	assert.NotEqual(t, ctxFirst.CSRFHash(), ctxSecond.CSRFHash())

	// 服务端不需要保存明文：从库里读回会话也能复算出同一个 CSRF。
	stored, err := model.FindSession(global.DB, utils.SHA256Hex(first.Token))
	require.NoError(t, err)
	assert.Equal(t, ctxFirst.CSRFHash(), stored.CSRFHash)
}

func TestTruncate(t *testing.T) {
	// 按字节截断（不是按 rune），超长 UA 不会撑爆列。
	assert.Equal(t, "abc", truncate("abc", 3))
	assert.Equal(t, "abc", truncate("abcdef", 3))
	assert.Equal(t, "", truncate("abc", 0))
	assert.Equal(t, "中文", truncate("中文字", 6))
}

// TestAuthService_DatabaseFailures 覆盖"查询/写入失败"这些平时跑不到的分支：
// 它们必须返回带上下文的内部错误，而不是把 ErrNotFound 当成"没查到"继续往下走。
func TestAuthService_DatabaseFailures(t *testing.T) {
	t.Run("登录时读计数失败", func(t *testing.T) {
		setupEnv(t)
		require.NoError(t, global.DB.Exec("DROP TABLE login_attempts").Error)

		_, err := AuthService{}.Login(loginInput("db-fail-counter", "x"))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "读取登录计数失败")
	})

	t.Run("登录时查管理员失败", func(t *testing.T) {
		setupEnv(t)
		require.NoError(t, global.DB.Exec("DROP TABLE admins").Error)

		_, err := AuthService{}.Login(loginInput("db-fail-admin", "x"))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "查询管理员失败")
	})

	t.Run("登录时建会话失败", func(t *testing.T) {
		setupEnv(t)
		createAdmin(t, "db-fail-session")
		require.NoError(t, global.DB.Exec("DROP TABLE sessions").Error)

		_, err := AuthService{}.Login(loginInput("db-fail-session", testLoginPassword))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "创建会话失败")
	})

	t.Run("登出时查会话失败", func(t *testing.T) {
		setupEnv(t)
		require.NoError(t, global.DB.Exec("DROP TABLE sessions").Error)

		err := AuthService{}.Logout("whatever")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "查询会话失败")
	})

	t.Run("鉴权时查会话失败", func(t *testing.T) {
		setupEnv(t)
		require.NoError(t, global.DB.Exec("DROP TABLE sessions").Error)

		_, err := AuthService{}.Authenticate("whatever")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "查询会话失败")
	})

	t.Run("鉴权时查管理员失败", func(t *testing.T) {
		setupEnv(t)
		adminID := createAdmin(t, "db-fail-auth-admin")
		now := dbtime.Now()
		session := model.Session{
			TokenHash: utils.SHA256Hex("db-fail-token"),
			AdminID:   adminID,
			CreatedAt: now,
			ExpiresAt: now.Add(time.Hour),
			// LastSeenAt 留在"刚刚"，避免顺手走到滑动续期分支。
			LastSeenAt: now,
		}
		require.NoError(t, model.CreateSession(global.DB, &session))
		require.NoError(t, global.DB.Exec("DROP TABLE admins").Error)

		_, err := AuthService{}.Authenticate("db-fail-token")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "查询管理员失败")
	})

	t.Run("记录失败次数出错只记日志", func(t *testing.T) {
		setupEnv(t)
		require.NoError(t, global.DB.Exec("DROP TABLE login_attempts").Error)

		// 计数故障不能把登录失败的主流程拖垮：recordLoginFailure 不返回错误。
		assert.NotPanics(t, func() {
			AuthService{}.recordLoginFailure("db-fail-record", dbtime.Now())
		})
	})

	t.Run("锁定审计写失败则整笔回滚", func(t *testing.T) {
		setupEnv(t)
		require.NoError(t, global.DB.Exec("DROP TABLE audit_logs").Error)
		require.NoError(t, model.CreateLoginAttempt(global.DB, &model.LoginAttempt{
			Key: "db-fail-lock-audit", Kind: model.AttemptKindLogin,
			Count: global.Config.Auth.LockFailures - 1, WindowStart: dbtime.Now(),
		}))

		assert.NotPanics(t, func() {
			AuthService{}.recordLoginFailure("db-fail-lock-audit", dbtime.Now())
		})

		// 审计与计数在同一事务里：审计写不进去，计数与锁定时间也不能落下。
		attempt, err := model.FindLoginAttempt(global.DB, "db-fail-lock-audit", model.AttemptKindLogin)
		require.NoError(t, err)
		assert.Equal(t, global.Config.Auth.LockFailures-1, attempt.Count, "事务回滚后计数保持原样")
		assert.Zero(t, int64(attempt.LockedUntil), "事务回滚后不该落锁")
	})

	t.Run("登出时写审计失败要报错", func(t *testing.T) {
		setupEnv(t)
		adminID := createAdmin(t, "db-fail-logout")
		now := dbtime.Now()
		require.NoError(t, model.CreateSession(global.DB, &model.Session{
			TokenHash:  utils.SHA256Hex("logout-tx-token"),
			AdminID:    adminID,
			CreatedAt:  now,
			LastSeenAt: now,
			ExpiresAt:  now.Add(time.Hour),
		}))
		// 审计写不进去 → 整个登出事务回滚（会话不该被删掉）。
		require.NoError(t, global.DB.Exec("DROP TABLE audit_logs").Error)

		err := AuthService{}.Logout("logout-tx-token")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "登出失败")
		assert.Equal(t, int64(1), countRows(t, "sessions", "token_hash = ?", utils.SHA256Hex("logout-tx-token")),
			"审计写失败时整个登出事务回滚，会话不能被删掉")
	})
}
