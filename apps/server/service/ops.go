package service

import (
	"errors"
	"fmt"
	"slices"

	"github.com/unicornfairy864/blog-zjut-si/apps/server/global"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/model"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/model/common/dbtime"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/model/common/errs"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/task"
)

// OpsService 处理任务队列与审计日志（agent.md §5「任务与审计」）。
type OpsService struct{}

// Jobs 返回任务分页；status 为空表示全部（§5）。
func (s OpsService) Jobs(status string, page, pageSize int) ([]model.Job, int64, error) {
	if status != "" && !validJobStatus(status) {
		return nil, 0, errs.New(errs.ErrorValidation, "status 取值非法")
	}
	jobs, total, err := model.ListJobs(global.DB, model.JobListFilter{
		Status: status,
		Limit:  pageSize,
		Offset: (page - 1) * pageSize,
	})
	if err != nil {
		return nil, 0, fmt.Errorf("查询任务失败: %w", err)
	}
	return jobs, total, nil
}

// EnqueueJob 手动投递周期任务（§5：只允许 cleanup / backup / link_check）。
func (s OpsService) EnqueueJob(jobType string, actor PostActor) (*model.Job, error) {
	if !slices.Contains(model.ManualJobTypes, jobType) {
		return nil, errs.New(errs.ErrorValidation, "只允许手动投递 cleanup / backup / link_check")
	}

	now := dbtime.Now()
	job := &model.Job{
		Type:        jobType,
		Payload:     "{}",
		RunAt:       now,
		Status:      model.JobStatusPending,
		MaxAttempts: model.DefaultJobMaxAttempts,
		CreatedAt:   now,
	}
	if err := model.CreateJob(global.DB, job); err != nil {
		return nil, fmt.Errorf("投递任务失败: %w", err)
	}

	ServiceGroupApp.Audit.Write(AuditInput{
		AdminID:    actor.AdminID,
		Action:     AuditActionJobEnqueue,
		TargetType: "job",
		TargetID:   fmt.Sprintf("%d", job.ID),
		Meta:       map[string]any{"type": jobType},
		IP:         actor.IP,
	})
	// 立刻唤醒调度器：手动投递的意义就是"现在就跑"。
	task.Notify()
	return job, nil
}

// RetryJob 把失败任务放回待执行并立刻唤醒调度器（§5）。
func (s OpsService) RetryJob(id int64, actor PostActor) (*model.Job, error) {
	job, err := model.FindJobByID(global.DB, id)
	if err != nil {
		if errors.Is(err, model.ErrNotFound) {
			return nil, errs.New(errs.ErrorNotFound, "任务不存在")
		}
		return nil, fmt.Errorf("查询任务失败: %w", err)
	}
	if job.Status != model.JobStatusFailed {
		return nil, errs.New(errs.ErrorConflict, "只有失败的任务可以重试")
	}

	if err := model.RequeueJob(global.DB, job.ID, dbtime.Now()); err != nil {
		return nil, fmt.Errorf("重试任务失败: %w", err)
	}
	ServiceGroupApp.Audit.Write(AuditInput{
		AdminID:    actor.AdminID,
		Action:     AuditActionJobRetry,
		TargetType: "job",
		TargetID:   fmt.Sprintf("%d", job.ID),
		Meta:       map[string]any{"type": job.Type},
		IP:         actor.IP,
	})

	task.Notify()
	return model.FindJobByID(global.DB, job.ID)
}

// AuditLogs 返回审计分页；筛选条件全部可选（§5）。
func (s OpsService) AuditLogs(filter model.AuditLogFilter, page, pageSize int) ([]model.AuditLog, int64, error) {
	filter.Limit = pageSize
	filter.Offset = (page - 1) * pageSize

	logs, total, err := model.ListAuditLogs(global.DB, filter)
	if err != nil {
		return nil, 0, fmt.Errorf("查询审计日志失败: %w", err)
	}
	return logs, total, nil
}

// validJobStatus 校验 jobs.status 取值（§4）。
func validJobStatus(status string) bool {
	switch status {
	case model.JobStatusPending, model.JobStatusRunning, model.JobStatusDone, model.JobStatusFailed:
		return true
	default:
		return false
	}
}
