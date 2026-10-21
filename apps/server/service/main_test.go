// Package service 的测试夹具（agent.md §7「测试」/ §17.2）。
//
// 为什么每个用例一套临时库：`global.DB` / `global.Config` / `global.Log` 都是进程级单例，
// 用例之间只能串行地替换它们（所以这里的用例**不写 `t.Parallel()`**，并行会互相踩数据）。
// 一套库 = 一个 `t.TempDir()`，迁移到最新版本后即用，用完由 `t.Cleanup` 关连接。
package service

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/unicornfairy864/blog-zjut-si/apps/server/config"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/core/db"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/global"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/migrations"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/model"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/model/common/dbtime"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/model/common/errs"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/utils"
)

// testEnv 是一次测试独占的运行环境。
type testEnv struct {
	// Dir 是这次测试的临时根目录（库、图片、附件、备份都在里面）。
	Dir string
}

// setupEnv 建一套隔离环境：临时目录 + 迁移完成的空库 + 静默日志 + 访客盐。
func setupEnv(t *testing.T) *testEnv {
	t.Helper()

	dir := t.TempDir()
	cfg := config.Config{
		Server: config.ServerConfig{
			Public:                   ":8640",
			Admin:                    ":8641",
			ReadHeaderTimeoutSeconds: 10,
			IdleTimeoutSeconds:       120,
			ShutdownTimeoutSeconds:   15,
		},
		Web: config.WebConfig{Upstream: "http://127.0.0.1:3000"},
		DB: config.DBConfig{
			Path:          filepath.Join(dir, "blog.db"),
			MaxOpenConns:  4,
			CacheSizeKiB:  16384,
			BusyTimeoutMS: 5000,
			AutoMigrate:   true,
		},
		Log: config.LogConfig{Level: "error"},
		Auth: config.AuthConfig{
			SessionTTLHours:   168,
			CookieName:        "sid",
			LoginPerMinute:    5,
			LockFailures:      5,
			LockWindowMinutes: 10,
			LockMinutes:       30,
		},
		Upload: config.UploadConfig{Dir: filepath.Join(dir, "uploads"), MaxBytes: 30 << 20},
		File:   config.FileConfig{Dir: filepath.Join(dir, "files"), MaxBytes: 20 << 20},
		Task:   config.TaskConfig{Enabled: true, IntervalSeconds: 60},
		Backup: config.BackupConfig{Dir: filepath.Join(dir, "backup")},
	}

	for _, d := range []string{cfg.Upload.Dir, cfg.File.Dir, cfg.Backup.Dir} {
		require.NoError(t, os.MkdirAll(d, 0o750))
	}

	global.Config = cfg
	global.Log = slog.New(slog.NewTextHandler(io.Discard, nil))

	handle, err := db.Open(cfg.DB)
	require.NoError(t, err, "打开测试库")
	global.DB = handle
	t.Cleanup(func() { _ = db.Close(handle) })

	version, err := db.Migrate(handle, migrations.FS, ".")
	require.NoError(t, err, "推进迁移")
	require.Positive(t, version, "迁移后版本号应为正")

	// 访客/IP 哈希依赖进程级盐；真实进程由 initialize.initSettings 注入。
	utils.SetPepper("test-pepper-0123456789")

	return &testEnv{Dir: dir}
}

// requireAppError 断言 err 是业务错误且 code 符合预期；只看错误码时用它。
//
// 与 appErrorOf 分成两个函数是为了过 errcheck：*errs.AppError 也实现了 error，
// 只断言不取值的调用点如果拿到返回值，就会被判成"未检查的错误"。
func requireAppError(t *testing.T, err error, code int) {
	t.Helper()

	_ = appErrorOf(t, err, code)
}

// appErrorOf 同 requireAppError，但把 AppError 返回给调用方继续看 Message / Details。
func appErrorOf(t *testing.T, err error, code int) *errs.AppError {
	t.Helper()

	require.Error(t, err)
	appErr, ok := errs.As(err)
	require.True(t, ok, "应当是业务错误，实际: %v", err)
	require.Equal(t, code, appErr.Code, "错误码不符（消息: %s）", appErr.Message)
	return appErr
}

// createAdmin 建一个管理员，返回其 ID（写操作的审计上下文要它）。
func createAdmin(t *testing.T, username string) int64 {
	t.Helper()

	hash, err := utils.HashPassword("test-password-0123456789")
	require.NoError(t, err)

	now := dbtime.Now()
	admin := &model.Admin{Username: username, PasswordHash: hash, CreatedAt: now, UpdatedAt: now}
	require.NoError(t, global.DB.Transaction(func(tx *gorm.DB) error {
		return model.CreateAdmin(tx, admin)
	}))
	require.NotZero(t, admin.ID)
	return admin.ID
}

// testActor 返回指向一个新建管理员的写操作上下文。
func testActor(t *testing.T) PostActor {
	t.Helper()

	return PostActor{AdminID: createAdmin(t, "tester"), IP: "203.0.113.7"}
}

// insertPost 绕过 service 直插一篇文章，用于给读路径造数据。
//
// status 传 model.PostStatusPublished 时补上 published_at（列表与公开端都按它过滤）。
func insertPost(t *testing.T, title, slug, status string) *model.Post {
	t.Helper()

	now := dbtime.Now()
	post := &model.Post{
		Slug:        slug,
		Title:       title,
		Excerpt:     title + " 的摘要",
		ContentMD:   "# " + title + "\n\n正文内容。",
		ContentHTML: "<h1>" + title + "</h1>",
		ContentText: title + " 正文内容。",
		Status:      status,
		CreatedAt:   now,
		UpdatedAt:   now,
		WordCount:   10,
		ReadingTime: 1,
		Lang:        model.DefaultPostLang,
		Revision:    1,
	}
	if status == model.PostStatusPublished {
		published := now
		post.PublishedAt = &published
	}
	require.NoError(t, global.DB.Create(post).Error)
	require.NotZero(t, post.ID)
	return post
}

// insertTag 直插一个标签。
func insertTag(t *testing.T, name, slug string) *model.Tag {
	t.Helper()

	tag := &model.Tag{Name: name, Slug: slug, CreatedAt: dbtime.Now()}
	require.NoError(t, global.DB.Create(tag).Error)
	return tag
}

// insertSeries 直插一个系列。
func insertSeries(t *testing.T, name, slug string) *model.Series {
	t.Helper()

	series := &model.Series{Name: name, Slug: slug, CreatedAt: dbtime.Now()}
	require.NoError(t, global.DB.Create(series).Error)
	return series
}

// countRows 统计一张表的行数，用于断言副作用（post_tags、audit_logs 之类）。
func countRows(t *testing.T, table string, where string, args ...any) int64 {
	t.Helper()

	query := "SELECT COUNT(*) FROM " + table
	if where != "" {
		query += " WHERE " + where
	}
	var count int64
	require.NoError(t, global.DB.Raw(query, args...).Scan(&count).Error)
	return count
}
