package service

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/unicornfairy864/blog-zjut-si/apps/server/global"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/model"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/utils"
)

// readyz 的阈值（agent.md §5）。
const (
	// readyzDiskMaxPercent 是磁盘占用上限。
	readyzDiskMaxPercent = 85
	// readyzQueueMaxPending 是队列积压上限。
	readyzQueueMaxPending = 100
)

// HealthService 提供存活与就绪检查；只在 8641 暴露。
type HealthService struct{}

// ReadinessCheck 是单项就绪检查结果。
type ReadinessCheck struct {
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail,omitempty"`
}

// Ready 依次检查 DB、磁盘、uploads 可写、队列积压。
func (s HealthService) Ready(ctx context.Context) (bool, []ReadinessCheck) {
	checks := []ReadinessCheck{
		checkDatabase(ctx),
		checkDisk(global.Config.Upload.Dir),
		checkUploadsWritable(global.Config.Upload.Dir),
		checkQueue(),
	}

	ready := true
	for _, c := range checks {
		if !c.OK {
			ready = false
		}
	}
	return ready, checks
}

// checkDatabase 确认连接池可用。
func checkDatabase(ctx context.Context) ReadinessCheck {
	if global.DB == nil {
		return ReadinessCheck{Name: "database", OK: false, Detail: "数据库未初始化"}
	}
	sqlDB, err := global.DB.DB()
	if err != nil {
		return ReadinessCheck{Name: "database", OK: false, Detail: "获取连接池失败"}
	}
	if err := sqlDB.PingContext(ctx); err != nil {
		return ReadinessCheck{Name: "database", OK: false, Detail: "Ping 失败"}
	}
	return ReadinessCheck{Name: "database", OK: true}
}

// checkDisk 检查磁盘占用是否低于 85%。
func checkDisk(dir string) ReadinessCheck {
	used, err := utils.DiskUsagePercent(dir)
	if err != nil {
		return ReadinessCheck{Name: "disk", OK: false, Detail: "读取磁盘占用失败"}
	}
	if used < 0 {
		// 当前平台不支持统计，视为通过。
		return ReadinessCheck{Name: "disk", OK: true, Detail: "当前平台跳过"}
	}
	if used >= readyzDiskMaxPercent {
		return ReadinessCheck{Name: "disk", OK: false, Detail: fmt.Sprintf("磁盘占用 %d%%", used)}
	}
	return ReadinessCheck{Name: "disk", OK: true, Detail: fmt.Sprintf("磁盘占用 %d%%", used)}
}

// checkUploadsWritable 尝试在 uploads 下建临时文件。
func checkUploadsWritable(dir string) ReadinessCheck {
	if dir == "" {
		return ReadinessCheck{Name: "uploads", OK: false, Detail: "未配置上传目录"}
	}
	file, err := os.CreateTemp(dir, ".readyz-*")
	if err != nil {
		return ReadinessCheck{Name: "uploads", OK: false, Detail: "上传目录不可写"}
	}
	name := file.Name()
	_ = file.Close()
	_ = os.Remove(filepath.Clean(name))
	return ReadinessCheck{Name: "uploads", OK: true}
}

// checkQueue 检查待执行任务积压。
func checkQueue() ReadinessCheck {
	if global.DB == nil {
		return ReadinessCheck{Name: "queue", OK: false, Detail: "数据库未初始化"}
	}
	pending, err := model.CountJobsByStatus(global.DB, model.JobStatusPending)
	if err != nil {
		return ReadinessCheck{Name: "queue", OK: false, Detail: "统计任务失败"}
	}
	if pending >= readyzQueueMaxPending {
		return ReadinessCheck{Name: "queue", OK: false, Detail: fmt.Sprintf("积压 %d 个任务", pending)}
	}
	return ReadinessCheck{Name: "queue", OK: true, Detail: fmt.Sprintf("积压 %d", pending)}
}
