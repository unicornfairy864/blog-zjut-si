package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"sync"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"gorm.io/gorm"
)

// versionTable 是 golang-migrate 约定的版本表名。
const versionTable = "schema_migrations"

// migrationDriver 实现 golang-migrate 的 database.Driver 接口。
//
// 为什么不直接用 migrate/database/sqlite：该包会 `_ "modernc.org/sqlite"`，
// 与 glebarez/go-sqlite 争抢 "sqlite" 驱动注册（sql.Register 重复即 panic）。
// 这里复用 gorm 已经建立的连接池，只补协议要求的版本表与执行逻辑（§7.1 V2 退路）。
type migrationDriver struct {
	db *sql.DB
	mu sync.Mutex
}

func newMigrationDriver(db *sql.DB) *migrationDriver {
	return &migrationDriver{db: db}
}

// Open 不支持按 URL 自建连接：迁移必须复用主连接池。
func (d *migrationDriver) Open(string) (database.Driver, error) {
	return nil, errors.New("迁移驱动只支持 WithInstance 方式")
}

// Close 不关闭共享连接池。
func (d *migrationDriver) Close() error { return nil }

// Lock 单进程部署，SQLite 文件锁已足够；无需额外迁移锁。
func (d *migrationDriver) Lock() error { return nil }

// Unlock 与 Lock 对称。
func (d *migrationDriver) Unlock() error { return nil }

// Run 执行一个迁移文件。
//
// SQLite 驱动支持单次 Exec 多条语句，整文件执行可以避免拆句把
// BEGIN ... END 的触发器块切断。
func (d *migrationDriver) Run(r io.Reader) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	body, err := io.ReadAll(r)
	if err != nil {
		return fmt.Errorf("读取迁移脚本失败: %w", err)
	}
	if _, err := d.db.ExecContext(context.Background(), string(body)); err != nil {
		return fmt.Errorf("执行迁移脚本失败: %w", err)
	}
	return nil
}

// SetVersion 记录当前版本与 dirty 标记。
func (d *migrationDriver) SetVersion(version int, dirty bool) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	ctx := context.Background()
	if err := d.ensureVersionTable(ctx); err != nil {
		return err
	}

	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("开启版本事务失败: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, "DELETE FROM "+versionTable); err != nil {
		return fmt.Errorf("清理版本记录失败: %w", err)
	}
	if version >= 0 {
		if _, err := tx.ExecContext(ctx,
			"INSERT INTO "+versionTable+" (version, dirty) VALUES (?, ?)", version, dirty); err != nil {
			return fmt.Errorf("写入版本记录失败: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("提交版本记录失败: %w", err)
	}
	return nil
}

// Version 返回当前版本；空库返回 -1。
func (d *migrationDriver) Version() (int, bool, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	ctx := context.Background()
	if err := d.ensureVersionTable(ctx); err != nil {
		return -1, false, err
	}

	var (
		version int
		dirty   bool
	)
	err := d.db.QueryRowContext(ctx, "SELECT version, dirty FROM "+versionTable+" LIMIT 1").Scan(&version, &dirty)
	if errors.Is(err, sql.ErrNoRows) {
		return -1, false, nil
	}
	if err != nil {
		return -1, false, fmt.Errorf("读取版本记录失败: %w", err)
	}
	return version, dirty, nil
}

// Drop 删除全部用户表（含 FTS 影子表）；仅用于测试与重建空库。
func (d *migrationDriver) Drop() error {
	d.mu.Lock()
	defer d.mu.Unlock()

	ctx := context.Background()
	rows, err := d.db.QueryContext(ctx,
		"SELECT type, name FROM sqlite_master WHERE name NOT LIKE 'sqlite_%' AND type IN ('table', 'view', 'trigger')")
	if err != nil {
		return fmt.Errorf("枚举数据库对象失败: %w", err)
	}
	type object struct{ kind, name string }
	var objects []object
	for rows.Next() {
		var o object
		if err := rows.Scan(&o.kind, &o.name); err != nil {
			_ = rows.Close()
			return err
		}
		objects = append(objects, o)
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return err
	}
	_ = rows.Close()

	for _, o := range objects {
		var stmt string
		switch o.kind {
		case "trigger":
			stmt = `DROP TRIGGER IF EXISTS "` + o.name + `"`
		case "view":
			stmt = `DROP VIEW IF EXISTS "` + o.name + `"`
		default:
			stmt = `DROP TABLE IF EXISTS "` + o.name + `"`
		}
		// FTS 影子表可能随主表一起消失，失败不必中断。
		_, _ = d.db.ExecContext(ctx, stmt)
	}
	return nil
}

// ensureVersionTable 建立版本表（契约外的最小表，仅迁移用）。
func (d *migrationDriver) ensureVersionTable(ctx context.Context) error {
	_, err := d.db.ExecContext(ctx,
		"CREATE TABLE IF NOT EXISTS "+versionTable+" (version INTEGER NOT NULL, dirty INTEGER NOT NULL)")
	if err != nil {
		return fmt.Errorf("创建版本表失败: %w", err)
	}
	return nil
}

// Migrate 把数据库推进到最新版本，返回最终版本号。
func Migrate(gdb *gorm.DB, fsys fs.FS, dir string) (int, error) {
	sqlDB, err := gdb.DB()
	if err != nil {
		return 0, fmt.Errorf("获取数据库连接池失败: %w", err)
	}

	src, err := iofs.New(fsys, dir)
	if err != nil {
		return 0, fmt.Errorf("读取内嵌迁移脚本失败: %w", err)
	}

	m, err := migrate.NewWithInstance("iofs", src, "sqlite", newMigrationDriver(sqlDB))
	if err != nil {
		return 0, fmt.Errorf("初始化迁移器失败: %w", err)
	}
	defer func() { _, _ = m.Close() }()

	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return 0, fmt.Errorf("迁移失败: %w", err)
	}

	version, dirty, err := m.Version()
	if err != nil && !errors.Is(err, migrate.ErrNilVersion) {
		return 0, fmt.Errorf("读取迁移版本失败: %w", err)
	}
	if dirty {
		return 0, errors.New("数据库处于 dirty 状态，请人工处理后再启动")
	}
	return int(version), nil
}
