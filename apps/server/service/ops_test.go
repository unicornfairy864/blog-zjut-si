package service

// 本文件覆盖 service/ops.go（任务代号 D18）：任务队列的管理端入口与审计分页。
//
// 与 publishing_test.go 共用 requireAppError 等辅助函数；用例同样串行（global.DB 是单例）。

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/unicornfairy864/blog-zjut-si/apps/server/global"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/model"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/model/common/dbtime"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/model/common/errs"
)

func TestOpsService_EnqueueJobAllowsManualTypes(t *testing.T) {
	setupEnv(t)
	actor := testActor(t)

	for _, jobType := range model.ManualJobTypes {
		t.Run(jobType, func(t *testing.T) {
			job, err := ServiceGroupApp.Ops.EnqueueJob(jobType, actor)
			require.NoError(t, err)
			require.NotZero(t, job.ID)
			assert.Equal(t, jobType, job.Type)
			assert.Equal(t, "{}", job.Payload)
			assert.Equal(t, model.JobStatusPending, job.Status)
			assert.Equal(t, model.DefaultJobMaxAttempts, job.MaxAttempts)
			// 手动投递的意义是"现在就跑"：run_at 落在当前时刻。
			assert.False(t, job.RunAt.Before(dbtime.Now().Add(-time.Minute)))

			stored, err := model.FindJobByID(global.DB, job.ID)
			require.NoError(t, err)
			assert.Equal(t, model.JobStatusPending, stored.Status)
			assert.Equal(t, job.RunAt, stored.RunAt)
		})
	}

	// 白名单里每个类型一条，且都写了审计。
	assert.Equal(t, int64(len(model.ManualJobTypes)), countRows(t, "jobs", ""))
	assert.Equal(t, int64(len(model.ManualJobTypes)),
		countRows(t, "audit_logs", "action = ?", AuditActionJobEnqueue))
}

func TestOpsService_EnqueueJobRejectsNonManualType(t *testing.T) {
	setupEnv(t)
	actor := testActor(t)

	for _, jobType := range []string{
		model.JobTypePublishScheduled,
		model.JobTypeMediaProcess,
		"unknown",
		"",
	} {
		_, err := ServiceGroupApp.Ops.EnqueueJob(jobType, actor)
		appErr := appErrorOf(t, err, errs.ErrorValidation)
		// 错误消息要列出允许的类型，便于调用方自查。
		for _, allowed := range model.ManualJobTypes {
			assert.Contains(t, appErr.Message, allowed)
		}
	}

	// 非法类型必须不落库、不写审计。
	assert.Equal(t, int64(0), countRows(t, "jobs", ""))
	assert.Equal(t, int64(0), countRows(t, "audit_logs", "action = ?", AuditActionJobEnqueue))
}

func TestOpsService_JobsFiltersAndPaginates(t *testing.T) {
	setupEnv(t)
	now := dbtime.Now()

	// 5 条任务：3 pending + 2 failed，run_at 依次递增，便于断言倒序。
	statuses := []string{
		model.JobStatusPending,
		model.JobStatusPending,
		model.JobStatusFailed,
		model.JobStatusPending,
		model.JobStatusFailed,
	}
	for i, status := range statuses {
		require.NoError(t, model.CreateJob(global.DB, &model.Job{
			Type:        model.JobTypeCleanup,
			Payload:     "{}",
			RunAt:       now.Add(time.Duration(i) * time.Minute),
			Status:      status,
			MaxAttempts: model.DefaultJobMaxAttempts,
			CreatedAt:   now,
		}))
	}

	ops := OpsService{}

	// 空 status = 全部；排序按 run_at 倒序（新任务在前）。
	all, total, err := ops.Jobs("", 1, 10)
	require.NoError(t, err)
	assert.Equal(t, int64(5), total)
	require.Len(t, all, 5)
	assert.True(t, all[0].RunAt >= all[1].RunAt)

	// status 过滤。
	failed, total, err := ops.Jobs(model.JobStatusFailed, 1, 10)
	require.NoError(t, err)
	assert.Equal(t, int64(2), total)
	require.Len(t, failed, 2)
	for _, job := range failed {
		assert.Equal(t, model.JobStatusFailed, job.Status)
	}

	// 分页：total 是过滤后的总数，与页大小无关；第二页接着第一页。
	page2, total, err := ops.Jobs("", 2, 2)
	require.NoError(t, err)
	assert.Equal(t, int64(5), total)
	require.Len(t, page2, 2)
	assert.Less(t, page2[0].RunAt, all[0].RunAt)

	// 没有匹配项时返回空页（不是错误）。
	done, total, err := ops.Jobs(model.JobStatusDone, 1, 10)
	require.NoError(t, err)
	assert.Equal(t, int64(0), total)
	assert.Empty(t, done)

	// 非法 status → 1003（不静默返回全量）。
	_, _, err = ops.Jobs("paused", 1, 10)
	requireAppError(t, err, errs.ErrorValidation)
}

func TestOpsService_RetryJobRequeuesOnlyFailedJobs(t *testing.T) {
	setupEnv(t)
	actor := testActor(t)
	now := dbtime.Now()

	failed := &model.Job{
		Type:        model.JobTypeBackup,
		Payload:     "{}",
		RunAt:       now.Add(-time.Hour),
		Status:      model.JobStatusFailed,
		Attempts:    3,
		MaxAttempts: model.DefaultJobMaxAttempts,
		LastError:   "磁盘满了",
		CreatedAt:   now.Add(-2 * time.Hour),
	}
	require.NoError(t, model.CreateJob(global.DB, failed))

	got, err := ServiceGroupApp.Ops.RetryJob(failed.ID, actor)
	require.NoError(t, err)
	assert.Equal(t, model.JobStatusPending, got.Status)
	// 手动重试不继承退避：尝试次数清零、错误清空、立刻可跑。
	assert.Zero(t, got.Attempts)
	assert.Empty(t, got.LastError)
	assert.Nil(t, got.LockedAt)
	assert.False(t, got.RunAt.Before(now))
	assert.Equal(t, int64(1), countRows(t, "audit_logs", "action = ?", AuditActionJobRetry))

	// 只有 failed 能重试：其余状态都是 1004。
	for _, status := range []string{model.JobStatusPending, model.JobStatusRunning, model.JobStatusDone} {
		job := &model.Job{
			Type: model.JobTypeCleanup, Payload: "{}", RunAt: now, Status: status,
			MaxAttempts: model.DefaultJobMaxAttempts, CreatedAt: now,
		}
		require.NoError(t, model.CreateJob(global.DB, job))
		_, err := ServiceGroupApp.Ops.RetryJob(job.ID, actor)
		requireAppError(t, err, errs.ErrorConflict)
	}

	// 任务不存在 → 1002。
	_, err = ServiceGroupApp.Ops.RetryJob(987654, actor)
	requireAppError(t, err, errs.ErrorNotFound)

	// 只有那一次成功重试写了审计。
	assert.Equal(t, int64(1), countRows(t, "audit_logs", "action = ?", AuditActionJobRetry))
}

func TestOpsService_AuditLogsFiltersAndPaginates(t *testing.T) {
	setupEnv(t)
	actor := testActor(t)

	// 直接经审计服务写 5 条，覆盖 action / targetType / targetId 三个过滤维度。
	seed := []AuditInput{
		{AdminID: actor.AdminID, Action: AuditActionPostPublish, TargetType: "post", TargetID: "1", IP: actor.IP},
		{AdminID: actor.AdminID, Action: AuditActionPostPublish, TargetType: "post", TargetID: "2", IP: actor.IP},
		{AdminID: actor.AdminID, Action: AuditActionPostSchedule, TargetType: "post", TargetID: "1", IP: actor.IP},
		{AdminID: actor.AdminID, Action: AuditActionPreviewCreate, TargetType: "preview", TargetID: "abc", IP: actor.IP},
		{AdminID: actor.AdminID, Action: AuditActionJobEnqueue, TargetType: "job", TargetID: "7", IP: actor.IP},
	}
	for _, in := range seed {
		ServiceGroupApp.Audit.Write(in)
	}

	ops := OpsService{}

	all, total, err := ops.AuditLogs(model.AuditLogFilter{}, 1, 10)
	require.NoError(t, err)
	assert.Equal(t, int64(5), total)
	assert.Len(t, all, 5)

	// 按 action 过滤。
	logs, total, err := ops.AuditLogs(model.AuditLogFilter{Action: AuditActionPostPublish}, 1, 10)
	require.NoError(t, err)
	assert.Equal(t, int64(2), total)
	assert.Len(t, logs, 2)

	// targetType + targetId 组合过滤。
	logs, total, err = ops.AuditLogs(model.AuditLogFilter{TargetType: "post", TargetID: "1"}, 1, 10)
	require.NoError(t, err)
	assert.Equal(t, int64(2), total)
	assert.Len(t, logs, 2)
	for _, entry := range logs {
		assert.Equal(t, "post", entry.TargetType)
		assert.Equal(t, "1", entry.TargetID)
	}

	// 分页：total 是全量，与页大小无关；两页不重叠。
	page1, total, err := ops.AuditLogs(model.AuditLogFilter{}, 1, 2)
	require.NoError(t, err)
	assert.Equal(t, int64(5), total)
	require.Len(t, page1, 2)
	page2, total, err := ops.AuditLogs(model.AuditLogFilter{}, 2, 2)
	require.NoError(t, err)
	assert.Equal(t, int64(5), total)
	require.Len(t, page2, 2)
	assert.NotEqual(t, page1[0].ID, page2[0].ID)

	// 过滤条件不匹配时返回空页（不是错误）。
	logs, total, err = ops.AuditLogs(model.AuditLogFilter{Action: "not.exists"}, 1, 10)
	require.NoError(t, err)
	assert.Equal(t, int64(0), total)
	assert.Empty(t, logs)
}
