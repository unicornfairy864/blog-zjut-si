// Package db 负责 SQLite 连接、PRAGMA 设置与版本化迁移。
package db

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"github.com/unicornfairy864/blog-zjut-si/apps/server/config"
)

// Open 按契约建立连接池：4 连接、每连接 16MB 缓存、WAL、写事务 immediate。
//
// PRAGMA 必须写进 DSN，否则只有第一条连接生效（agent.md §7）。
func Open(cfg config.DBConfig) (*gorm.DB, error) {
	dsn, err := buildDSN(cfg)
	if err != nil {
		return nil, err
	}

	gdb, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		// 写事务由 service 显式发起，避免 gorm 隐式包一层。
		SkipDefaultTransaction: true,
		// gorm 自带的 SQL 日志是纯文本，会污染 JSON 日志；错误由上层统一记录。
		Logger: logger.Default.LogMode(logger.Silent),
		// 时间统一用 Unix 秒（dbtime.Unix），不让 gorm 自动维护时间列。
		NowFunc: func() time.Time { return time.Now().UTC() },
	})
	if err != nil {
		return nil, fmt.Errorf("打开数据库失败: %w", err)
	}

	sqlDB, err := gdb.DB()
	if err != nil {
		return nil, fmt.Errorf("获取数据库连接池失败: %w", err)
	}
	sqlDB.SetMaxOpenConns(cfg.MaxOpenConns)
	sqlDB.SetMaxIdleConns(cfg.MaxOpenConns)
	sqlDB.SetConnMaxLifetime(0)
	sqlDB.SetConnMaxIdleTime(0)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := sqlDB.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("数据库连接不可用: %w", err)
	}

	var journalMode string
	if err := sqlDB.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&journalMode); err != nil {
		return nil, fmt.Errorf("读取 journal_mode 失败: %w", err)
	}
	if !strings.EqualFold(journalMode, "wal") {
		slog.Warn("数据库未运行在 WAL 模式", "journal_mode", journalMode)
	}

	return gdb, nil
}

// Close 关闭连接池。
func Close(gdb *gorm.DB) error {
	if gdb == nil {
		return nil
	}
	sqlDB, err := gdb.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}

// buildDSN 生成 modernc 风格的 DSN（glebarez/sqlite 复用该实现）。
func buildDSN(cfg config.DBConfig) (string, error) {
	abs, err := filepath.Abs(cfg.Path)
	if err != nil {
		return "", fmt.Errorf("解析数据库路径失败: %w", err)
	}
	if dir := filepath.Dir(abs); dir != "" {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return "", fmt.Errorf("创建数据库目录失败: %w", err)
		}
	}

	params := []string{
		"_pragma=journal_mode(WAL)",
		"_pragma=synchronous(NORMAL)",
		fmt.Sprintf("_pragma=busy_timeout(%d)", cfg.BusyTimeoutMS),
		"_pragma=foreign_keys(ON)",
		"_pragma=temp_store(MEMORY)",
		fmt.Sprintf("_pragma=cache_size(-%d)", cfg.CacheSizeKiB),
		// SQLite 写事务默认 deferred，并发下容易 SQLITE_BUSY；契约要求 immediate。
		"_txlock=immediate",
	}
	return "file:" + filepath.ToSlash(abs) + "?" + strings.Join(params, "&"), nil
}
