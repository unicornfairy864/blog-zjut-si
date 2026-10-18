// Package task 是后台任务调度器；任务类型从 D4 起逐步接入。
package task

import (
	"context"
	"time"

	"github.com/unicornfairy864/blog-zjut-si/apps/server/config"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/global"
)

// Scheduler 是单 goroutine 的 ticker 调度器（agent.md §13）。
type Scheduler struct {
	interval time.Duration
	enabled  bool
}

// NewScheduler 构造调度器；interval 来自 task.interval_seconds（默认 60s）。
func NewScheduler(cfg config.TaskConfig) *Scheduler {
	interval := time.Duration(cfg.IntervalSeconds) * time.Second
	if interval <= 0 {
		interval = time.Minute
	}
	return &Scheduler{interval: interval, enabled: cfg.Enabled}
}

// Start 阻塞运行调度循环，直到 ctx 取消。
func (s *Scheduler) Start(ctx context.Context) error {
	if !s.enabled {
		global.Log.Info("任务调度器已禁用", "reason", "task.enabled=false")
		return nil
	}
	global.Log.Info("任务调度器启动", "interval", s.interval.String())

	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			global.Log.Info("任务调度器停止")
			return nil
		case <-ticker.C:
			s.dispatch(ctx)
		}
	}
}

// dispatch 扫描到期任务并派发。
//
// D1–D3 还没有任务类型（jobs 表随 migration 建好），这里保持空实现；
// D4 起接 publish_scheduled，D7 起接 media_process，以此类推。
func (s *Scheduler) dispatch(ctx context.Context) {
	_ = ctx
}
