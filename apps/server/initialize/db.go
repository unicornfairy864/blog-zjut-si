package initialize

import (
	"github.com/unicornfairy864/blog-zjut-si/apps/server/core/db"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/global"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/migrations"
)

// initDB 打开连接池并按配置推进迁移。
//
// 迁移脚本内嵌在二进制里（migrations.FS），容器无需额外挂载。
func initDB() {
	handle, err := db.Open(global.Config.DB)
	if err != nil {
		fatal("打开数据库失败", err)
	}
	global.DB = handle

	if !global.Config.DB.AutoMigrate {
		global.Log.Warn("已跳过自动迁移", "reason", "db.auto_migrate=false")
		return
	}

	version, err := db.Migrate(global.DB, migrations.FS, ".")
	if err != nil {
		fatal("数据库迁移失败", err)
	}
	global.Log.Info("数据库就绪", "path", global.Config.DB.Path, "schema_version", version)
}
