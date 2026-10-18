package model

import (
	"gorm.io/gorm"

	"github.com/unicornfairy864/blog-zjut-si/apps/server/model/common/dbtime"
)

// jobs.status 的取值（agent.md §4）。
const (
	JobStatusPending = "pending"
	JobStatusRunning = "running"
	JobStatusDone    = "done"
	JobStatusFailed  = "failed"
)

// 任务类型（agent.md §4 / §13）；D8 先接媒体两类。
const (
	JobTypeMediaProcess = "media_process"
	JobTypeMediaArchive = "media_archive"
)

// DefaultJobMaxAttempts 是任务默认最大尝试次数。
const DefaultJobMaxAttempts = 3

// Job 对应 jobs 表。
type Job struct {
	ID          int64        `gorm:"column:id;primaryKey;autoIncrement" json:"id"`
	Type        string       `gorm:"column:type" json:"type"`
	Payload     string       `gorm:"column:payload" json:"payload"`
	RunAt       dbtime.Unix  `gorm:"column:run_at" json:"runAt"`
	Status      string       `gorm:"column:status" json:"status"`
	Attempts    int          `gorm:"column:attempts" json:"attempts"`
	MaxAttempts int          `gorm:"column:max_attempts" json:"maxAttempts"`
	LastError   string       `gorm:"column:last_error" json:"lastError"`
	LockedAt    *dbtime.Unix `gorm:"column:locked_at" json:"lockedAt"`
	CreatedAt   dbtime.Unix  `gorm:"column:created_at" json:"createdAt"`
}

// TableName 固定表名。
func (Job) TableName() string { return "jobs" }

// CountJobsByStatus 统计某状态的任务数（readyz 的队列积压检查用）。
func CountJobsByStatus(db *gorm.DB, status string) (int64, error) {
	var n int64
	err := db.Model(&Job{}).Where("status = ?", status).Count(&n).Error
	return n, err
}

// CreateJob 投递一条任务。
func CreateJob(db *gorm.DB, job *Job) error {
	return db.Create(job).Error
}

// ClaimNextJob 取最早到期的一条 pending 任务并标记 running（单进程调度器）。
//
// 契约要求"取任务前原子置 running"（§13）；这里用事务内的条件更新达成，
// 集群部署不在本项目范围内。
func ClaimNextJob(db *gorm.DB, now dbtime.Unix) (*Job, error) {
	var job Job
	err := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("status = ? AND run_at <= ?", JobStatusPending, now).
			Order("run_at ASC, id ASC").Take(&job).Error; err != nil {
			return wrapErr(err)
		}

		result := tx.Model(&Job{}).
			Where("id = ? AND status = ?", job.ID, JobStatusPending).
			Updates(map[string]any{
				"status":    JobStatusRunning,
				"locked_at": now,
				"attempts":  gorm.Expr("attempts + 1"),
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return ErrNotFound
		}

		job.Status = JobStatusRunning
		job.LockedAt = &now
		job.Attempts++
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &job, nil
}

// CompleteJob 把任务标记为完成。
func CompleteJob(db *gorm.DB, id int64) error {
	return db.Model(&Job{}).Where("id = ?", id).
		Updates(map[string]any{"status": JobStatusDone, "last_error": ""}).Error
}

// RetryJob 把任务放回 pending 并设置下次执行时间。
func RetryJob(db *gorm.DB, id int64, runAt dbtime.Unix, lastError string) error {
	return db.Model(&Job{}).Where("id = ?", id).
		Updates(map[string]any{
			"status":     JobStatusPending,
			"run_at":     runAt,
			"last_error": lastError,
			"locked_at":  nil,
		}).Error
}

// FailJob 把任务标记为失败（不再重试）。
func FailJob(db *gorm.DB, id int64, lastError string) error {
	return db.Model(&Job{}).Where("id = ?", id).
		Updates(map[string]any{
			"status":     JobStatusFailed,
			"last_error": lastError,
			"locked_at":  nil,
		}).Error
}

// ResetStaleJobs 把 locked_at 超时的 running 任务放回 pending（§6：超 10 分钟）。
func ResetStaleJobs(db *gorm.DB, before dbtime.Unix) (int64, error) {
	result := db.Model(&Job{}).
		Where("status = ? AND locked_at IS NOT NULL AND locked_at <= ?", JobStatusRunning, before).
		Updates(map[string]any{"status": JobStatusPending, "locked_at": nil})
	return result.RowsAffected, result.Error
}
