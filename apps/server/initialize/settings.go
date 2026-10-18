package initialize

import (
	"github.com/unicornfairy864/blog-zjut-si/apps/server/global"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/model"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/service"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/utils"
)

// initSettings 读取 visitor_salt 并注入哈希盐。
//
// migration 000002 已保证键存在；这里只处理"被手工清掉"的兜底。
func initSettings() {
	salt, err := service.ServiceGroupApp.Setting.String(model.SettingVisitorSalt)
	if err != nil {
		fatal("读取 visitor_salt 失败", err)
	}
	if salt == "" {
		salt, err = utils.RandomToken(32)
		if err != nil {
			fatal("生成 visitor_salt 失败", err)
		}
		if err := service.ServiceGroupApp.Setting.PutString(model.SettingVisitorSalt, salt); err != nil {
			fatal("写入 visitor_salt 失败", err)
		}
		global.Log.Warn("settings.visitor_salt 缺失，已重新生成")
	}
	utils.SetPepper(salt)
}
