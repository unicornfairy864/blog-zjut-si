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

// 任务类型（agent.md §4 / §13）。
const (
	JobTypePublishScheduled = "publish_scheduled"
	JobTypeMediaProcess     = "media_process"
	JobTypeLinkCheck        = "link_check"
	JobTypeBackup           = "backup"
	JobTypeCleanup          = "cleanup"
)

// ManualJobTypes 是允许管理端手动投递的任务类型（§5 `POST /jobs`）。
var ManualJobTypes = []string{JobTypeCleanup, JobTypeBackup, JobTypeLinkCheck}

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

// JobListFilter 是管理端任务列表的筛选条件（§5）。
type JobListFilter struct {
	Status string
	Limit  int
	Offset int
}

// ListJobs 返回任务分页，按 run_at 倒序（新任务在前）。
func ListJobs(db *gorm.DB, f JobListFilter) ([]Job, int64, error) {
	query := func() *gorm.DB {
		q := db.Model(&Job{})
		if f.Status != "" {
			q = q.Where("status = ?", f.Status)
		}
		return q
	}

	var total int64
	if err := query().Count(&total).Error; err != nil {
		return nil, 0, err
	}

	jobs := make([]Job, 0)
	q := query().Order("run_at DESC, id DESC")
	if f.Limit > 0 {
		q = q.Limit(f.Limit)
	}
	if f.Offset > 0 {
		q = q.Offset(f.Offset)
	}
	if err := q.Find(&jobs).Error; err != nil {
		return nil, 0, err
	}
	return jobs, total, nil
}

// FindJobByID 按主键查询任务；不存在时返回 ErrNotFound。
func FindJobByID(db *gorm.DB, id int64) (*Job, error) {
	var job Job
	if err := db.Where("id = ?", id).Take(&job).Error; err != nil {
		return nil, wrapErr(err)
	}
	return &job, nil
}

// RequeueJob 把失败任务立刻放回 pending，并把尝试次数清零（手动重试不继承退避）。
func RequeueJob(db *gorm.DB, id int64, runAt dbtime.Unix) error {
	return db.Model(&Job{}).Where("id = ?", id).
		Updates(map[string]any{
			"status":     JobStatusPending,
			"run_at":     runAt,
			"attempts":   0,
			"last_error": "",
			"locked_at":  nil,
		}).Error
}

// CountJobsOfTypeSince 统计某类型在 since（含）之后创建的任务数；
// 调度器用它判断"当天该跑的周期任务是否已经跑过"（§13）。
func CountJobsOfTypeSince(db *gorm.DB, jobType string, since dbtime.Unix) (int64, error) {
	var n int64
	err := db.Model(&Job{}).
		Where("type = ? AND created_at >= ?", jobType, int64(since)).
		Count(&n).Error
	return n, err
}

// DeletePendingJobs 删除同类型、同 payload 的待执行任务（取消定时发布时用）。
//
// payload 是我们自己序列化出来的规范 JSON（`{"postId":3}`），所以可以精确比对。
func DeletePendingJobs(db *gorm.DB, jobType, payload string) (int64, error) {
	result := db.Where("type = ? AND status = ? AND payload = ?", jobType, JobStatusPending, payload).
		Delete(&Job{})
	return result.RowsAffected, result.Error
}

// DeleteJobsByPost 删除某文章相关的任务（物理删除文章时用）。
func DeleteJobsByPost(db *gorm.DB, postID int64, payload string) error {
	return db.Where("payload = ?", payload).Delete(&Job{}).Error
}
