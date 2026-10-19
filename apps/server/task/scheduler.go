// Package task 是后台任务调度器；任务类型从 D4 起逐步接入。
package task

import (
	"context"
	"errors"
	"time"

	"github.com/unicornfairy864/blog-zjut-si/apps/server/config"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/global"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/model"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/model/common/dbtime"
)

// Handler 执行一种任务；返回错误会触发退避重试。
type Handler func(ctx context.Context, job *model.Job) error

// wake 用于业务侧投递任务后立即唤醒调度器，避免等满一个 tick。
var wake = make(chan struct{}, 1)

// Notify 非阻塞地唤醒调度器；没有任务时也不会阻塞调用方。
func Notify() {
	select {
	case wake <- struct{}{}:
	default:
	}
}

// Scheduler 是单 goroutine 的 ticker 调度器（agent.md §13）。
type Scheduler struct {
	interval time.Duration
	enabled  bool
	handlers map[string]Handler
}

// NewScheduler 构造调度器；interval 来自 task.interval_seconds（默认 60s）。
func NewScheduler(cfg config.TaskConfig, handlers map[string]Handler) *Scheduler {
	interval := time.Duration(cfg.IntervalSeconds) * time.Second
	if interval <= 0 {
		interval = time.Minute
	}
	return &Scheduler{interval: interval, enabled: cfg.Enabled, handlers: handlers}
}

// Start 阻塞运行调度循环，直到 ctx 取消。
func (s *Scheduler) Start(ctx context.Context) error {
	if !s.enabled {
		global.Log.Info("任务调度器已禁用", "reason", "task.enabled=false")
		return nil
	}
	global.Log.Info("任务调度器启动", "interval", s.interval.String())

	// 启动先扫一次，避免重启后任务白等一个周期。
	s.dispatch(ctx)

	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			global.Log.Info("任务调度器停止")
			return nil
		case <-wake:
			s.dispatch(ctx)
		case <-ticker.C:
			s.dispatch(ctx)
		}
	}
}

const (
	// maxJobsPerTick 限制单次扫描最多处理的任务数，避免长期占用 CPU。
	maxJobsPerTick = 20
	// staleAfter 是 running 任务被视为卡死的时限（§6：10 分钟）。
	staleAfter = 10 * time.Minute
	// maxErrorLen 是 last_error 的最大字符数。
	maxErrorLen = 500
)

// dispatch 领取并串行执行到期任务（并发固定为 1）。
func (s *Scheduler) dispatch(ctx context.Context) {
	// 先把"当天该跑还没跑"的周期任务入队，再统一领取（§13）。
	s.enqueuePeriodic(time.Now())

	if n, err := model.ResetStaleJobs(global.DB, dbtime.Now().Add(-staleAfter)); err != nil {
		global.Log.Error("回收卡死任务失败", "err", err)
	} else if n > 0 {
		global.Log.Warn("回收卡死任务", "count", n)
	}

	for i := 0; i < maxJobsPerTick; i++ {
		select {
		case <-ctx.Done():
			return
		default:
		}

		job, err := model.ClaimNextJob(global.DB, dbtime.Now())
		if errors.Is(err, model.ErrNotFound) {
			return
		}
		if err != nil {
			global.Log.Error("领取任务失败", "err", err)
			return
		}
		s.run(ctx, job)
	}
}

// run 执行单个任务，并按 §6 退避策略处理失败。
func (s *Scheduler) run(ctx context.Context, job *model.Job) {
	handler, ok := s.handlers[job.Type]
	if !ok {
		_ = model.FailJob(global.DB, job.ID, "未知任务类型")
		global.Log.Error("未知任务类型", "type", job.Type, "id", job.ID)
		return
	}

	if err := handler(ctx, job); err != nil {
		if job.Attempts >= job.MaxAttempts {
			_ = model.FailJob(global.DB, job.ID, truncateError(err.Error()))
			global.Log.Error("任务失败", "type", job.Type, "id", job.ID,
				"attempts", job.Attempts, "err", err)
			return
		}
		delay := backoff(job.Attempts)
		if retryErr := model.RetryJob(global.DB, job.ID, dbtime.Now().Add(delay), truncateError(err.Error())); retryErr != nil {
			global.Log.Error("任务重试入库失败", "type", job.Type, "id", job.ID, "err", retryErr)
		}
		global.Log.Warn("任务重试", "type", job.Type, "id", job.ID,
			"attempts", job.Attempts, "retry_in", delay.String(), "err", err)
		return
	}

	if err := model.CompleteJob(global.DB, job.ID); err != nil {
		global.Log.Error("任务完成入库失败", "type", job.Type, "id", job.ID, "err", err)
	}
}

// backoff 返回 §6 的退避时长：1m / 5m / 15m。
func backoff(attempts int) time.Duration {
	switch attempts {
	case 1:
		return time.Minute
	case 2:
		return 5 * time.Minute
	default:
		return 15 * time.Minute
	}
}

// truncateError 截断错误文本，避免撑爆 jobs.last_error。
func truncateError(message string) string {
	runes := []rune(message)
	if len(runes) <= maxErrorLen {
		return message
	}
	return string(runes[:maxErrorLen])
}
