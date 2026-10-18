package initialize

import (
	"os"
	"strings"

	"gorm.io/gorm"

	"github.com/unicornfairy864/blog-zjut-si/apps/server/global"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/model"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/model/common/dbtime"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/service"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/utils"
)

// 首个管理员的引导变量；只在库中没有管理员时生效（幂等）。
const (
	envAdminUsername = "BLOG_ADMIN_USERNAME"
	envAdminPassword = "BLOG_ADMIN_PASSWORD"
)

// bootstrapAdmin 在 admins 表为空且提供了环境变量时创建首个账号。
//
// 口令 ≥16 位（agent.md §9），只以 argon2id 哈希入库，绝不写日志。
func bootstrapAdmin() {
	count, err := model.CountAdmins(global.DB)
	if err != nil {
		fatal("统计管理员失败", err)
	}
	if count > 0 {
		return
	}

	username := strings.TrimSpace(os.Getenv(envAdminUsername))
	password := os.Getenv(envAdminPassword)
	if username == "" || password == "" {
		global.Log.Warn("库中没有管理员，已跳过引导创建",
			"hint", "设置 "+envAdminUsername+" 与 "+envAdminPassword+" 后重启即可创建")
		return
	}
	if len([]rune(password)) < utils.MinPasswordLength {
		global.Log.Error("初始管理员口令长度不足，未创建账号", "min_length", utils.MinPasswordLength)
		return
	}

	hash, err := utils.HashPassword(password)
	if err != nil {
		fatal("计算口令哈希失败", err)
	}

	now := dbtime.Now()
	admin := &model.Admin{Username: username, PasswordHash: hash, CreatedAt: now, UpdatedAt: now}
	if err := global.DB.Transaction(func(tx *gorm.DB) error {
		if err := model.CreateAdmin(tx, admin); err != nil {
			return err
		}
		return service.ServiceGroupApp.Audit.WriteTx(tx, service.AuditInput{
			AdminID:    admin.ID,
			Action:     service.AuditActionAdminBoot,
			TargetType: "admin",
			TargetID:   username,
		})
	}); err != nil {
		fatal("创建初始管理员失败", err)
	}

	global.Log.Info("已创建初始管理员", "username", username)
}
