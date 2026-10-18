# 0002 · 迁移执行器：golang-migrate 核心 + 自写 database.Driver

日期：2026-10-18
状态：已采纳

## 背景

契约要求 `golang-migrate` + `migrations/` 版本化 SQL，且必须选纯 Go 驱动（§7）。
§7.1 V2 的失败退路是"自写极简 migrator"。

实现前验证发现：`github.com/golang-migrate/migrate/v4/database/sqlite` 的源码里有

```go
_ "modernc.org/sqlite"
```

即它会注册一个名为 `sqlite` 的驱动。而本项目按契约使用 `glebarez/sqlite`（gorm 驱动），
它底层是 `glebarez/go-sqlite`（modernc 的 fork），**也注册同名驱动 `sqlite`**。
两个包同时被链接时，`database/sql` 的重复注册会直接 panic。

## 选项

1. **改用 mattn/go-sqlite3（sqlite3 驱动）**：需要 CGO，违反静态编译要求。
2. **迁移时单独开一个 modernc 连接，再关掉**：同一进程里仍需链接 modernc 包，panic 依旧。
3. **不用 golang-migrate，自写 80 行 migrator**：可接受（§7.1 V2 退路），但丢掉 dirty 标记、
   source 抽象与版本校验，后续要自己补。
4. **用 golang-migrate 的核心（`migrate.NewWithInstance`），自写 `database.Driver`**（本决定）：
   只实现 `Run / Version / SetVersion / Lock / Unlock / Close / Drop`，复用 gorm 已经建立的
   `*sql.DB`。

## 决定

采用 4。实现见 `apps/server/core/db/migrate.go`：

- `Run(io.Reader)` 整文件 `Exec`：SQLite 驱动支持单次多语句，避免拆句切断
  `BEGIN … END` 的 FTS 触发器块。
- `SetVersion` 用事务覆盖 `schema_migrations` 的一行；`Version` 空库返回 `-1`。
- `Lock/Unlock` 是空实现：单实例部署，SQLite 文件锁已足够。
- `Close` 不关闭共享连接池。

## 后果

- 仍是 golang-migrate 的版本推进、dirty 检测与 iofs source；只是数据库侧换了 60 行适配。
- 依赖树里没有 `modernc.org/sqlite` 的驱动注册，二进制更小，也不会 panic。
- `Down` 不在业务路径上（回滚用"旧镜像 + 备份"），因此 `Drop` 只用于本地重建。
- 新增迁移仍需遵守 `{version}_{name}.{up,down}.sql` 命名；`dirty=true` 时进程拒绝启动。
