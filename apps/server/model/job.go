package model

import (
	"github.com/unicornfairy864/blog-zjut-si/apps/server/model/common/dbtime"
	"gorm.io/gorm"
)

// jobs.status 的取值（agent.md §4）。
const (
	JobStatusPending = "pending"
	JobStatusRunning = "running"
	JobStatusDone    = "done"
	JobStatusFailed  = "failed"
)

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
