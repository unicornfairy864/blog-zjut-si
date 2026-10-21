package service

// 本文件覆盖 service/publishing.go 与 service/task.go（任务代号 D18）。
//
// 用例串行执行：global.DB / global.Config / global.Log 是进程级单例，
// 每个用例用 setupEnv(t) 换一套临时库（夹具见 main_test.go），所以这里不写 t.Parallel()。

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/unicornfairy864/blog-zjut-si/apps/server/global"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/model"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/model/common/dbtime"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/model/common/errs"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/utils"
)

// requireAppError 断言 err 是业务错误且 code 符合预期，返回 AppError 供进一步断言消息。
//
// 为什么不用 assert.ErrorIs：被测代码把非业务错误统一包成内部错误，
// 只有拿到 AppError 才能区分"业务拒绝"与"内部失败"。
// requireAppError / appErrorOf 定义在 main_test.go（多个测试文件共用）。

// reloadPost 从库里重读文章（含回收站记录），用于断言副作用落库后的真实状态。
func reloadPost(t *testing.T, id int64) *model.Post {
	t.Helper()

	post, err := model.FindPostByID(global.DB, id, true)
	require.NoError(t, err)
	return post
}

// markScheduled 直接把文章摆成"已排期 + 到点时间"，用于任务入口的用例。
func markScheduled(t *testing.T, id int64, at dbtime.Unix) {
	t.Helper()

	require.NoError(t, model.UpdatePostFields(global.DB, id, map[string]any{
		"status":       model.PostStatusScheduled,
		"published_at": at,
	}))
}

// ---------------------------------------------------------------------------
// PostService.Schedule
// ---------------------------------------------------------------------------

func TestPostService_ScheduleCreatesJobAndMarksScheduled(t *testing.T) {
	setupEnv(t)
	actor := testActor(t)
	post := insertPost(t, "定时发布", "sched-future", model.PostStatusDraft)

	at := dbtime.Now().Add(2 * time.Hour)
	got, err := ServiceGroupApp.Post.Schedule(post.ID, at, "  下周见  ", actor)
	require.NoError(t, err)

	// 文章落 scheduled，published_at 就是排定时间（不是"现在"）。
	assert.Equal(t, model.PostStatusScheduled, got.Status)
	require.NotNil(t, got.PublishedAt)
	assert.Equal(t, at, *got.PublishedAt)

	// 投递 publish_scheduled：payload 是规范 JSON，run_at = 排定时间，待执行状态。
	require.Equal(t, int64(1), countRows(t, "jobs", "type = ? AND status = ? AND payload = ?",
		model.JobTypePublishScheduled, model.JobStatusPending, scheduledJobPayload(post.ID)))
	var job model.Job
	require.NoError(t, global.DB.Where("type = ?", model.JobTypePublishScheduled).Take(&job).Error)
	assert.Equal(t, at, job.RunAt)
	assert.Equal(t, model.DefaultJobMaxAttempts, job.MaxAttempts)

	// 审计：动作 + note 去空白 + publishedAt 落 meta。
	var entry model.AuditLog
	require.NoError(t, global.DB.Where("action = ?", AuditActionPostSchedule).Take(&entry).Error)
	assert.Equal(t, "post", entry.TargetType)
	assert.Equal(t, strconv.FormatInt(post.ID, 10), entry.TargetID)
	assert.Contains(t, entry.MetaJSON, `"note":"下周见"`)
	assert.Contains(t, entry.MetaJSON, strconv.FormatInt(int64(at), 10))
}

func TestPostService_ScheduleRejectsNonFutureTime(t *testing.T) {
	setupEnv(t)
	actor := testActor(t)
	post := insertPost(t, "过去时间", "sched-past", model.PostStatusDraft)

	// 过去时间与零值（Unix 0 = 1970）都不是未来时间。
	for name, at := range map[string]dbtime.Unix{
		"过去时间": dbtime.Now().Add(-time.Minute),
		"零值时间": 0,
	} {
		t.Run(name, func(t *testing.T) {
			_, err := ServiceGroupApp.Post.Schedule(post.ID, at, "", actor)
			appErr := appErrorOf(t, err, errs.ErrorValidation)
			assert.Contains(t, appErr.Message, "未来时间")
		})
	}

	// 校验在事务外短路：不落任务、不改状态、不写审计。
	assert.Equal(t, int64(0), countRows(t, "jobs", "type = ?", model.JobTypePublishScheduled))
	assert.Equal(t, model.PostStatusDraft, reloadPost(t, post.ID).Status)
	assert.Equal(t, int64(0), countRows(t, "audit_logs", "action = ?", AuditActionPostSchedule))
}

func TestPostService_ScheduleRejectsArchivedPost(t *testing.T) {
	setupEnv(t)
	actor := testActor(t)
	post := insertPost(t, "已归档", "sched-archived", model.PostStatusArchived)

	_, err := ServiceGroupApp.Post.Schedule(post.ID, dbtime.Now().Add(time.Hour), "", actor)
	requireAppError(t, err, errs.ErrorConflict)

	assert.Equal(t, int64(0), countRows(t, "jobs", "type = ?", model.JobTypePublishScheduled))
	assert.Equal(t, model.PostStatusArchived, reloadPost(t, post.ID).Status)
}

func TestPostService_ScheduleOnPublishedPostRejected(t *testing.T) {
	// §6 的状态机只有 draft ──schedule──▶ scheduled：已发布文章必须拒绝（1004）。
	// 早先只拦 archived，已发布文章会被静默改成 scheduled，公开端随即查不到它，
	// published_at 也被改写成未来时间——等于"排期即下线"（D18 修复）。
	setupEnv(t)
	actor := testActor(t)
	post := insertPost(t, "已发布", "sched-published", model.PostStatusPublished)

	at := dbtime.Now().Add(time.Hour)
	_, err := ServiceGroupApp.Post.Schedule(post.ID, at, "", actor)
	requireAppError(t, err, errs.ErrorConflict)

	// 文章状态与发布时间原样不动，也没有留下待执行任务。
	reloaded := reloadPost(t, post.ID)
	assert.Equal(t, model.PostStatusPublished, reloaded.Status)
	require.NotNil(t, reloaded.PublishedAt)
	assert.Equal(t, *post.PublishedAt, *reloaded.PublishedAt)
	assert.Equal(t, int64(0), countRows(t, "jobs", "type = ?", model.JobTypePublishScheduled))

	// 先取消发布就能定时。
	_, err = ServiceGroupApp.Post.Unpublish(post.ID, actor)
	require.NoError(t, err)
	got, err := ServiceGroupApp.Post.Schedule(post.ID, at, "", actor)
	require.NoError(t, err)
	assert.Equal(t, model.PostStatusScheduled, got.Status)
	assert.Equal(t, int64(1), countRows(t, "jobs", "type = ?", model.JobTypePublishScheduled))
}

func TestPostService_ScheduleOnMissingPostReturnsNotFound(t *testing.T) {
	// 文章不存在 → 1002（D18 修复）：此前被包成普通错误，API 层归一成 1006 内部错误。
	setupEnv(t)
	actor := testActor(t)

	_, err := ServiceGroupApp.Post.Schedule(987654, dbtime.Now().Add(time.Hour), "", actor)
	requireAppError(t, err, errs.ErrorNotFound)
	assert.Equal(t, int64(0), countRows(t, "jobs", "type = ?", model.JobTypePublishScheduled))
}

func TestPostService_ScheduleTwiceReplacesPendingJob(t *testing.T) {
	setupEnv(t)
	actor := testActor(t)
	post := insertPost(t, "重复排期", "sched-twice", model.PostStatusDraft)

	first := dbtime.Now().Add(time.Hour)
	_, err := ServiceGroupApp.Post.Schedule(post.ID, first, "", actor)
	require.NoError(t, err)

	second := dbtime.Now().Add(3 * time.Hour)
	got, err := ServiceGroupApp.Post.Schedule(post.ID, second, "", actor)
	require.NoError(t, err)

	// 重复调用先撤旧任务再投新任务：只留一条 pending，且指向新时间。
	assert.Equal(t, second, *got.PublishedAt)
	require.Equal(t, int64(1), countRows(t, "jobs", "type = ? AND status = ?",
		model.JobTypePublishScheduled, model.JobStatusPending))
	var job model.Job
	require.NoError(t, global.DB.Where("type = ?", model.JobTypePublishScheduled).Take(&job).Error)
	assert.Equal(t, second, job.RunAt)

	// 只有 pending 会被撤掉：已经 running/done 的旧任务留在表里。
	// 这是"到点连发两次"的边界，实现靠 PublishScheduledPost 的状态复查兜住（见下个用例）。
	require.NoError(t, global.DB.Model(&model.Job{}).Where("id = ?", job.ID).
		Update("status", model.JobStatusDone).Error)

	third := dbtime.Now().Add(4 * time.Hour)
	_, err = ServiceGroupApp.Post.Schedule(post.ID, third, "", actor)
	require.NoError(t, err)
	assert.Equal(t, int64(2), countRows(t, "jobs", "type = ?", model.JobTypePublishScheduled))
	assert.Equal(t, int64(1), countRows(t, "jobs", "type = ? AND status = ?",
		model.JobTypePublishScheduled, model.JobStatusDone))
}

// ---------------------------------------------------------------------------
// PostService.PublishScheduledPost
// ---------------------------------------------------------------------------

func TestPostService_PublishScheduledPostPublishesDuePost(t *testing.T) {
	setupEnv(t)
	post := insertPost(t, "到点发布", "due-post", model.PostStatusDraft)
	at := dbtime.Now().Add(-time.Minute)
	markScheduled(t, post.ID, at)

	require.NoError(t, ServiceGroupApp.Post.PublishScheduledPost(post.ID))

	got := reloadPost(t, post.ID)
	assert.Equal(t, model.PostStatusPublished, got.Status)
	require.NotNil(t, got.PublishedAt)
	// 发布时刻取排定时间：多篇积压时仍保持原有先后。
	assert.Equal(t, at, *got.PublishedAt)
	assert.Equal(t, 2, got.Revision)
	assert.NotEmpty(t, got.ContentHTML)

	// 与手动发布同源：写一条 revision，note 是"定时发布"。
	var revision model.PostRevision
	require.NoError(t, global.DB.Where("post_id = ? AND revision_no = ?", post.ID, 2).Take(&revision).Error)
	assert.Equal(t, "定时发布", revision.Note)
	assert.Equal(t, got.ContentMD, revision.ContentMD)
	assert.Equal(t, got.ContentHTML, revision.ContentHTML)

	// 审计 post.publish，且 meta 标记 scheduled=true。
	var entry model.AuditLog
	require.NoError(t, global.DB.Where("action = ?", AuditActionPostPublish).Take(&entry).Error)
	assert.Contains(t, entry.MetaJSON, `"scheduled":true`)
}

func TestPostService_PublishScheduledPostIsIdempotent(t *testing.T) {
	setupEnv(t)

	// 文章已被手工发布：任务到点后只是跳过，不算失败，也不重复写版本。
	post := insertPost(t, "手工发布", "skip-published", model.PostStatusPublished)
	before := reloadPost(t, post.ID)

	require.NoError(t, ServiceGroupApp.Post.PublishScheduledPost(post.ID))
	require.NoError(t, ServiceGroupApp.Post.PublishScheduledPost(post.ID))

	after := reloadPost(t, post.ID)
	assert.Equal(t, model.PostStatusPublished, after.Status)
	assert.Equal(t, before.Revision, after.Revision)
	assert.Equal(t, before.PublishedAt, after.PublishedAt)
	assert.Equal(t, int64(0), countRows(t, "post_revisions", "post_id = ?", post.ID))
	assert.Equal(t, int64(0), countRows(t, "audit_logs", "action = ?", AuditActionPostPublish))
}

func TestPostService_PublishScheduledPostSkipsNonScheduledStates(t *testing.T) {
	setupEnv(t)

	cases := []struct {
		name   string
		status string
	}{
		{"已归档", model.PostStatusArchived},
		{"已取消回草稿", model.PostStatusDraft},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			post := insertPost(t, tc.name, fmt.Sprintf("skip-state-%d", i), model.PostStatusDraft)
			markScheduled(t, post.ID, dbtime.Now().Add(-time.Minute))
			// 用户中途改了状态：任务到点只跳过。
			require.NoError(t, model.UpdatePostFields(global.DB, post.ID, map[string]any{"status": tc.status}))

			require.NoError(t, ServiceGroupApp.Post.PublishScheduledPost(post.ID))

			assert.Equal(t, tc.status, reloadPost(t, post.ID).Status)
			assert.Equal(t, int64(0), countRows(t, "post_revisions", "post_id = ?", post.ID))
		})
	}
}

func TestPostService_PublishScheduledPostSkipsWhenNotDue(t *testing.T) {
	setupEnv(t)
	post := insertPost(t, "还没到点", "skip-future", model.PostStatusDraft)
	markScheduled(t, post.ID, dbtime.Now().Add(time.Hour))

	require.NoError(t, ServiceGroupApp.Post.PublishScheduledPost(post.ID))

	assert.Equal(t, model.PostStatusScheduled, reloadPost(t, post.ID).Status)
	assert.Equal(t, int64(0), countRows(t, "post_revisions", "post_id = ?", post.ID))
}

func TestPostService_PublishScheduledPostSkipsDeletedOrMissing(t *testing.T) {
	setupEnv(t)

	t.Run("文章不存在", func(t *testing.T) {
		// 被物理删除（回收站清理）后任务可能还在：跳过而不是报错。
		require.NoError(t, ServiceGroupApp.Post.PublishScheduledPost(987654))
	})

	t.Run("文章在回收站", func(t *testing.T) {
		post := insertPost(t, "回收站", "skip-deleted", model.PostStatusDraft)
		markScheduled(t, post.ID, dbtime.Now().Add(-time.Minute))
		require.NoError(t, global.DB.Exec("UPDATE posts SET deleted_at = ? WHERE id = ?",
			int64(dbtime.Now()), post.ID).Error)

		require.NoError(t, ServiceGroupApp.Post.PublishScheduledPost(post.ID))

		assert.Equal(t, model.PostStatusScheduled, reloadPost(t, post.ID).Status)
		assert.Equal(t, int64(0), countRows(t, "post_revisions", "post_id = ?", post.ID))
	})
}

func TestPublishing_CancelScheduledJobsRemovesPendingJob(t *testing.T) {
	setupEnv(t)
	actor := testActor(t)
	post := insertPost(t, "取消定时", "cancel-sched", model.PostStatusDraft)

	_, err := ServiceGroupApp.Post.Schedule(post.ID, dbtime.Now().Add(time.Hour), "", actor)
	require.NoError(t, err)

	// 撤任务按 payload 精确匹配。
	require.NoError(t, cancelScheduledJobs(global.DB, post.ID))
	assert.Equal(t, int64(0), countRows(t, "jobs", "type = ?", model.JobTypePublishScheduled))

	// 幂等：没有待执行任务时也不报错。
	require.NoError(t, cancelScheduledJobs(global.DB, post.ID))

	// 只撤该文的任务：别的文章的排期不受影响。
	other := insertPost(t, "别人的定时", "cancel-sched-other", model.PostStatusDraft)
	_, err = ServiceGroupApp.Post.Schedule(other.ID, dbtime.Now().Add(time.Hour), "", actor)
	require.NoError(t, err)

	require.NoError(t, cancelScheduledJobs(global.DB, post.ID))
	assert.Equal(t, int64(1), countRows(t, "jobs", "type = ?", model.JobTypePublishScheduled))
	assert.Equal(t, int64(1), countRows(t, "jobs", "type = ? AND payload = ?",
		model.JobTypePublishScheduled, scheduledJobPayload(other.ID)))
}

// ---------------------------------------------------------------------------
// PostService 预览链接
// ---------------------------------------------------------------------------
func TestPostService_CreatePreviewLinkAndConsumeOnce(t *testing.T) {
	setupEnv(t)
	actor := testActor(t)
	post := insertPost(t, "预览草稿", "preview-once", model.PostStatusDraft)

	before := dbtime.Now()
	link, err := ServiceGroupApp.Post.CreatePreviewLink(post.ID, actor)
	require.NoError(t, err)

	assert.Len(t, link.Token, previewTokenBytes*2)
	assert.Equal(t, "/preview/"+link.Token, link.URL)
	// 24h 有效期（容忍 2s 的执行误差）。
	assert.InDelta(t, int64(before.Add(previewTokenTTL)), int64(link.ExpiresAt), 2)

	// 库里只存 hash：明文只在返回值里出现这一次。
	assert.Equal(t, int64(1), countRows(t, "preview_tokens", "post_id = ?", post.ID))
	assert.Equal(t, int64(1), countRows(t, "preview_tokens", "token_hash = ?", utils.SHA256Hex(link.Token)))
	assert.Equal(t, int64(0), countRows(t, "preview_tokens", "token_hash = ?", link.Token))
	assert.Equal(t, int64(1), countRows(t, "audit_logs", "action = ?", AuditActionPreviewCreate))

	preview, err := ServiceGroupApp.Post.PreviewByToken(link.Token)
	require.NoError(t, err)
	assert.Equal(t, post.Slug, preview.Slug)
	assert.Equal(t, post.Title, preview.Title)
	assert.Equal(t, model.PostStatusDraft, preview.Status)
	assert.Equal(t, post.ContentHTML, preview.ContentHTML)
	assert.Equal(t, post.Excerpt, preview.Excerpt)
	assert.Equal(t, link.ExpiresAt, preview.ExpiresAt)

	// 首次访问写 used_at。
	assert.Equal(t, int64(1), countRows(t, "preview_tokens",
		"token_hash = ? AND used_at IS NOT NULL", utils.SHA256Hex(link.Token)))

	// 第二次访问：一次性链接已用掉 → 2006。
	_, err = ServiceGroupApp.Post.PreviewByToken(link.Token)
	appErr := appErrorOf(t, err, errs.ErrorPreviewExpired)
	assert.Contains(t, appErr.Message, "已使用")
}

func TestPostService_CreatePreviewLinkInvalidatesPreviousUnusedToken(t *testing.T) {
	setupEnv(t)
	actor := testActor(t)
	post := insertPost(t, "重新生成", "preview-regen", model.PostStatusDraft)

	first, err := ServiceGroupApp.Post.CreatePreviewLink(post.ID, actor)
	require.NoError(t, err)
	second, err := ServiceGroupApp.Post.CreatePreviewLink(post.ID, actor)
	require.NoError(t, err)

	assert.NotEqual(t, first.Token, second.Token)
	// 一次性链接不该同时存在多把钥匙：旧链接被撤掉。
	assert.Equal(t, int64(1), countRows(t, "preview_tokens", "post_id = ?", post.ID))

	_, err = ServiceGroupApp.Post.PreviewByToken(first.Token)
	requireAppError(t, err, errs.ErrorNotFound)

	_, err = ServiceGroupApp.Post.PreviewByToken(second.Token)
	require.NoError(t, err)
}

func TestPostService_PreviewByTokenRejectsExpired(t *testing.T) {
	setupEnv(t)
	actor := testActor(t)
	post := insertPost(t, "过期预览", "preview-expired", model.PostStatusDraft)

	link, err := ServiceGroupApp.Post.CreatePreviewLink(post.ID, actor)
	require.NoError(t, err)

	// 24h 边界不方便等，直接把 expires_at 拨到过去。
	require.NoError(t, global.DB.Exec("UPDATE preview_tokens SET expires_at = ? WHERE token_hash = ?",
		int64(dbtime.Now().Add(-time.Second)), utils.SHA256Hex(link.Token)).Error)

	_, err = ServiceGroupApp.Post.PreviewByToken(link.Token)
	appErr := appErrorOf(t, err, errs.ErrorPreviewExpired)
	assert.Contains(t, appErr.Message, "过期")

	// 失败的访问不能把令牌标成已用（事务回滚）。
	assert.Equal(t, int64(0), countRows(t, "preview_tokens",
		"token_hash = ? AND used_at IS NOT NULL", utils.SHA256Hex(link.Token)))
}

func TestPostService_PreviewByTokenRejectsDeletedPost(t *testing.T) {
	setupEnv(t)
	actor := testActor(t)
	post := insertPost(t, "文章被删", "preview-deleted", model.PostStatusDraft)

	link, err := ServiceGroupApp.Post.CreatePreviewLink(post.ID, actor)
	require.NoError(t, err)

	require.NoError(t, global.DB.Exec("UPDATE posts SET deleted_at = ? WHERE id = ?",
		int64(dbtime.Now()), post.ID).Error)

	_, err = ServiceGroupApp.Post.PreviewByToken(link.Token)
	requireAppError(t, err, errs.ErrorGone)
	// 事务回滚：没有消耗掉令牌。
	assert.Equal(t, int64(0), countRows(t, "preview_tokens",
		"token_hash = ? AND used_at IS NOT NULL", utils.SHA256Hex(link.Token)))
}

func TestPostService_PreviewByTokenRejectsUnknownAndMalformedToken(t *testing.T) {
	setupEnv(t)

	// 形状合法但库里没有 → 1002。
	unknown := strings.Repeat("ab", previewTokenBytes)
	_, err := ServiceGroupApp.Post.PreviewByToken(unknown)
	requireAppError(t, err, errs.ErrorNotFound)

	// 形状非法（长度/字符集）在查库前就被拒 → 1002。
	for name, token := range map[string]string{
		"太短":     "abc",
		"太长":     strings.Repeat("a", previewTokenBytes*2+1),
		"大写十六进制": strings.Repeat("A", previewTokenBytes*2),
		"非十六进制":  strings.Repeat("z", previewTokenBytes*2),
		"空串":     "",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := ServiceGroupApp.Post.PreviewByToken(token)
			requireAppError(t, err, errs.ErrorNotFound)
		})
	}
}

func TestPostService_CreatePreviewLinkRejectsMissingOrDeletedPost(t *testing.T) {
	setupEnv(t)
	actor := testActor(t)

	_, err := ServiceGroupApp.Post.CreatePreviewLink(987654, actor)
	requireAppError(t, err, errs.ErrorNotFound)

	post := insertPost(t, "已删除", "preview-link-deleted", model.PostStatusDraft)
	require.NoError(t, global.DB.Exec("UPDATE posts SET deleted_at = ? WHERE id = ?",
		int64(dbtime.Now()), post.ID).Error)

	_, err = ServiceGroupApp.Post.CreatePreviewLink(post.ID, actor)
	requireAppError(t, err, errs.ErrorGone)
	assert.Equal(t, int64(0), countRows(t, "preview_tokens", "post_id = ?", post.ID))
}

func TestPostService_DeletePreviewLink(t *testing.T) {
	setupEnv(t)
	actor := testActor(t)
	post := insertPost(t, "作废链接", "preview-delete", model.PostStatusDraft)

	link, err := ServiceGroupApp.Post.CreatePreviewLink(post.ID, actor)
	require.NoError(t, err)

	require.NoError(t, ServiceGroupApp.Post.DeletePreviewLink(link.Token, actor))
	assert.Equal(t, int64(0), countRows(t, "preview_tokens", "token_hash = ?", utils.SHA256Hex(link.Token)))

	// 审计只记 token 前 8 位，不落完整明文。
	var entry model.AuditLog
	require.NoError(t, global.DB.Where("action = ?", AuditActionPreviewDelete).Take(&entry).Error)
	assert.Equal(t, "preview", entry.TargetType)
	assert.Equal(t, link.Token[:8], entry.TargetID)

	// 再删一次 / 形状非法 → 1002。
	requireAppError(t, ServiceGroupApp.Post.DeletePreviewLink(link.Token, actor), errs.ErrorNotFound)
	requireAppError(t, ServiceGroupApp.Post.DeletePreviewLink("not-a-token", actor), errs.ErrorNotFound)

	// 作废之后不能再预览。
	_, err = ServiceGroupApp.Post.PreviewByToken(link.Token)
	requireAppError(t, err, errs.ErrorNotFound)
}

// ---------------------------------------------------------------------------
// PostService.RestoreRevision
// ---------------------------------------------------------------------------

func TestPostService_RestoreRevisionWritesNewRevision(t *testing.T) {
	setupEnv(t)
	actor := testActor(t)
	post := insertPost(t, "当前标题", "restore-post", model.PostStatusPublished)

	// insertPost 只写 posts，不写 post_revisions；这里造一条历史版本。
	old := &model.PostRevision{
		PostID:      post.ID,
		RevisionNo:  1,
		Title:       "旧标题",
		ContentMD:   "# 旧内容\n\n第一版正文。",
		ContentHTML: "<h1>旧内容</h1>",
		Note:        "初始版本",
		CreatedAt:   dbtime.Now().Add(-time.Hour),
	}
	require.NoError(t, model.CreatePostRevision(global.DB, old))

	got, err := ServiceGroupApp.Post.RestoreRevision(post.ID, old.ID, "  回到第一版  ", actor)
	require.NoError(t, err)

	assert.Equal(t, "旧标题", got.Title)
	assert.Equal(t, "# 旧内容\n\n第一版正文。", got.ContentMD)
	assert.Equal(t, 2, got.Revision)
	assert.NotEmpty(t, got.ContentHTML)

	// 历史不被改写：老版本原样保留，回滚是"再写一条新版本"。
	require.Equal(t, int64(2), countRows(t, "post_revisions", "post_id = ?", post.ID))
	var kept model.PostRevision
	require.NoError(t, global.DB.Where("post_id = ? AND revision_no = ?", post.ID, 1).Take(&kept).Error)
	assert.Equal(t, "旧标题", kept.Title)
	assert.Equal(t, "初始版本", kept.Note)

	var added model.PostRevision
	require.NoError(t, global.DB.Where("post_id = ? AND revision_no = ?", post.ID, 2).Take(&added).Error)
	assert.Equal(t, "回到第一版", added.Note) // note 去空白
	assert.Equal(t, old.Title, added.Title)
	assert.Equal(t, old.ContentMD, added.ContentMD)
	assert.Equal(t, got.ContentHTML, added.ContentHTML)

	// 审计记 from/to 版本号。
	var entry model.AuditLog
	require.NoError(t, global.DB.Where("action = ?", AuditActionPostRestore).Take(&entry).Error)
	assert.Contains(t, entry.MetaJSON, `"from":1`)
	assert.Contains(t, entry.MetaJSON, `"to":2`)
}

func TestPostService_RestoreRevisionUsesDefaultNote(t *testing.T) {
	setupEnv(t)
	actor := testActor(t)
	post := insertPost(t, "默认备注", "restore-default-note", model.PostStatusDraft)

	old := &model.PostRevision{
		PostID: post.ID, RevisionNo: 1, Title: post.Title,
		ContentMD: "旧正文", ContentHTML: "旧正文", CreatedAt: dbtime.Now(),
	}
	require.NoError(t, model.CreatePostRevision(global.DB, old))

	_, err := ServiceGroupApp.Post.RestoreRevision(post.ID, old.ID, "   ", actor)
	require.NoError(t, err)

	var added model.PostRevision
	require.NoError(t, global.DB.Where("post_id = ? AND revision_no = ?", post.ID, 2).Take(&added).Error)
	assert.Equal(t, "回滚到版本 1", added.Note)
}

func TestPostService_RestoreRevisionRejectsBadIDs(t *testing.T) {
	setupEnv(t)
	actor := testActor(t)
	post := insertPost(t, "目标文章", "restore-bad-ids", model.PostStatusDraft)
	other := insertPost(t, "别的文章", "restore-other-post", model.PostStatusDraft)

	otherRevision := &model.PostRevision{
		PostID: other.ID, RevisionNo: 1, Title: other.Title,
		ContentMD: "x", ContentHTML: "x", CreatedAt: dbtime.Now(),
	}
	require.NoError(t, model.CreatePostRevision(global.DB, otherRevision))

	// 版本不存在。
	_, err := ServiceGroupApp.Post.RestoreRevision(post.ID, 987654, "", actor)
	requireAppError(t, err, errs.ErrorNotFound)
	// 版本不属于该文章 → 按不存在处理。
	_, err = ServiceGroupApp.Post.RestoreRevision(post.ID, otherRevision.ID, "", actor)
	requireAppError(t, err, errs.ErrorNotFound)

	// 文章不存在 → 1002（D18 修复，此前被包成内部错误）。
	_, err = ServiceGroupApp.Post.RestoreRevision(987654, otherRevision.ID, "", actor)
	requireAppError(t, err, errs.ErrorNotFound)

	// 失败的调用不写新版本、不改正文。
	assert.Equal(t, int64(0), countRows(t, "post_revisions", "post_id = ?", post.ID))
	assert.Equal(t, "目标文章", reloadPost(t, post.ID).Title)
}

// ---------------------------------------------------------------------------
// TaskService.HandlePublishScheduled
// ---------------------------------------------------------------------------

func TestTaskService_HandlePublishScheduled(t *testing.T) {
	setupEnv(t)
	post := insertPost(t, "任务到点", "task-publish", model.PostStatusDraft)
	markScheduled(t, post.ID, dbtime.Now().Add(-time.Minute))

	job := &model.Job{Type: model.JobTypePublishScheduled, Payload: scheduledJobPayload(post.ID)}
	require.NoError(t, TaskService{}.HandlePublishScheduled(context.Background(), job))
	assert.Equal(t, model.PostStatusPublished, reloadPost(t, post.ID).Status)

	// 参数不是 JSON / 缺 postId → 报错（调度器会走退避重试，不算成功）。
	err := TaskService{}.HandlePublishScheduled(context.Background(), &model.Job{Payload: "not-json"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "解析任务参数失败")

	err = TaskService{}.HandlePublishScheduled(context.Background(), &model.Job{Payload: `{"postId":0}`})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "缺少 postId")
}

// ---------------------------------------------------------------------------
// TaskService.HandleCleanup
// ---------------------------------------------------------------------------

func TestTaskService_HandleCleanupRemovesExpiredRows(t *testing.T) {
	setupEnv(t)
	now := dbtime.Now()

	// 回收站：31 天前软删的文章会被物理删除，1 天前的留下。
	expiredPost := insertPost(t, "过期回收站", "cleanup-old", model.PostStatusDraft)
	require.NoError(t, model.SoftDeletePost(global.DB, expiredPost.ID, now.Add(-31*24*time.Hour)))
	require.NoError(t, model.CreatePostRevision(global.DB, &model.PostRevision{
		PostID: expiredPost.ID, RevisionNo: 1, Title: expiredPost.Title,
		ContentMD: "x", ContentHTML: "x", CreatedAt: now,
	}))
	freshPost := insertPost(t, "新回收站", "cleanup-fresh", model.PostStatusDraft)
	require.NoError(t, model.SoftDeletePost(global.DB, freshPost.ID, now.Add(-24*time.Hour)))

	// page_views：91 天前删，1 天前留。
	require.NoError(t, global.DB.Create(&model.PageView{Path: "/old", CreatedAt: now.Add(-91 * 24 * time.Hour)}).Error)
	require.NoError(t, global.DB.Create(&model.PageView{Path: "/new", CreatedAt: now.Add(-24 * time.Hour)}).Error)

	// sessions：过期删，有效留。
	require.NoError(t, model.CreateSession(global.DB, &model.Session{
		TokenHash: "expired-session", AdminID: 1, CreatedAt: now, LastSeenAt: now, ExpiresAt: now.Add(-time.Hour),
	}))
	require.NoError(t, model.CreateSession(global.DB, &model.Session{
		TokenHash: "live-session", AdminID: 1, CreatedAt: now, LastSeenAt: now, ExpiresAt: now.Add(time.Hour),
	}))

	// preview_tokens：过期删、已用删、未用且未过期留。
	previewPost := insertPost(t, "预览目标", "cleanup-preview", model.PostStatusDraft)
	usedAt := now
	require.NoError(t, model.CreatePreviewToken(global.DB, &model.PreviewToken{
		TokenHash: "expired-token", PostID: previewPost.ID, ExpiresAt: now.Add(-time.Hour), CreatedAt: now,
	}))
	require.NoError(t, model.CreatePreviewToken(global.DB, &model.PreviewToken{
		TokenHash: "used-token", PostID: previewPost.ID, ExpiresAt: now.Add(time.Hour), UsedAt: &usedAt, CreatedAt: now,
	}))
	require.NoError(t, model.CreatePreviewToken(global.DB, &model.PreviewToken{
		TokenHash: "live-token", PostID: previewPost.ID, ExpiresAt: now.Add(time.Hour), CreatedAt: now,
	}))

	// login_attempts：窗口过期且已解锁的删；仍锁定或窗口内的留。
	require.NoError(t, model.CreateLoginAttempt(global.DB, &model.LoginAttempt{
		Key: "old-unlocked", Kind: model.AttemptKindLogin, Count: 3,
		WindowStart: now.Add(-48 * time.Hour), LockedUntil: 0,
	}))
	require.NoError(t, model.CreateLoginAttempt(global.DB, &model.LoginAttempt{
		Key: "old-locked", Kind: model.AttemptKindLogin, Count: 5,
		WindowStart: now.Add(-48 * time.Hour), LockedUntil: now.Add(time.Hour),
	}))
	require.NoError(t, model.CreateLoginAttempt(global.DB, &model.LoginAttempt{
		Key: "recent", Kind: model.AttemptKindLogin, Count: 1,
		WindowStart: now.Add(-time.Hour), LockedUntil: 0,
	}))

	// 版本超量：55 条裁到 50 条（§4）。
	revisionPost := insertPost(t, "版本很多", "cleanup-revisions", model.PostStatusPublished)
	for no := 1; no <= 55; no++ {
		require.NoError(t, model.CreatePostRevision(global.DB, &model.PostRevision{
			PostID: revisionPost.ID, RevisionNo: no, Title: revisionPost.Title,
			ContentMD: "x", ContentHTML: "x", CreatedAt: now.Add(time.Duration(no) * time.Second),
		}))
	}

	require.NoError(t, ServiceGroupApp.Task.HandleCleanup(context.Background(), nil))

	assert.Equal(t, int64(0), countRows(t, "posts", "id = ?", expiredPost.ID))
	assert.Equal(t, int64(1), countRows(t, "posts", "id = ?", freshPost.ID))
	assert.Equal(t, int64(0), countRows(t, "post_revisions", "post_id = ?", expiredPost.ID))
	assert.Equal(t, int64(1), countRows(t, "page_views", ""))
	assert.Equal(t, int64(1), countRows(t, "sessions", ""))
	assert.Equal(t, int64(1), countRows(t, "preview_tokens", ""))
	assert.Equal(t, int64(2), countRows(t, "login_attempts", ""))
	assert.Equal(t, int64(50), countRows(t, "post_revisions", "post_id = ?", revisionPost.ID))
	// 裁掉的是最老的 5 条（revision_no 1..5）。
	assert.Equal(t, int64(0), countRows(t, "post_revisions", "post_id = ? AND revision_no <= ?", revisionPost.ID, 5))

	// 幂等：再跑一次结果不变。
	require.NoError(t, ServiceGroupApp.Task.HandleCleanup(context.Background(), nil))
	assert.Equal(t, int64(50), countRows(t, "post_revisions", "post_id = ?", revisionPost.ID))
	assert.Equal(t, int64(1), countRows(t, "page_views", ""))
}

// ---------------------------------------------------------------------------
// TaskService.HandleBackup / HandleIntegrityCheck
// ---------------------------------------------------------------------------

func TestTaskService_HandleBackupCreatesSnapshot(t *testing.T) {
	setupEnv(t)

	// 上传目录与附件目录里各放一个文件：快照必须把它们一起带上（§14）。
	require.NoError(t, os.WriteFile(filepath.Join(global.Config.Upload.Dir, "pic.txt"), []byte("img"), 0o640))
	require.NoError(t, os.WriteFile(filepath.Join(global.Config.File.Dir, "doc.txt"), []byte("file"), 0o640))

	require.NoError(t, ServiceGroupApp.Task.HandleBackup(context.Background(), nil))

	stamp := dbtime.Now().Time().In(postSiteLocation()).Format("20060102")
	dbPath := filepath.Join(global.Config.Backup.Dir, "blog-"+stamp+".db")

	file, err := os.Open(dbPath)
	require.NoError(t, err, "库快照应当存在且能打开")

	header := make([]byte, len(sqliteMagic))
	_, err = io.ReadFull(file, header)
	require.NoError(t, err)
	// 读完立刻关掉：第二次备份要删掉同名文件，Windows 上占用中的文件删不掉。
	require.NoError(t, file.Close())
	assert.Equal(t, sqliteMagic, string(header), "快照必须是完整的 SQLite 文件")

	for _, name := range []string{
		filepath.Join("uploads-"+stamp, "pic.txt"),
		filepath.Join("files-"+stamp, "doc.txt"),
	} {
		_, err := os.Stat(filepath.Join(global.Config.Backup.Dir, name))
		require.NoError(t, err, "目录快照里应当有 %s", name)
	}

	// 同一天重复执行：删掉旧文件重做（VACUUM INTO 要求目标不存在），仍然只有一份。
	require.NoError(t, ServiceGroupApp.Task.HandleBackup(context.Background(), nil))
	entries, err := os.ReadDir(global.Config.Backup.Dir)
	require.NoError(t, err)
	dbs := 0
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasPrefix(entry.Name(), "blog-") && strings.HasSuffix(entry.Name(), ".db") {
			dbs++
		}
	}
	assert.Equal(t, 1, dbs, "同一天只应留一个库快照")
}

func TestTaskService_HandleBackupRequiresDir(t *testing.T) {
	setupEnv(t)

	dir := global.Config.Backup.Dir
	global.Config.Backup.Dir = ""
	t.Cleanup(func() { global.Config.Backup.Dir = dir })

	err := ServiceGroupApp.Task.HandleBackup(context.Background(), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "backup.dir")
}

func TestTaskService_HandleIntegrityCheckOnHealthyDB(t *testing.T) {
	setupEnv(t)

	result, err := integrityCheck()
	require.NoError(t, err)
	assert.Equal(t, "ok", result)

	// 还没备份过：不是"库坏了"，任务仍算通过（只记 warn）。
	require.NoError(t, ServiceGroupApp.Task.HandleIntegrityCheck(context.Background(), nil))

	// 备份之后，校验对象是最近一次快照的文件头。
	require.NoError(t, ServiceGroupApp.Task.HandleBackup(context.Background(), nil))
	require.NoError(t, ServiceGroupApp.Task.HandleIntegrityCheck(context.Background(), nil))

	name, size, err := latestBackupSnapshot()
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(name, "blog-"))
	assert.True(t, strings.HasSuffix(name, ".db"))
	assert.Positive(t, size)
}

func TestTaskService_LatestBackupSnapshotRejectsCorruptFile(t *testing.T) {
	setupEnv(t)

	// 目录里还没有快照。
	_, _, err := latestBackupSnapshot()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "还没有数据库快照")

	// 文件头对不上（备份被截断或写坏）。
	require.NoError(t, os.WriteFile(filepath.Join(global.Config.Backup.Dir, "blog-20200101.db"),
		[]byte("not-a-sqlite-file"), 0o640))
	_, _, err = latestBackupSnapshot()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "不是完整的 SQLite 文件")

	// 未配置目录。
	dir := global.Config.Backup.Dir
	global.Config.Backup.Dir = ""
	t.Cleanup(func() { global.Config.Backup.Dir = dir })
	_, _, err = latestBackupSnapshot()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "backup.dir")
}

func TestTaskService_PruneBackupsRemovesOnlyExpired(t *testing.T) {
	setupEnv(t)
	dir := global.Config.Backup.Dir

	oldDB := filepath.Join(dir, "blog-20200101.db")
	freshDB := filepath.Join(dir, "blog-20990101.db")
	oldUploads := filepath.Join(dir, "uploads-20200101")
	require.NoError(t, os.WriteFile(oldDB, []byte("old"), 0o640))
	require.NoError(t, os.WriteFile(freshDB, []byte("new"), 0o640))
	require.NoError(t, os.MkdirAll(oldUploads, 0o750))

	// pruneBackups 按 ModTime 判断，把过期项的 mtime 拨到保留期之外。
	stale := time.Now().Add(-40 * 24 * time.Hour)
	require.NoError(t, os.Chtimes(oldDB, stale, stale))
	// 目录快照的保留期是 90 天，要拨得更久才算过期。
	staleUploads := time.Now().Add(-100 * 24 * time.Hour)
	require.NoError(t, os.Chtimes(oldUploads, staleUploads, staleUploads))

	removed, err := pruneBackups(dir, "blog-", ".db", backupDBRetention)
	require.NoError(t, err)
	assert.Equal(t, 1, removed)
	_, err = os.Stat(oldDB)
	assert.True(t, os.IsNotExist(err))
	_, err = os.Stat(freshDB)
	require.NoError(t, err, "未过期的快照不能被删")

	// 目录快照按 90 天保留期单独裁剪。
	removed, err = pruneBackups(dir, "uploads-", "", backupUploadsRetention)
	require.NoError(t, err)
	assert.Equal(t, 1, removed)
	_, err = os.Stat(oldUploads)
	assert.True(t, os.IsNotExist(err))
}

func TestTaskService_SnapshotDirSkipsMissingSource(t *testing.T) {
	setupEnv(t)

	// 目录不存在不是错误（首次部署时 uploads/files 可能还没建）。
	count, err := snapshotDir(filepath.Join(global.Config.Upload.Dir, "not-exist"),
		filepath.Join(t.TempDir(), "target"))
	require.NoError(t, err)
	assert.Zero(t, count)
}

func TestTaskService_CopyFile(t *testing.T) {
	setupEnv(t)
	dir := t.TempDir()
	src := filepath.Join(dir, "src.txt")
	dst := filepath.Join(dir, "dst.txt")
	require.NoError(t, os.WriteFile(src, []byte("附件内容"), 0o640))

	// 硬链接不可用时的退路：内容必须完整复制。
	require.NoError(t, copyFile(src, dst))
	copied, err := os.ReadFile(dst)
	require.NoError(t, err)
	assert.Equal(t, "附件内容", string(copied))

	// 源文件不存在 → 报错（不静默产生空文件）。
	require.Error(t, copyFile(filepath.Join(dir, "missing.txt"), filepath.Join(dir, "x.txt")))
}

// ---------------------------------------------------------------------------
// TaskService.HandleLinkCheck
// ---------------------------------------------------------------------------

func TestTaskService_HandleLinkCheckRecordsResults(t *testing.T) {
	setupEnv(t)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ok":
			w.WriteHeader(http.StatusOK)
		case "/gone.png":
			w.WriteHeader(http.StatusNotFound)
		case "/head-unsupported.png":
			// 部分站点不支持 HEAD：headURL 会退回 GET。
			if r.Method == http.MethodHead {
				w.WriteHeader(http.StatusMethodNotAllowed)
				return
			}
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer server.Close()

	post := insertPost(t, "外链巡检", "link-check-post", model.PostStatusPublished)
	contentHTML := strings.Join([]string{
		`<p><a href="` + server.URL + `/ok">ok</a></p>`,
		`<p><a href="` + server.URL + `/ok">重复链接</a></p>`, // 去重
		`<p><img src="` + server.URL + `/gone.png"></p>`,  // 404 → 失败
		`<p><img src="` + server.URL + `/head-unsupported.png"></p>`,
		`<p><img src="/uploads/local.png"></p>`, // 站内相对路径不检查
	}, "\n")
	require.NoError(t, model.UpdatePostFields(global.DB, post.ID, map[string]any{"content_html": contentHTML}))

	// 旧记录会被本次结果整体替换。
	require.NoError(t, model.ReplaceLinkChecks(global.DB, post.ID, []model.LinkCheck{
		{PostID: post.ID, URL: "https://stale.example/x", Kind: model.LinkCheckKindLink, CheckedAt: dbtime.Now()},
	}))

	require.NoError(t, ServiceGroupApp.Task.HandleLinkCheck(context.Background(), nil))

	// 3 条目标：/ok（链接）+ /gone.png + /head-unsupported.png（图片）；重复与相对路径不计。
	require.Equal(t, int64(3), countRows(t, "link_checks", "post_id = ?", post.ID))
	assert.Equal(t, int64(0), countRows(t, "link_checks", "url = ?", "https://stale.example/x"))

	var okRow model.LinkCheck
	require.NoError(t, global.DB.Where("post_id = ? AND url = ?", post.ID, server.URL+"/ok").Take(&okRow).Error)
	assert.True(t, okRow.OK)
	assert.Equal(t, http.StatusOK, okRow.StatusCode)
	assert.Empty(t, okRow.Error)
	assert.Equal(t, model.LinkCheckKindLink, okRow.Kind)
	assert.False(t, okRow.CheckedAt.IsZero())

	var missing model.LinkCheck
	require.NoError(t, global.DB.Where("post_id = ? AND url = ?", post.ID, server.URL+"/gone.png").Take(&missing).Error)
	assert.False(t, missing.OK)
	assert.Equal(t, http.StatusNotFound, missing.StatusCode)
	assert.Contains(t, missing.Error, "404")
	assert.Equal(t, model.LinkCheckKindImage, missing.Kind)

	// HEAD 405 → GET 200，仍算可用。
	var fallback model.LinkCheck
	require.NoError(t, global.DB.Where("post_id = ? AND url = ?", post.ID, server.URL+"/head-unsupported.png").Take(&fallback).Error)
	assert.True(t, fallback.OK)
	assert.Equal(t, http.StatusOK, fallback.StatusCode)
}

func TestTaskService_HandleLinkCheckSkipsNonPublishedPosts(t *testing.T) {
	setupEnv(t)

	post := insertPost(t, "草稿不外检", "link-check-draft", model.PostStatusDraft)
	require.NoError(t, model.UpdatePostFields(global.DB, post.ID, map[string]any{
		"content_html": `<a href="https://example.invalid/x">x</a>`,
	}))

	// 草稿不参与巡检：即使正文里有外链也不发请求、不落库。
	require.NoError(t, ServiceGroupApp.Task.HandleLinkCheck(context.Background(), nil))
	assert.Equal(t, int64(0), countRows(t, "link_checks", "post_id = ?", post.ID))
}

func TestTaskService_HandleLinkCheckClearsRowsWhenTargetsRemoved(t *testing.T) {
	// 正文里的外链/图片被全部删掉后，这篇的旧记录必须一起清掉（D18 修复）。
	// 早先 `len(targets) == 0 → continue` 跳过了 ReplaceLinkChecks，
	// 任务页会一直显示早已不存在的坏链。
	setupEnv(t)

	post := insertPost(t, "外链已删", "link-check-stale", model.PostStatusPublished)
	require.NoError(t, model.UpdatePostFields(global.DB, post.ID, map[string]any{
		"content_html": `<p>正文里已经没有外链了</p>`,
	}))
	require.NoError(t, model.ReplaceLinkChecks(global.DB, post.ID, []model.LinkCheck{
		{PostID: post.ID, URL: "https://stale.example/x", Kind: model.LinkCheckKindLink,
			StatusCode: http.StatusNotFound, Error: "HTTP 404", CheckedAt: dbtime.Now()},
	}))

	require.NoError(t, ServiceGroupApp.Task.HandleLinkCheck(context.Background(), nil))

	assert.Equal(t, int64(0), countRows(t, "link_checks", "post_id = ?", post.ID))
}

func TestTaskService_ExtractCheckTargets(t *testing.T) {
	html := `<a href="https://a.example/1">a</a><a href="https://a.example/1">dup</a>` +
		`<img src="https://b.example/2.png"><img src="/uploads/rel.png">` +
		`<a href="https://c.example/3">c</a>`

	targets := extractCheckTargets(html)
	require.Len(t, targets, 3)
	// 先收全部 <a>（文档顺序），再收 <img>：这是实现的分组顺序。
	assert.Equal(t, checkTarget{url: "https://a.example/1", kind: model.LinkCheckKindLink}, targets[0])
	assert.Equal(t, checkTarget{url: "https://c.example/3", kind: model.LinkCheckKindLink}, targets[1])
	assert.Equal(t, checkTarget{url: "https://b.example/2.png", kind: model.LinkCheckKindImage}, targets[2])

	// 每篇最多 linkCheckPerPost 条，避免一篇文章占满整轮巡检。
	var many strings.Builder
	for i := 0; i < linkCheckPerPost+5; i++ {
		fmt.Fprintf(&many, `<a href="https://x.example/%d">x</a>`, i)
	}
	assert.Len(t, extractCheckTargets(many.String()), linkCheckPerPost)

	// 站内相对路径与空串不检查。
	assert.Empty(t, extractCheckTargets(`<a href="/local">local</a><a href="">empty</a>`))
}
