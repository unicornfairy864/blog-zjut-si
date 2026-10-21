package service

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/unicornfairy864/blog-zjut-si/apps/server/core/render"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/global"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/model"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/model/common/dbtime"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/model/common/errs"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/utils"
)

// 发布控制相关常量（agent.md §5「发布控制与任务口径」）。
const (
	// previewTokenBytes 是预览令牌的随机字节数（hex 后 64 位）。
	previewTokenBytes = 32
	// previewTokenTTL 是预览链接的有效期（§4：24h）。
	previewTokenTTL = 24 * time.Hour
)

// PreviewLink 是一次性预览链接的生成结果；token 明文只出现这一次（§5）。
type PreviewLink struct {
	Token     string      `json:"token"`
	URL       string      `json:"url"`
	ExpiresAt dbtime.Unix `json:"expiresAt"`
}

// PreviewPost 是公开预览接口的输出（§5）。
//
// 不含评论与计数：预览只是"看正文"，不是公开页。
type PreviewPost struct {
	Slug        string       `json:"slug"`
	Title       string       `json:"title"`
	Status      string       `json:"status"`
	Excerpt     string       `json:"excerpt"`
	ContentHTML string       `json:"contentHtml"`
	WordCount   int          `json:"wordCount"`
	ReadingTime int          `json:"readingTime"`
	PublishedAt *dbtime.Unix `json:"publishedAt"`
	UpdatedAt   dbtime.Unix  `json:"updatedAt"`
	ExpiresAt   dbtime.Unix  `json:"expiresAt"`
}

// scheduledJobPayload 是 publish_scheduled 任务的 payload；固定格式便于精确撤销（§5）。
func scheduledJobPayload(postID int64) string {
	return fmt.Sprintf(`{"postId":%d}`, postID)
}

// Schedule 定时发布：publishedAt 必须是未来时间（§5 / §6）。
//
// 重复调用会先撤掉该文待执行的同类任务，再按新时间投递，避免到点连发两次。
func (s PostService) Schedule(id int64, publishedAt dbtime.Unix, note string, actor PostActor) (*model.Post, error) {
	if !publishedAt.After(dbtime.Now()) {
		return nil, errs.New(errs.ErrorValidation, "publishedAt 必须是未来时间")
	}

	if err := global.DB.Transaction(func(tx *gorm.DB) error {
		post, err := model.FindPostByID(tx, id, false)
		if err != nil {
			return err
		}
		if post.Status == model.PostStatusArchived {
			return errs.New(errs.ErrorConflict, "已归档文章不能定时发布")
		}
		// §6 的状态机里没有 published → scheduled 这条边：直接放行会把文章从公开端
		// 撤下来，还会把原发布时间改写成未来时间。要重新定时必须先取消发布。
		if post.Status == model.PostStatusPublished {
			return errs.New(errs.ErrorConflict, "已发布文章请先取消发布再定时")
		}

		now := dbtime.Now()
		payload := scheduledJobPayload(post.ID)
		if _, err := model.DeletePendingJobs(tx, model.JobTypePublishScheduled, payload); err != nil {
			return err
		}
		if err := model.CreateJob(tx, &model.Job{
			Type:        model.JobTypePublishScheduled,
			Payload:     payload,
			RunAt:       publishedAt,
			Status:      model.JobStatusPending,
			MaxAttempts: model.DefaultJobMaxAttempts,
			CreatedAt:   now,
		}); err != nil {
			return err
		}
		if err := model.UpdatePostFields(tx, post.ID, map[string]any{
			"status":       model.PostStatusScheduled,
			"published_at": publishedAt,
			"updated_at":   now,
		}); err != nil {
			return err
		}
		return ServiceGroupApp.Audit.WriteTx(tx, AuditInput{
			AdminID:    actor.AdminID,
			Action:     AuditActionPostSchedule,
			TargetType: "post",
			TargetID:   strconv.FormatInt(post.ID, 10),
			Meta:       map[string]any{"publishedAt": int64(publishedAt), "note": strings.TrimSpace(note)},
			IP:         actor.IP,
		})
	}); err != nil {
		if _, ok := errs.As(err); ok {
			return nil, err
		}
		return nil, s.notFoundOr(err, "定时发布失败")
	}
	return s.Get(id)
}

// PublishScheduledPost 是 publish_scheduled 任务的入口：到点后再确认一次状态（§13）。
//
// 用户中途取消定时、改成草稿或删了文章，都只是跳过，不算任务失败。
func (s PostService) PublishScheduledPost(postID int64) error {
	return global.DB.Transaction(func(tx *gorm.DB) error {
		post, err := model.FindPostByID(tx, postID, true)
		if err != nil {
			if errors.Is(err, model.ErrNotFound) {
				global.Log.Warn("定时发布任务跳过：文章不存在", "post_id", postID)
				return nil
			}
			return err
		}

		now := dbtime.Now()
		if post.DeletedAt != nil || post.Status != model.PostStatusScheduled || post.PublishedAt == nil {
			global.Log.Info("定时发布任务跳过", "post_id", postID, "status", post.Status)
			return nil
		}
		if post.PublishedAt.After(now) {
			// 正常路径下不会发生（改时间会重投任务）；真遇到就只记日志，不误发。
			global.Log.Warn("定时发布任务跳过：时间还没到", "post_id", postID,
				"published_at", int64(*post.PublishedAt))
			return nil
		}

		// 发布时刻用排定的时间：多篇积压时仍保持原有先后。
		return publishInTx(tx, post, "定时发布", *post.PublishedAt, now, PostActor{})
	})
}

// RestoreRevision 用旧版本覆盖正文，并**再写一条新 revision**（§5：历史不被改写）。
func (s PostService) RestoreRevision(id, revisionID int64, note string, actor PostActor) (*model.Post, error) {
	if err := global.DB.Transaction(func(tx *gorm.DB) error {
		post, err := model.FindPostByID(tx, id, false)
		if err != nil {
			return err
		}
		revision, err := model.FindPostRevision(tx, post.ID, revisionID)
		if err != nil {
			if errors.Is(err, model.ErrNotFound) {
				return errs.New(errs.ErrorNotFound, "版本不存在")
			}
			return err
		}

		now := dbtime.Now()
		rendered := render.Markdown(revision.ContentMD)
		revisionNo := post.Revision + 1
		restoreNote := strings.TrimSpace(note)
		if restoreNote == "" {
			restoreNote = fmt.Sprintf("回滚到版本 %d", revision.RevisionNo)
		}

		if err := model.CreatePostRevision(tx, &model.PostRevision{
			PostID:      post.ID,
			RevisionNo:  revisionNo,
			Title:       revision.Title,
			ContentMD:   revision.ContentMD,
			ContentHTML: rendered.HTML,
			Note:        restoreNote,
			CreatedAt:   now,
		}); err != nil {
			return err
		}
		if err := model.UpdatePostFields(tx, post.ID, map[string]any{
			"title":        revision.Title,
			"content_md":   revision.ContentMD,
			"content_html": rendered.HTML,
			"content_text": rendered.Text,
			"word_count":   rendered.WordCount,
			"reading_time": rendered.ReadingTime,
			"excerpt":      excerptOrAuto(post.Excerpt, rendered.Text),
			"revision":     revisionNo,
			"updated_at":   now,
		}); err != nil {
			return err
		}
		if _, err := model.PrunePostRevisions(tx, post.ID, postRevisionKeep); err != nil {
			return err
		}
		return ServiceGroupApp.Audit.WriteTx(tx, AuditInput{
			AdminID:    actor.AdminID,
			Action:     AuditActionPostRestore,
			TargetType: "post",
			TargetID:   strconv.FormatInt(post.ID, 10),
			Meta:       map[string]any{"from": revision.RevisionNo, "to": revisionNo},
			IP:         actor.IP,
		})
	}); err != nil {
		if _, ok := errs.As(err); ok {
			return nil, err
		}
		return nil, s.notFoundOr(err, "回滚版本失败")
	}
	return s.Get(id)
}

// CreatePreviewLink 生成一次性预览链接：库里只存 hash，明文只回一次（§5）。
func (s PostService) CreatePreviewLink(id int64, actor PostActor) (PreviewLink, error) {
	post, err := model.FindPostByID(global.DB, id, true)
	if err != nil {
		return PreviewLink{}, s.notFoundOr(err, "查询文章失败")
	}
	if post.DeletedAt != nil {
		return PreviewLink{}, errs.New(errs.ErrorGone, "文章已删除")
	}

	token, err := utils.RandomToken(previewTokenBytes)
	if err != nil {
		return PreviewLink{}, err
	}
	expiresAt := dbtime.Now().Add(previewTokenTTL)

	if err := global.DB.Transaction(func(tx *gorm.DB) error {
		// 重新生成即作废旧链接：一次性链接不该同时存在多把钥匙。
		if _, err := model.DeleteUnusedPreviewTokens(tx, post.ID); err != nil {
			return err
		}
		if err := model.CreatePreviewToken(tx, &model.PreviewToken{
			TokenHash: utils.SHA256Hex(token),
			PostID:    post.ID,
			ExpiresAt: expiresAt,
			CreatedAt: dbtime.Now(),
		}); err != nil {
			return err
		}
		return ServiceGroupApp.Audit.WriteTx(tx, AuditInput{
			AdminID:    actor.AdminID,
			Action:     AuditActionPreviewCreate,
			TargetType: "post",
			TargetID:   strconv.FormatInt(post.ID, 10),
			Meta:       map[string]any{"expiresAt": int64(expiresAt)},
			IP:         actor.IP,
		})
	}); err != nil {
		return PreviewLink{}, fmt.Errorf("生成预览链接失败: %w", err)
	}

	return PreviewLink{Token: token, URL: "/preview/" + token, ExpiresAt: expiresAt}, nil
}

// DeletePreviewLink 立即作废一条预览链接；token 不存在 → 404（§5）。
func (s PostService) DeletePreviewLink(token string, actor PostActor) error {
	if !validPreviewToken(token) {
		return errs.New(errs.ErrorNotFound, "预览链接不存在")
	}
	deleted, err := model.DeletePreviewToken(global.DB, utils.SHA256Hex(token))
	if err != nil {
		return fmt.Errorf("作废预览链接失败: %w", err)
	}
	if !deleted {
		return errs.New(errs.ErrorNotFound, "预览链接不存在")
	}

	ServiceGroupApp.Audit.Write(AuditInput{
		AdminID:    actor.AdminID,
		Action:     AuditActionPreviewDelete,
		TargetType: "preview",
		TargetID:   token[:8],
		IP:         actor.IP,
	})
	return nil
}

// PreviewByToken 用一次性令牌取预览内容：首次访问写 used_at，之后或过期 → 410/2006（§5）。
func (s PostService) PreviewByToken(token string) (PreviewPost, error) {
	if !validPreviewToken(token) {
		return PreviewPost{}, errs.New(errs.ErrorNotFound, "预览链接不存在")
	}

	var result PreviewPost
	err := global.DB.Transaction(func(tx *gorm.DB) error {
		record, err := model.FindPreviewToken(tx, utils.SHA256Hex(token))
		if err != nil {
			if errors.Is(err, model.ErrNotFound) {
				return errs.New(errs.ErrorNotFound, "预览链接不存在")
			}
			return err
		}

		now := dbtime.Now()
		switch {
		case record.UsedAt != nil:
			return errs.New(errs.ErrorPreviewExpired, "预览链接已使用")
		case !record.ExpiresAt.After(now):
			return errs.New(errs.ErrorPreviewExpired, "预览链接已过期")
		}

		post, err := model.FindPostByID(tx, record.PostID, true)
		if err != nil {
			if errors.Is(err, model.ErrNotFound) {
				return errs.New(errs.ErrorNotFound, "文章不存在")
			}
			return err
		}
		if post.DeletedAt != nil {
			return errs.New(errs.ErrorGone, "文章已删除")
		}

		if err := model.MarkPreviewTokenUsed(tx, record.TokenHash, now); err != nil {
			return err
		}
		result = PreviewPost{
			Slug:        post.Slug,
			Title:       post.Title,
			Status:      post.Status,
			Excerpt:     post.Excerpt,
			ContentHTML: post.ContentHTML,
			WordCount:   post.WordCount,
			ReadingTime: post.ReadingTime,
			PublishedAt: post.PublishedAt,
			UpdatedAt:   post.UpdatedAt,
			ExpiresAt:   record.ExpiresAt,
		}
		return nil
	})
	if err != nil {
		if _, ok := errs.As(err); ok {
			return PreviewPost{}, err
		}
		return PreviewPost{}, fmt.Errorf("读取预览失败: %w", err)
	}

	// 正文图片同样补 srcset 与原图直链，预览才和发布结果一致（§10）。
	result.ContentHTML = ServiceGroupApp.Media.DecorateContent(result.ContentHTML)
	return result, nil
}

// publishInTx 把一篇文章置为已发布：写版本快照、重算渲染、revision+1（§6）。
//
// 手动发布、定时发布共用这一处，保证两条路径的产物完全一致。
// at 是 published_at 的取值（手动发布传 now，定时发布传排定时间）。
func publishInTx(tx *gorm.DB, post *model.Post, note string, at, now dbtime.Unix, actor PostActor) error {
	rendered := render.Markdown(post.ContentMD)
	revisionNo := post.Revision + 1

	if err := model.CreatePostRevision(tx, &model.PostRevision{
		PostID:      post.ID,
		RevisionNo:  revisionNo,
		Title:       post.Title,
		ContentMD:   post.ContentMD,
		ContentHTML: rendered.HTML,
		Note:        strings.TrimSpace(note),
		CreatedAt:   now,
	}); err != nil {
		return err
	}

	fields := map[string]any{
		"status":       model.PostStatusPublished,
		"published_at": at,
		"content_html": rendered.HTML,
		"content_text": rendered.Text,
		"word_count":   rendered.WordCount,
		"reading_time": rendered.ReadingTime,
		"excerpt":      excerptOrAuto(post.Excerpt, rendered.Text),
		"revision":     revisionNo,
		"updated_at":   now,
	}
	if err := model.UpdatePostFields(tx, post.ID, fields); err != nil {
		return err
	}
	if _, err := model.PrunePostRevisions(tx, post.ID, postRevisionKeep); err != nil {
		return err
	}
	if err := ServiceGroupApp.Audit.WriteTx(tx, AuditInput{
		AdminID:    actor.AdminID,
		Action:     AuditActionPostPublish,
		TargetType: "post",
		TargetID:   strconv.FormatInt(post.ID, 10),
		Meta:       map[string]any{"revision": revisionNo, "scheduled": at != now},
		IP:         actor.IP,
	}); err != nil {
		return err
	}

	post.Status = model.PostStatusPublished
	post.PublishedAt = &at
	post.Revision = revisionNo
	return nil
}

// cancelScheduledJobs 撤掉某文待执行的定时发布任务（取消发布、回草稿时调用）。
func cancelScheduledJobs(tx *gorm.DB, postID int64) error {
	_, err := model.DeletePendingJobs(tx, model.JobTypePublishScheduled, scheduledJobPayload(postID))
	return err
}

// validPreviewToken 校验预览令牌形状：32 字节随机数的 hex。
func validPreviewToken(token string) bool {
	if len(token) != previewTokenBytes*2 {
		return false
	}
	for i := 0; i < len(token); i++ {
		switch ch := token[i]; {
		case ch >= '0' && ch <= '9', ch >= 'a' && ch <= 'f':
		default:
			return false
		}
	}
	return true
}
