package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/unicornfairy864/blog-zjut-si/apps/server/global"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/model"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/model/common/dbtime"
)

// TaskService 承载后台任务的具体动作（agent.md §13）。
//
// 任务体只做"一件事"：重试、退避与并发（固定 1）都由 task.Scheduler 统一负责。
type TaskService struct{}

// 保留期与限额（agent.md §13 / §14）。
const (
	// postTrashRetention 是回收站保留期：满 30 天物理删除。
	postTrashRetention = 30 * 24 * time.Hour
	// pageViewRetention 是访问明细保留期：90 天。
	pageViewRetention = 90 * 24 * time.Hour
	// originalRetention 是本地原图在归档完成后的保留期：30 天。
	originalRetention = 30 * 24 * time.Hour
	// loginAttemptRetention 是登录失败计数的清理门槛。
	loginAttemptRetention = 24 * time.Hour
	// backupDBRetention / backupUploadsRetention 是备份保留期（§14：库 30 天、图片 90 天）。
	backupDBRetention      = 30 * 24 * time.Hour
	backupUploadsRetention = 90 * 24 * time.Hour
	// linkCheckMaxURLs / linkCheckPerPost 限制单次巡检的外链数量，避免长时间占住调度器。
	linkCheckMaxURLs = 60
	linkCheckPerPost = 10
	// linkCheckTimeout 是单次 HEAD 的超时。
	linkCheckTimeout = 3 * time.Second
)

// scheduledPayload 是 publish_scheduled 任务的参数。
type scheduledPayload struct {
	PostID int64 `json:"postId"`
}

// HandlePublishScheduled 执行定时发布（§13）。
func (s TaskService) HandlePublishScheduled(_ context.Context, job *model.Job) error {
	var payload scheduledPayload
	if err := json.Unmarshal([]byte(job.Payload), &payload); err != nil {
		return fmt.Errorf("解析任务参数失败: %w", err)
	}
	if payload.PostID <= 0 {
		return errors.New("任务参数缺少 postId")
	}
	return ServiceGroupApp.Post.PublishScheduledPost(payload.PostID)
}

// HandleCleanup 按 §13 清理过期数据；每一步都幂等，可随时手动重跑。
func (s TaskService) HandleCleanup(_ context.Context, _ *model.Job) error {
	now := dbtime.Now()
	result := model.CleanupResult{}

	posts, err := model.PurgeDeletedPosts(global.DB, now.Add(-postTrashRetention))
	if err != nil {
		return fmt.Errorf("清理回收站失败: %w", err)
	}
	result.Posts = posts

	views, err := model.DeletePageViewsBefore(global.DB, now.Add(-pageViewRetention))
	if err != nil {
		return fmt.Errorf("清理访问明细失败: %w", err)
	}
	result.PageViews = views

	postIDs, err := model.ListPostsWithManyRevisions(global.DB, postRevisionKeep)
	if err != nil {
		return fmt.Errorf("查询超量版本失败: %w", err)
	}
	for _, postID := range postIDs {
		removed, pruneErr := model.PrunePostRevisions(global.DB, postID, postRevisionKeep)
		if pruneErr != nil {
			return fmt.Errorf("裁剪文章 %d 的版本失败: %w", postID, pruneErr)
		}
		result.Revisions += removed
	}

	sessions, err := model.DeleteExpiredSessions(global.DB, now)
	if err != nil {
		return fmt.Errorf("清理过期会话失败: %w", err)
	}
	result.Sessions = sessions

	previewLinks, err := model.DeleteExpiredPreviewTokens(global.DB, now)
	if err != nil {
		return fmt.Errorf("清理过期预览链接失败: %w", err)
	}
	result.PreviewLinks = previewLinks

	attempts, err := model.DeleteLoginAttemptsBefore(global.DB, now.Add(-loginAttemptRetention))
	if err != nil {
		return fmt.Errorf("清理登录计数失败: %w", err)
	}
	result.LoginAttempt = attempts

	originals, err := ServiceGroupApp.Media.PurgeArchivedOriginals(now.Add(-originalRetention))
	if err != nil {
		return fmt.Errorf("清理本地原图失败: %w", err)
	}
	result.Originals = originals

	global.Log.Info("清理完成",
		"posts", result.Posts, "page_views", result.PageViews, "revisions", result.Revisions,
		"sessions", result.Sessions, "preview_links", result.PreviewLinks,
		"login_attempts", result.LoginAttempt, "originals", result.Originals)
	return nil
}

// HandleBackup 备份数据库与图片（§14）：`VACUUM INTO` + uploads 硬链接快照。
//
// 用硬链接而不是复制：同一文件系统上零拷贝，且上传目录里的文件写完就不再改（C10）。
func (s TaskService) HandleBackup(_ context.Context, _ *model.Job) error {
	dir := global.Config.Backup.Dir
	if dir == "" {
		return errors.New("未配置 backup.dir")
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("创建备份目录失败: %w", err)
	}

	now := dbtime.Now().Time().In(postSiteLocation())
	stamp := now.Format("20060102")
	dbPath := filepath.Join(dir, "blog-"+stamp+".db")
	if _, err := os.Stat(dbPath); err == nil {
		// 同一天重复执行：删掉旧文件重做，保证备份是完整的单文件。
		if err := os.Remove(dbPath); err != nil {
			return fmt.Errorf("清理旧备份失败: %w", err)
		}
	}

	// VACUUM INTO 需要目标文件不存在，且会顺带整理碎片。
	if err := global.DB.Exec("VACUUM INTO ?", dbPath).Error; err != nil {
		return fmt.Errorf("备份数据库失败: %w", err)
	}

	snapshot := filepath.Join(dir, "uploads-"+stamp)
	files, err := snapshotUploads(global.Config.Upload.Dir, snapshot)
	if err != nil {
		return fmt.Errorf("备份上传目录失败: %w", err)
	}

	removedDB, err := pruneBackups(dir, "blog-", ".db", backupDBRetention)
	if err != nil {
		return err
	}
	removedUploads, err := pruneBackups(dir, "uploads-", "", backupUploadsRetention)
	if err != nil {
		return err
	}

	global.Log.Info("备份完成", "db", dbPath, "uploads_files", files,
		"pruned_db", removedDB, "pruned_uploads", removedUploads)
	return nil
}

// HandleLinkCheck 巡检已发布文章里的外链与正文图片（§13），结果写入 link_checks。
func (s TaskService) HandleLinkCheck(ctx context.Context, _ *model.Job) error {
	posts, err := model.ListPublishedForLinkCheck(global.DB)
	if err != nil {
		return fmt.Errorf("查询待巡检文章失败: %w", err)
	}

	client := &http.Client{Timeout: linkCheckTimeout}
	checked := 0
	failures := 0

	for i := range posts {
		if checked >= linkCheckMaxURLs {
			break
		}
		targets := extractCheckTargets(posts[i].ContentHTML)
		if len(targets) == 0 {
			continue
		}

		items := make([]model.LinkCheck, 0, len(targets))
		now := dbtime.Now()
		for _, target := range targets {
			if checked >= linkCheckMaxURLs {
				break
			}
			checked++

			item := model.LinkCheck{
				PostID:    posts[i].ID,
				URL:       target.url,
				Kind:      target.kind,
				CheckedAt: now,
			}
			status, headErr := headURL(ctx, client, target.url)
			item.StatusCode = status
			switch {
			case headErr != nil:
				item.Error = truncateRunes(headErr.Error(), 200)
			case status >= 400:
				item.Error = fmt.Sprintf("HTTP %d", status)
			default:
				item.OK = true
			}
			if !item.OK {
				failures++
			}
			items = append(items, item)
		}
		if err := model.ReplaceLinkChecks(global.DB, posts[i].ID, items); err != nil {
			return fmt.Errorf("写入巡检结果失败: %w", err)
		}
	}

	global.Log.Info("外链巡检完成", "checked", checked, "failures", failures)
	return nil
}

// checkTarget 是一个待检查的 URL 及其类型。
type checkTarget struct {
	url  string
	kind string
}

var (
	// linkCheckHrefPattern / linkCheckSrcPattern 从已消毒的正文 HTML 里取外链与图片。
	linkCheckHrefPattern = regexp.MustCompile(`<a[^>]+href="(https?://[^"]+)"`)
	linkCheckSrcPattern  = regexp.MustCompile(`<img[^>]+src="(https?://[^"]+)"`)
)

// extractCheckTargets 取正文里的站外链接与图片；站内相对路径（/uploads/…）不检查。
func extractCheckTargets(contentHTML string) []checkTarget {
	targets := make([]checkTarget, 0, linkCheckPerPost)
	seen := make(map[string]struct{}, linkCheckPerPost)

	collect := func(pattern *regexp.Regexp, kind string) {
		for _, match := range pattern.FindAllStringSubmatch(contentHTML, -1) {
			if len(targets) >= linkCheckPerPost {
				return
			}
			url := strings.TrimSpace(match[1])
			if url == "" {
				continue
			}
			if _, ok := seen[url]; ok {
				continue
			}
			seen[url] = struct{}{}
			targets = append(targets, checkTarget{url: url, kind: kind})
		}
	}

	collect(linkCheckHrefPattern, model.LinkCheckKindLink)
	collect(linkCheckSrcPattern, model.LinkCheckKindImage)
	return targets
}

// headURL 发一次 HEAD；部分站点不支持 HEAD 时退回 GET（只读响应头就关掉）。
func headURL(ctx context.Context, client *http.Client, url string) (int, error) {
	status, err := doRequest(ctx, client, http.MethodHead, url)
	if err == nil && status < 400 {
		return status, nil
	}

	getStatus, getErr := doRequest(ctx, client, http.MethodGet, url)
	if getErr != nil {
		return status, getErr
	}
	return getStatus, nil
}

// doRequest 发一次请求并立刻关闭响应体；只关心状态码。
func doRequest(ctx context.Context, client *http.Client, method, url string) (int, error) {
	request, err := http.NewRequestWithContext(ctx, method, url, nil)
	if err != nil {
		return 0, err
	}
	request.Header.Set("User-Agent", "zjut-si-link-check/1.0")

	response, err := client.Do(request)
	if err != nil {
		return 0, err
	}
	defer func() {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		_ = response.Body.Close()
	}()
	return response.StatusCode, nil
}

// snapshotUploads 把上传目录硬链接到快照目录；跨文件系统时退回复制。
func snapshotUploads(source, target string) (int, error) {
	if _, err := os.Stat(source); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return 0, nil
		}
		return 0, err
	}

	files := 0
	err := filepath.WalkDir(source, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			relative, err := filepath.Rel(source, path)
			if err != nil {
				return err
			}
			return os.MkdirAll(filepath.Join(target, relative), 0o750)
		}

		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		destination := filepath.Join(target, relative)
		if _, statErr := os.Stat(destination); statErr == nil {
			return nil
		}
		if linkErr := os.Link(path, destination); linkErr == nil {
			files++
			return nil
		}
		if copyErr := copyFile(path, destination); copyErr != nil {
			return copyErr
		}
		files++
		return nil
	})
	if err != nil {
		return files, err
	}
	return files, nil
}

// copyFile 是硬链接不可用时的退路。
func copyFile(source, target string) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()

	out, err := os.Create(target)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

// pruneBackups 删除超过保留期的备份文件或目录，返回删除数量。
func pruneBackups(dir, prefix, suffix string, retention time.Duration) (int, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, fmt.Errorf("读取备份目录失败: %w", err)
	}

	cutoff := time.Now().Add(-retention)
	removed := 0
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasPrefix(name, prefix) || (suffix != "" && !strings.HasSuffix(name, suffix)) {
			continue
		}
		info, err := entry.Info()
		if err != nil || info.ModTime().After(cutoff) {
			continue
		}
		path := filepath.Join(dir, name)
		if err := os.RemoveAll(path); err != nil {
			return removed, fmt.Errorf("删除过期备份 %s 失败: %w", name, err)
		}
		removed++
	}
	return removed, nil
}
