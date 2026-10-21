// public.go / health.go 的单元测试（任务代号 D18）。
//
// 为什么全部串行：`global.DB` / `global.Config` 是进程级单例，每个用例用 setupEnv 换一套临时库，
// 并行会互相踩数据，所以本文件不写 t.Parallel()。
package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/unicornfairy864/blog-zjut-si/apps/server/global"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/model"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/model/common/dbtime"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/model/common/errs"
)

// ---------------------------------------------------------------------------
// 夹具补充（main_test.go / setting_test.go 里没有的造数手段）
// ---------------------------------------------------------------------------

// updatePost 直改文章列；统一补 updated_at，与 service 写库的口径一致。
func updatePost(t *testing.T, postID int64, fields map[string]any) {
	t.Helper()

	fields["updated_at"] = dbtime.Now()
	require.NoError(t, global.DB.Model(&model.Post{}).Where("id = ?", postID).Updates(fields).Error)
}

// setPublishedAt 把发布时间钉到确定的时间点；insertPost 只会给"现在"，
// 而标签 / 系列 / 相邻文章 / 归档的用例都靠 published_at 排序。
func setPublishedAt(t *testing.T, postID int64, at time.Time) {
	t.Helper()

	updatePost(t, postID, map[string]any{"published_at": at.Unix()})
}

// setPostSeries 把文章挂到某系列并设定 series_order。
func setPostSeries(t *testing.T, postID, seriesID int64, order int) {
	t.Helper()

	updatePost(t, postID, map[string]any{"series_id": seriesID, "series_order": order})
}

// linkPostTag 直插一条 post_tags 关联（post_tags 没有外键约束，写前不必建文章）。
func linkPostTag(t *testing.T, postID, tagID int64) {
	t.Helper()

	require.NoError(t, global.DB.Exec(
		`INSERT INTO post_tags (post_id, tag_id) VALUES (?, ?)`, postID, tagID).Error)
}

// mediaStorageKey 造一个符合 §10 目录约定的 storage_key：{sha[:2]}/{sha256}。
//
// 正文装饰的正则只认小写 hex，所以用真实摘要而不是随手拼的字符串。
func mediaStorageKey(seed string) string {
	sum := sha256.Sum256([]byte(seed))
	hexSum := hex.EncodeToString(sum[:])
	return hexSum[:2] + "/" + hexSum
}

// insertMedia 直插一条 ready 媒体；封面与正文图片装饰都需要"库里查得到"的记录。
func insertMedia(t *testing.T, storageKey, ext string, variants map[string]string, width, height int) *model.Media {
	t.Helper()

	raw, err := json.Marshal(variants)
	require.NoError(t, err)

	now := dbtime.Now()
	media := &model.Media{
		SHA256:        strings.ReplaceAll(storageKey, "/", ""),
		StorageDriver: model.StorageDriverLocal,
		StorageKey:    storageKey,
		OriginalName:  "pic." + ext,
		Mime:          "image/" + ext,
		Ext:           ext,
		Bytes:         2048,
		Width:         width,
		Height:        height,
		Pixels:        int64(width) * int64(height),
		VariantsJSON:  string(raw),
		Blurhash:      "L6Pj0^",
		Alt:           "示例图",
		Status:        model.MediaStatusReady,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	require.NoError(t, global.DB.Create(media).Error)
	require.NotZero(t, media.ID)
	return media
}

// insertPage 直插一条静态页；deleted 为 true 时带上软删时间（公开端应给 410）。
func insertPage(t *testing.T, slug, title, contentHTML string, deleted bool) *model.Page {
	t.Helper()

	now := dbtime.Now()
	page := &model.Page{
		Slug:           slug,
		Title:          title,
		ContentMD:      "# " + title,
		ContentHTML:    contentHTML,
		ContentText:    title + " 正文",
		SEOTitle:       title + " - SEO",
		SEODescription: title + " 的说明",
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if deleted {
		at := now
		page.DeletedAt = &at
	}
	require.NoError(t, global.DB.Create(page).Error)
	require.NotZero(t, page.ID)
	return page
}

// insertLink 直插一条友链。
func insertLink(t *testing.T, name string, sort int, status string) *model.Link {
	t.Helper()

	link := &model.Link{
		Name:        name,
		URL:         "https://example.com/" + name,
		Avatar:      "https://example.com/avatar.png",
		Description: name + " 的说明",
		Sort:        sort,
		Status:      status,
		CreatedAt:   dbtime.Now(),
	}
	require.NoError(t, global.DB.Create(link).Error)
	require.NotZero(t, link.ID)
	return link
}

// insertPendingJobs 造 n 条待执行任务，用来压 readyz 的队列阈值。
func insertPendingJobs(t *testing.T, n int) {
	t.Helper()

	now := dbtime.Now()
	for i := 0; i < n; i++ {
		require.NoError(t, model.CreateJob(global.DB, &model.Job{
			Type:        model.JobTypeCleanup,
			Payload:     "{}",
			RunAt:       now,
			Status:      model.JobStatusPending,
			MaxAttempts: model.DefaultJobMaxAttempts,
			CreatedAt:   now,
		}))
	}
}

// slugs 把公开列表项压成 slug 序列，便于断言排序。
func slugs(items []model.PublicPostListItem) []string {
	out := make([]string, 0, len(items))
	for i := range items {
		out = append(out, items[i].Slug)
	}
	return out
}

// requireAppError / appErrorOf 定义在 main_test.go（多个测试文件共用）。

// ---------------------------------------------------------------------------
// health.go
// ---------------------------------------------------------------------------

func TestHealthService_ReadyHealthy(t *testing.T) {
	setupEnv(t)

	ready, checks := HealthService{}.Ready(context.Background())

	require.True(t, ready, "空库 + 可写 uploads 应当就绪")
	require.Len(t, checks, 4)
	assert.Equal(t, []string{"database", "disk", "uploads", "queue"},
		[]string{checks[0].Name, checks[1].Name, checks[2].Name, checks[3].Name})
	for _, check := range checks {
		assert.True(t, check.OK, "检查项 %s 应当通过", check.Name)
	}

	// database 通过时不带 detail；queue 通过时汇报积压数。
	assert.Empty(t, checks[0].Detail)
	assert.NotEmpty(t, checks[1].Detail)
	assert.Empty(t, checks[2].Detail)
	assert.Equal(t, "积压 0", checks[3].Detail)
}

func TestHealthService_ReadyWithoutDatabase(t *testing.T) {
	setupEnv(t)

	handle := global.DB
	global.DB = nil
	// 恢复进程级单例：用例串行，后一个用例虽然会重新 setupEnv，
	// 但 Cleanup 阶段的 db.Close(handle) 仍按捕获的 handle 走，不受影响。
	t.Cleanup(func() { global.DB = handle })

	ready, checks := HealthService{}.Ready(context.Background())

	assert.False(t, ready, "库未初始化时不能就绪")
	assert.False(t, checks[0].OK)
	assert.Equal(t, "数据库未初始化", checks[0].Detail)
	assert.False(t, checks[3].OK)
	assert.Equal(t, "数据库未初始化", checks[3].Detail)
	assert.True(t, checks[1].OK)
	assert.True(t, checks[2].OK)
}

func TestHealthService_ReadyClosedDatabase(t *testing.T) {
	setupEnv(t)

	// 把连接池关掉：DB 句柄还在，但 Ping 一定失败（模拟库被摘走）。
	sqlDB, err := global.DB.DB()
	require.NoError(t, err)
	require.NoError(t, sqlDB.Close())

	ready, checks := HealthService{}.Ready(context.Background())

	assert.False(t, ready)
	assert.False(t, checks[0].OK)
	assert.Equal(t, "Ping 失败", checks[0].Detail)
	// 库不可用时队列统计也会失败，不能假装积压为 0。
	assert.False(t, checks[3].OK)
	assert.Equal(t, "统计任务失败", checks[3].Detail)
}

func TestHealthService_ReadyQueueBacklog(t *testing.T) {
	setupEnv(t)

	insertPendingJobs(t, readyzQueueMaxPending-1)
	ready, checks := HealthService{}.Ready(context.Background())
	assert.True(t, ready, "积压 %d 个仍未到阈值", readyzQueueMaxPending-1)
	assert.True(t, checks[3].OK)
	assert.Equal(t, "积压 99", checks[3].Detail)

	insertPendingJobs(t, 1)
	ready, checks = HealthService{}.Ready(context.Background())
	assert.False(t, ready, "到阈值即不就绪（>= 而不是 >）")
	assert.False(t, checks[3].OK)
	assert.Equal(t, "积压 100 个任务", checks[3].Detail)
}

func TestHealthService_ReadyUploadsDirUnavailable(t *testing.T) {
	env := setupEnv(t)

	// 目录不存在 → 建临时文件失败；磁盘项在 Windows 上不统计，仍应通过。
	global.Config.Upload.Dir = filepath.Join(env.Dir, "missing-uploads")
	ready, checks := HealthService{}.Ready(context.Background())
	assert.False(t, ready)
	assert.False(t, checks[2].OK)
	assert.Equal(t, "上传目录不可写", checks[2].Detail)

	// 目录配置为空走另一个分支。
	global.Config.Upload.Dir = ""
	ready, checks = HealthService{}.Ready(context.Background())
	assert.False(t, ready)
	assert.False(t, checks[2].OK)
	assert.Equal(t, "未配置上传目录", checks[2].Detail)
}

// ---------------------------------------------------------------------------
// PublicService.Site
// ---------------------------------------------------------------------------

func TestPublicService_SiteCountsOnlyPublished(t *testing.T) {
	setupEnv(t)

	published := insertPost(t, "已发布", "published", model.PostStatusPublished)
	draft := insertPost(t, "草稿", "draft", model.PostStatusDraft)
	insertPost(t, "归档", "archived", model.PostStatusArchived)
	removed := insertPost(t, "已删除", "removed", model.PostStatusPublished)
	updatePost(t, removed.ID, map[string]any{"deleted_at": dbtime.Now()})

	liveTag := insertTag(t, "Go", "go")
	draftTag := insertTag(t, "草稿标签", "draft-tag")
	insertTag(t, "空标签", "empty-tag")
	deadTag := insertTag(t, "回收站标签", "dead-tag")
	linkPostTag(t, published.ID, liveTag.ID)
	linkPostTag(t, draft.ID, draftTag.ID)
	linkPostTag(t, removed.ID, deadTag.ID)

	liveSeries := insertSeries(t, "有文章的系列", "live-series")
	insertSeries(t, "空系列", "empty-series")
	setPostSeries(t, published.ID, liveSeries.ID, 0)

	insertLink(t, "可见友链", 0, model.LinkStatusActive)
	insertLink(t, "隐藏友链", 1, model.LinkStatusHidden)

	info, err := PublicService{}.Site()
	require.NoError(t, err)

	assert.Equal(t, "Asia/Shanghai", info.Timezone)
	assert.Equal(t, "/uploads", info.ImgBaseURL)
	assert.Equal(t, int64(1), info.ContentVersion)
	assert.Equal(t, int64(1), info.Counts.Posts, "只算已发布且未删除")
	assert.Equal(t, int64(1), info.Counts.Tags, "只算挂着已发布文章的标签")
	// 系列清单本身保留空系列（count 为 0），计数口径与 /api/series 一致。
	assert.Equal(t, int64(2), info.Counts.Series)
	assert.Equal(t, int64(1), info.Counts.Links, "只算 active 友链")
}

func TestPublicService_SiteSettingsFallbacks(t *testing.T) {
	setupEnv(t)

	settings := SettingService{}

	// site_timezone 缺键 → 契约缺省值（不能报错）。
	require.NoError(t, global.DB.Exec(
		`DELETE FROM settings WHERE key = ?`, model.SettingSiteTimezone).Error)
	info, err := PublicService{}.Site()
	require.NoError(t, err)
	assert.Equal(t, "Asia/Shanghai", info.Timezone)

	// 空串与纯空白同样退回缺省值（timezoneName 会 TrimSpace）。
	for _, raw := range []string{`""`, `"   "`} {
		putRawSetting(t, model.SettingSiteTimezone, raw)
		info, err = PublicService{}.Site()
		require.NoError(t, err)
		assert.Equal(t, "Asia/Shanghai", info.Timezone)
	}

	// img_base_url 末尾斜杠要被裁掉。
	require.NoError(t, settings.PutString(model.SettingImgBaseURL, "https://cdn.example.com/"))
	info, err = PublicService{}.Site()
	require.NoError(t, err)
	assert.Equal(t, "https://cdn.example.com", info.ImgBaseURL)

	// 空值退回同源相对路径。
	require.NoError(t, settings.PutString(model.SettingImgBaseURL, ""))
	info, err = PublicService{}.Site()
	require.NoError(t, err)
	assert.Equal(t, "/uploads", info.ImgBaseURL)
}

func TestPublicService_SiteRejectsInvalidContentVersion(t *testing.T) {
	setupEnv(t)

	// content_version 存了非数字：Site 必须报错，而不是静默给 0（公开读的 ETag 依赖它）。
	putRawSetting(t, model.SettingContentVersion, `"abc"`)

	_, err := PublicService{}.Site()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "不是 JSON 整数")
}

// ---------------------------------------------------------------------------
// PublicService.Tags / TagPosts
// ---------------------------------------------------------------------------

func TestPublicService_TagsOnlyNonEmpty(t *testing.T) {
	setupEnv(t)

	// 空库：空切片而不是 nil。
	empty, err := PublicService{}.Tags()
	require.NoError(t, err)
	assert.NotNil(t, empty)
	assert.Empty(t, empty)

	busy := insertTag(t, "busy", "busy")
	alpha := insertTag(t, "alpha", "alpha")
	beta := insertTag(t, "beta", "beta")
	draftOnly := insertTag(t, "draft-only", "draft-only")

	first := insertPost(t, "一", "p1", model.PostStatusPublished)
	second := insertPost(t, "二", "p2", model.PostStatusPublished)
	third := insertPost(t, "三", "p3", model.PostStatusPublished)
	draft := insertPost(t, "草稿", "p-draft", model.PostStatusDraft)

	linkPostTag(t, first.ID, busy.ID)
	linkPostTag(t, second.ID, busy.ID)
	linkPostTag(t, third.ID, alpha.ID)
	linkPostTag(t, third.ID, beta.ID)
	linkPostTag(t, draft.ID, draftOnly.ID)

	tags, err := PublicService{}.Tags()
	require.NoError(t, err)
	require.Len(t, tags, 3, "挂着 0 篇已发布文章的标签不出现")
	assert.Equal(t, []string{"busy", "alpha", "beta"},
		[]string{tags[0].Slug, tags[1].Slug, tags[2].Slug})
	assert.Equal(t, int64(2), tags[0].Count)
	assert.Equal(t, "busy", tags[0].Name)
	// count 相同时按名称升序。
	assert.Equal(t, alpha.ID, tags[1].ID)
	assert.Equal(t, int64(1), tags[1].Count)
	assert.Equal(t, beta.ID, tags[2].ID)
}

func TestPublicService_TagPostsPaginationAndMissingTag(t *testing.T) {
	setupEnv(t)

	tag := insertTag(t, "Go", "go")
	insertTag(t, "空", "empty")

	oldest := insertPost(t, "最早", "oldest", model.PostStatusPublished)
	middle := insertPost(t, "中间", "middle", model.PostStatusPublished)
	newest := insertPost(t, "最新", "newest", model.PostStatusPublished)
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	setPublishedAt(t, oldest.ID, base)
	setPublishedAt(t, middle.ID, base.Add(24*time.Hour))
	setPublishedAt(t, newest.ID, base.Add(48*time.Hour))
	for _, post := range []*model.Post{oldest, middle, newest} {
		linkPostTag(t, post.ID, tag.ID)
	}

	// 草稿 / 归档 / 回收站挂着同一个标签：既不进 list 也不进 total。
	draft := insertPost(t, "草稿", "p-draft", model.PostStatusDraft)
	archived := insertPost(t, "归档", "p-archived", model.PostStatusArchived)
	removed := insertPost(t, "已删除", "p-removed", model.PostStatusPublished)
	updatePost(t, removed.ID, map[string]any{"deleted_at": dbtime.Now()})
	for _, post := range []*model.Post{draft, archived, removed} {
		linkPostTag(t, post.ID, tag.ID)
	}

	detail, err := PublicService{}.TagPosts("go", 1, 2)
	require.NoError(t, err)
	assert.Equal(t, tag.ID, detail.Tag.ID)
	assert.Equal(t, "Go", detail.Tag.Name)
	assert.Equal(t, int64(3), detail.Total)
	assert.Equal(t, 1, detail.Page)
	assert.Equal(t, 2, detail.PageSize)
	assert.Equal(t, []string{"newest", "middle"}, slugs(detail.List))

	detail, err = PublicService{}.TagPosts("go", 2, 2)
	require.NoError(t, err)
	assert.Equal(t, int64(3), detail.Total, "翻页不改变 total")
	assert.Equal(t, []string{"oldest"}, slugs(detail.List))

	detail, err = PublicService{}.TagPosts("go", 3, 2)
	require.NoError(t, err)
	assert.Equal(t, int64(3), detail.Total)
	assert.NotNil(t, detail.List, "空页也要是空切片（JSON 出 [] 而不是 null）")
	assert.Empty(t, detail.List)

	// 标签存在但没有已发布文章：不是错误，list 空、total 0。
	detail, err = PublicService{}.TagPosts("empty", 1, 10)
	require.NoError(t, err)
	assert.Equal(t, int64(0), detail.Total)
	assert.NotNil(t, detail.List)
	assert.Empty(t, detail.List)

	// 标签不存在：1002。
	_, err = PublicService{}.TagPosts("no-such-tag", 1, 10)
	requireAppError(t, err, errs.ErrorNotFound)
}

// ---------------------------------------------------------------------------
// PublicService.Series / SeriesDetail
// ---------------------------------------------------------------------------

func TestPublicService_SeriesListKeepsEmptySeries(t *testing.T) {
	setupEnv(t)

	busy := insertSeries(t, "busy", "busy")
	insertSeries(t, "empty", "empty")
	draftOnly := insertSeries(t, "draft-only", "draft-only")

	first := insertPost(t, "一", "s1", model.PostStatusPublished)
	second := insertPost(t, "二", "s2", model.PostStatusPublished)
	draft := insertPost(t, "草稿", "s3", model.PostStatusDraft)
	setPostSeries(t, first.ID, busy.ID, 0)
	setPostSeries(t, second.ID, busy.ID, 0)
	setPostSeries(t, draft.ID, draftOnly.ID, 0)

	items, err := PublicService{}.Series()
	require.NoError(t, err)
	require.Len(t, items, 3, "空系列也保留（count 为 0）")
	assert.Equal(t, busy.ID, items[0].ID)
	assert.Equal(t, int64(2), items[0].Count, "只数已发布文章")
	// count 都是 0 时按名称升序：draft-only < empty。
	assert.Equal(t, []string{"draft-only", "empty"}, []string{items[1].Slug, items[2].Slug})
	assert.Equal(t, int64(0), items[1].Count)
	assert.Equal(t, int64(0), items[2].Count)
}

func TestPublicService_SeriesDetailOrderAndMissingSeries(t *testing.T) {
	setupEnv(t)

	series := insertSeries(t, "从零写博客", "from-zero")
	other := insertSeries(t, "空系列", "empty-series")

	third := insertPost(t, "第三", "s-third", model.PostStatusPublished)
	first := insertPost(t, "第一", "s-first", model.PostStatusPublished)
	second := insertPost(t, "第二", "s-second", model.PostStatusPublished)
	draft := insertPost(t, "草稿", "s-draft", model.PostStatusDraft)
	setPostSeries(t, third.ID, series.ID, 3)
	setPostSeries(t, first.ID, series.ID, 1)
	setPostSeries(t, second.ID, series.ID, 2)
	setPostSeries(t, draft.ID, series.ID, 1)

	// series_order 相同时按 published_at 升序。
	tieA := insertPost(t, "并列 A", "s-tie-a", model.PostStatusPublished)
	tieB := insertPost(t, "并列 B", "s-tie-b", model.PostStatusPublished)
	setPostSeries(t, tieA.ID, series.ID, 4)
	setPostSeries(t, tieB.ID, series.ID, 4)
	base := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	setPublishedAt(t, tieA.ID, base.Add(time.Hour))
	setPublishedAt(t, tieB.ID, base)

	detail, err := PublicService{}.SeriesDetail("from-zero")
	require.NoError(t, err)
	assert.Equal(t, series.ID, detail.Series.ID)
	assert.Equal(t, "从零写博客", detail.Series.Name)
	assert.Equal(t, int64(5), detail.Total, "Total 是该系列已发布文章数（草稿不算）")
	assert.Equal(t, []string{"s-first", "s-second", "s-third", "s-tie-b", "s-tie-a"}, slugs(detail.List))

	// 系列存在但没有已发布文章：空列表而不是错误。
	detail, err = PublicService{}.SeriesDetail("empty-series")
	require.NoError(t, err)
	assert.Equal(t, other.ID, detail.Series.ID)
	assert.Equal(t, int64(0), detail.Total)
	assert.NotNil(t, detail.List)
	assert.Empty(t, detail.List)

	_, err = PublicService{}.SeriesDetail("no-such-series")
	requireAppError(t, err, errs.ErrorNotFound)
}

// ---------------------------------------------------------------------------
// PublicService.Archive
// ---------------------------------------------------------------------------

func TestPublicService_ArchiveGroupsByYearMonth(t *testing.T) {
	setupEnv(t)

	octLate := insertPost(t, "十月下旬", "oct-late", model.PostStatusPublished)
	octEarly := insertPost(t, "十月初", "oct-early", model.PostStatusPublished)
	sep := insertPost(t, "九月", "sep", model.PostStatusPublished)
	lastYear := insertPost(t, "去年", "last-year", model.PostStatusPublished)
	setPublishedAt(t, octLate.ID, time.Date(2026, 10, 15, 3, 0, 0, 0, time.UTC))
	setPublishedAt(t, octEarly.ID, time.Date(2026, 10, 1, 3, 0, 0, 0, time.UTC))
	setPublishedAt(t, sep.ID, time.Date(2026, 9, 20, 3, 0, 0, 0, time.UTC))
	setPublishedAt(t, lastYear.ID, time.Date(2025, 12, 31, 3, 0, 0, 0, time.UTC))

	// 草稿 / 归档 / 回收站都不进归档。
	insertPost(t, "草稿", "a-draft", model.PostStatusDraft)
	insertPost(t, "归档", "a-archived", model.PostStatusArchived)
	removed := insertPost(t, "已删除", "a-removed", model.PostStatusPublished)
	updatePost(t, removed.ID, map[string]any{"deleted_at": dbtime.Now()})

	archive, err := PublicService{}.Archive()
	require.NoError(t, err)
	assert.Equal(t, int64(4), archive.Total)
	require.Len(t, archive.Years, 2)

	assert.Equal(t, 2026, archive.Years[0].Year)
	assert.Equal(t, 3, archive.Years[0].Count)
	require.Len(t, archive.Years[0].Months, 2)
	assert.Equal(t, 10, archive.Years[0].Months[0].Month)
	assert.Equal(t, 2, archive.Years[0].Months[0].Count)
	require.Len(t, archive.Years[0].Months[0].Posts, 2)
	assert.Equal(t, "oct-late", archive.Years[0].Months[0].Posts[0].Slug)
	assert.Equal(t, octLate.ID, archive.Years[0].Months[0].Posts[0].ID)
	assert.Equal(t, "十月下旬", archive.Years[0].Months[0].Posts[0].Title)
	assert.Equal(t, dbtime.FromTime(time.Date(2026, 10, 15, 3, 0, 0, 0, time.UTC)),
		archive.Years[0].Months[0].Posts[0].PublishedAt)
	assert.Equal(t, "oct-early", archive.Years[0].Months[0].Posts[1].Slug)
	assert.Equal(t, 9, archive.Years[0].Months[1].Month)
	assert.Equal(t, 1, archive.Years[0].Months[1].Count)

	assert.Equal(t, 2025, archive.Years[1].Year)
	assert.Equal(t, 1, archive.Years[1].Count)
	require.Len(t, archive.Years[1].Months, 1)
	assert.Equal(t, 12, archive.Years[1].Months[0].Month)
}

func TestPublicService_ArchiveTimezoneSwitchAndFallback(t *testing.T) {
	setupEnv(t)

	// 2024-01-31T20:00:00Z：UTC 还是 1 月，上海已经是 2 月 1 日凌晨。
	post := insertPost(t, "跨月", "cross-month", model.PostStatusPublished)
	setPublishedAt(t, post.ID, time.Date(2024, 1, 31, 20, 0, 0, 0, time.UTC))

	archive, err := PublicService{}.Archive()
	require.NoError(t, err)
	require.Len(t, archive.Years, 1)
	require.Len(t, archive.Years[0].Months, 1)
	assert.Equal(t, 2024, archive.Years[0].Year)
	assert.Equal(t, 2, archive.Years[0].Months[0].Month, "默认 Asia/Shanghai 已跨到 2 月")

	settings := SettingService{}
	require.NoError(t, settings.PutString(model.SettingSiteTimezone, "UTC"))
	archive, err = PublicService{}.Archive()
	require.NoError(t, err)
	assert.Equal(t, 1, archive.Years[0].Months[0].Month, "改成 UTC 后回到 1 月")

	// 非法时区：location() 记一条告警后退回默认时区。
	require.NoError(t, settings.PutString(model.SettingSiteTimezone, "Bad/Zone"))
	archive, err = PublicService{}.Archive()
	require.NoError(t, err)
	assert.Equal(t, 2, archive.Years[0].Months[0].Month)
}

func TestBuildArchiveSkipsPostWithoutPublishedAt(t *testing.T) {
	loc, err := time.LoadLocation("Asia/Shanghai")
	require.NoError(t, err)

	published := dbtime.FromTime(time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC))

	// 防御性分支：Archive 走的 SQL 已经过滤 published_at IS NULL，直接调用才走得到。
	archive := buildArchive([]model.Post{
		{ID: 1, Slug: "no-time", Title: "无发布时间"},
		{ID: 2, Slug: "with-time", Title: "有发布时间", PublishedAt: &published},
	}, loc)

	// Total 取的是入参长度，生产路径上入参已过滤，所以两者一致。
	assert.Equal(t, int64(2), archive.Total)
	require.Len(t, archive.Years, 1)
	assert.Equal(t, 1, archive.Years[0].Count)
	require.Len(t, archive.Years[0].Months, 1)
	require.Len(t, archive.Years[0].Months[0].Posts, 1)
	assert.Equal(t, "with-time", archive.Years[0].Months[0].Posts[0].Slug)
}

func TestPublicService_PublishedWithoutPublishedAtMissesArchive(t *testing.T) {
	setupEnv(t)

	insertPost(t, "正常", "normal", model.PostStatusPublished)
	broken := insertPost(t, "缺发布时间", "broken", model.PostStatusPublished)
	updatePost(t, broken.ID, map[string]any{"published_at": nil})

	// 疑似缺陷：published_at 为 NULL 的"已发布"文章计入 /api/site 的 posts，
	// 也出现在 /api/posts 列表里，却从 /api/archive 消失（归档 SQL 多一个
	// published_at IS NOT NULL），三处口径不一致。正常流程走不到（Publish 必写发布时间），
	// 但脏数据会让归档少一篇、且列表里那篇永远排在最后。
	info, err := PublicService{}.Site()
	require.NoError(t, err)
	assert.Equal(t, int64(2), info.Counts.Posts)

	items, total, err := PublicService{}.Posts(1, 10)
	require.NoError(t, err)
	assert.Equal(t, int64(2), total)
	assert.Len(t, items, 2)

	archive, err := PublicService{}.Archive()
	require.NoError(t, err)
	assert.Equal(t, int64(1), archive.Total)
}

// ---------------------------------------------------------------------------
// PublicService.Posts / postListItems
// ---------------------------------------------------------------------------

func TestPublicService_PostsPinnedFirstAndPaginated(t *testing.T) {
	setupEnv(t)

	oldest := insertPost(t, "最早", "oldest", model.PostStatusPublished)
	newest := insertPost(t, "最新", "newest", model.PostStatusPublished)
	pinned := insertPost(t, "置顶", "pinned", model.PostStatusPublished)
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	setPublishedAt(t, oldest.ID, base)
	setPublishedAt(t, newest.ID, base.Add(48*time.Hour))
	setPublishedAt(t, pinned.ID, base.Add(-24*time.Hour))
	updatePost(t, pinned.ID, map[string]any{"is_pinned": true})

	// 草稿 / 定时 / 归档 / 回收站一律不可见。
	insertPost(t, "草稿", "p-draft", model.PostStatusDraft)
	insertPost(t, "定时", "p-scheduled", model.PostStatusScheduled)
	insertPost(t, "归档", "p-archived", model.PostStatusArchived)
	removed := insertPost(t, "已删除", "p-removed", model.PostStatusPublished)
	updatePost(t, removed.ID, map[string]any{"deleted_at": dbtime.Now()})

	// 置顶优先，其次 published_at 倒序：置顶那篇发布时间最早也排第一。
	items, total, err := PublicService{}.Posts(1, 2)
	require.NoError(t, err)
	assert.Equal(t, int64(3), total)
	assert.Equal(t, []string{"pinned", "newest"}, slugs(items))

	items, total, err = PublicService{}.Posts(2, 2)
	require.NoError(t, err)
	assert.Equal(t, int64(3), total)
	assert.Equal(t, []string{"oldest"}, slugs(items))
}

func TestPublicService_PostListItemsCoverVariantsAndEmptyTags(t *testing.T) {
	setupEnv(t)

	insertPost(t, "无封面无标签", "plain", model.PostStatusPublished)

	full := insertPost(t, "四档封面", "full-cover", model.PostStatusPublished)
	fullMedia := insertMedia(t, mediaStorageKey("full"), "jpg", map[string]string{
		"480": "480.webp", "960": "960.webp", "1600": "1600.webp", "2400": "2400.webp",
	}, 2400, 1200)
	updatePost(t, full.ID, map[string]any{"cover_media_id": fullMedia.ID})

	// 封面 id 指向一条已经不存在的媒体：cover 出 null，但列表项本身不能报错。
	dangling := insertPost(t, "悬空封面", "dangling-cover", model.PostStatusPublished)
	updatePost(t, dangling.ID, map[string]any{"cover_media_id": int64(999999)})

	// 只有一档变体：url 只能取到这一档（正文 srcset 要求 ≥2 档，封面照给 variants）。
	single := insertPost(t, "单档封面", "single-cover", model.PostStatusPublished)
	singleMedia := insertMedia(t, mediaStorageKey("single"), "png", map[string]string{"480": "480.webp"}, 480, 320)
	updatePost(t, single.ID, map[string]any{"cover_media_id": singleMedia.ID})

	// 两篇文章共用同一张封面：批量取封面必须去重（一次 WHERE id IN，禁止 N+1）。
	shared := insertPost(t, "共用封面", "shared-cover", model.PostStatusPublished)
	updatePost(t, shared.ID, map[string]any{"cover_media_id": fullMedia.ID})

	items, total, err := PublicService{}.Posts(1, 10)
	require.NoError(t, err)
	assert.Equal(t, int64(5), total)
	require.Len(t, items, 5)

	bySlug := make(map[string]model.PublicPostListItem, len(items))
	for _, item := range items {
		bySlug[item.Slug] = item
	}

	// 没有标签时 Tags 是空切片而不是 nil（JSON 出 [] 而不是 null）。
	assert.NotNil(t, bySlug["plain"].Tags)
	assert.Empty(t, bySlug["plain"].Tags)
	assert.Nil(t, bySlug["plain"].Cover)

	cover := bySlug["full-cover"].Cover
	require.NotNil(t, cover)
	assert.Equal(t, fullMedia.SHA256, cover.SHA256)
	assert.Equal(t, "jpg", cover.Ext)
	assert.Equal(t, 2400, cover.Width)
	assert.Equal(t, 1200, cover.Height)
	assert.Equal(t, "示例图", cover.Alt)
	assert.Equal(t, "L6Pj0^", cover.Blurhash)
	assert.Equal(t, "/uploads/"+fullMedia.StorageKey+"/1600.webp", cover.URL)
	assert.Equal(t, "/uploads/"+fullMedia.StorageKey+"/original.jpg", cover.OrigURL)
	require.Len(t, cover.Variants, 4)
	for _, tier := range []string{"480", "960", "1600", "2400"} {
		assert.Equal(t, "/uploads/"+fullMedia.StorageKey+"/"+tier+".webp", cover.Variants[tier])
	}

	assert.Nil(t, bySlug["dangling-cover"].Cover, "媒体记录已不在 → cover 为 null")

	// 共用封面的两篇拿到同一份数据（去重只影响查询次数，不影响输出）。
	require.NotNil(t, bySlug["shared-cover"].Cover)
	assert.Equal(t, cover.URL, bySlug["shared-cover"].Cover.URL)

	// 疑似缺陷：variants 里没有 1600 时，toMediaItem 取的是 map 遍历到的第一个键，
	// 而 Go 的 map 遍历顺序随机——只有一档时看不出来，多档（如 480/960）时
	// cover.url 会在几档之间随机跳，前端拿不到稳定地址。
	singleCover := bySlug["single-cover"].Cover
	require.NotNil(t, singleCover)
	assert.Equal(t, "/uploads/"+singleMedia.StorageKey+"/480.webp", singleCover.URL)
	assert.Equal(t, map[string]string{"480": "/uploads/" + singleMedia.StorageKey + "/480.webp"},
		singleCover.Variants)
}

// ---------------------------------------------------------------------------
// PublicService.PostDetail
// ---------------------------------------------------------------------------

func TestPublicService_PostDetailNeighboursOnlyPublished(t *testing.T) {
	setupEnv(t)

	first := insertPost(t, "第一篇", "first", model.PostStatusPublished)
	second := insertPost(t, "第二篇", "second", model.PostStatusPublished)
	third := insertPost(t, "第三篇", "third", model.PostStatusPublished)
	base := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	setPublishedAt(t, first.ID, base)
	setPublishedAt(t, second.ID, base.Add(time.Hour))
	setPublishedAt(t, third.ID, base.Add(2*time.Hour))

	// 草稿即使被塞了 published_at 也不能当邻居（草稿隔离是契约硬要求）。
	draft := insertPost(t, "夹在中间的草稿", "draft-between", model.PostStatusDraft)
	setPublishedAt(t, draft.ID, base.Add(30*time.Minute))

	detail, err := PublicService{}.PostDetail("second")
	require.NoError(t, err)
	require.NotNil(t, detail.Prev)
	require.NotNil(t, detail.Next)
	assert.Equal(t, "first", detail.Prev.Slug)
	assert.Equal(t, "第一篇", detail.Prev.Title)
	assert.Equal(t, "third", detail.Next.Slug)

	detail, err = PublicService{}.PostDetail("first")
	require.NoError(t, err)
	assert.Nil(t, detail.Prev, "最早一篇没有上一篇")
	require.NotNil(t, detail.Next)
	assert.Equal(t, "second", detail.Next.Slug, "中间的草稿不算下一篇")

	detail, err = PublicService{}.PostDetail("third")
	require.NoError(t, err)
	require.NotNil(t, detail.Prev)
	assert.Equal(t, "second", detail.Prev.Slug)
	assert.Nil(t, detail.Next, "最晚一篇没有下一篇")
}

func TestPublicService_PostDetailStatusCodes(t *testing.T) {
	setupEnv(t)

	insertPost(t, "草稿", "draft", model.PostStatusDraft)
	insertPost(t, "定时", "scheduled", model.PostStatusScheduled)
	insertPost(t, "归档", "archived", model.PostStatusArchived)
	removed := insertPost(t, "已删除", "removed", model.PostStatusPublished)
	updatePost(t, removed.ID, map[string]any{"deleted_at": dbtime.Now()})

	// 草稿 / 定时：1002，不泄露存在性；归档 / 已删除：1007。
	_, err := PublicService{}.PostDetail("draft")
	requireAppError(t, err, errs.ErrorNotFound)
	_, err = PublicService{}.PostDetail("scheduled")
	requireAppError(t, err, errs.ErrorNotFound)
	_, err = PublicService{}.PostDetail("archived")
	requireAppError(t, err, errs.ErrorGone)
	_, err = PublicService{}.PostDetail("removed")
	requireAppError(t, err, errs.ErrorGone)
	_, err = PublicService{}.PostDetail("no-such-post")
	requireAppError(t, err, errs.ErrorNotFound)
}

func TestPublicService_PostDetailDecoratesContentImages(t *testing.T) {
	setupEnv(t)

	media := insertMedia(t, mediaStorageKey("decorate"), "jpg", map[string]string{
		"480": "480.webp", "960": "960.webp", "1600": "1600.webp", "2400": "2400.webp",
	}, 2400, 1200)
	archived := insertMedia(t, mediaStorageKey("decorate-archived"), "png", map[string]string{
		"480": "480.webp", "960": "960.webp",
	}, 960, 640)
	require.NoError(t, global.DB.Model(&model.Media{}).Where("id = ?", archived.ID).
		Updates(map[string]any{"orig_archived": true}).Error)

	missingKey := mediaStorageKey("missing-media")
	content := "<p>正文</p>" +
		`<img src="/uploads/` + media.StorageKey + `/1600.webp" alt="图">` +
		`<img src="/uploads/` + archived.StorageKey + `/960.webp" alt="归档原图">` +
		`<img src="https://example.com/outside.png" alt="外链">` +
		`<img src="/uploads/` + missingKey + `/1600.webp" alt="库里没有">`

	post := insertPost(t, "带图文章", "with-images", model.PostStatusPublished)
	updatePost(t, post.ID, map[string]any{"content_html": content})

	detail, err := PublicService{}.PostDetail("with-images")
	require.NoError(t, err)

	// 本站图片补四档 srcset / sizes / 宽高 / 懒加载，并包一层原图直链。
	assert.Contains(t, detail.ContentHTML,
		`srcset="/uploads/`+media.StorageKey+`/480.webp 480w, /uploads/`+media.StorageKey+
			`/960.webp 960w, /uploads/`+media.StorageKey+`/1600.webp 1600w, /uploads/`+
			media.StorageKey+`/2400.webp 2400w"`)
	assert.Contains(t, detail.ContentHTML, `sizes="(min-width: 1024px) 768px, 100vw"`)
	assert.Contains(t, detail.ContentHTML, `width="2400"`)
	assert.Contains(t, detail.ContentHTML, `height="1200"`)
	assert.Contains(t, detail.ContentHTML, `loading="lazy" decoding="async"`)
	assert.Contains(t, detail.ContentHTML,
		`<a class="media-orig" href="/uploads/`+media.StorageKey+
			`/original.jpg" target="_blank" rel="noopener noreferrer">`)

	// 原图被管理端删掉（orig_archived）：srcset 照补，但不给「查看原图」链接。
	assert.Contains(t, detail.ContentHTML,
		`srcset="/uploads/`+archived.StorageKey+`/480.webp 480w, /uploads/`+
			archived.StorageKey+`/960.webp 960w"`)
	assert.NotContains(t, detail.ContentHTML, `href="/uploads/`+archived.StorageKey+`/original.png"`)

	// 外链图片与库里查不到的图片原样保留。
	assert.Contains(t, detail.ContentHTML, `<img src="https://example.com/outside.png" alt="外链">`)
	assert.Contains(t, detail.ContentHTML,
		`<img src="/uploads/`+missingKey+`/1600.webp" alt="库里没有">`)

	// 正文里没有图片：原样返回（DecorateContent 的早退分支）。
	plain := insertPost(t, "无图", "no-images", model.PostStatusPublished)
	plainDetail, err := PublicService{}.PostDetail("no-images")
	require.NoError(t, err)
	assert.Equal(t, plain.ContentHTML, plainDetail.ContentHTML)

	// img_base_url 换成 CDN 前缀后，正则与链接都跟着换（不能硬编码 uploads/）。
	cdn := insertMedia(t, mediaStorageKey("cdn"), "webp", map[string]string{
		"480": "480.webp", "960": "960.webp",
	}, 960, 640)
	require.NoError(t, SettingService{}.PutString(model.SettingImgBaseURL, "https://cdn.example.com"))
	cdnPost := insertPost(t, "CDN 图", "cdn-image", model.PostStatusPublished)
	updatePost(t, cdnPost.ID, map[string]any{
		"content_html": `<img src="https://cdn.example.com/` + cdn.StorageKey + `/960.webp" alt="cdn">`,
	})

	cdnDetail, err := PublicService{}.PostDetail("cdn-image")
	require.NoError(t, err)
	assert.Contains(t, cdnDetail.ContentHTML,
		`<a class="media-orig" href="https://cdn.example.com/`+cdn.StorageKey+`/original.webp"`)
	assert.Contains(t, cdnDetail.ContentHTML,
		`srcset="https://cdn.example.com/`+cdn.StorageKey+`/480.webp 480w, https://cdn.example.com/`+
			cdn.StorageKey+`/960.webp 960w"`)
}

// ---------------------------------------------------------------------------
// PublicService.Related
// ---------------------------------------------------------------------------

func TestPublicService_RelatedByTagOrSeries(t *testing.T) {
	setupEnv(t)

	tag := insertTag(t, "Go", "go")
	series := insertSeries(t, "系列", "series")

	anchor := insertPost(t, "锚点", "anchor", model.PostStatusPublished)
	linkPostTag(t, anchor.ID, tag.ID)
	setPostSeries(t, anchor.ID, series.ID, 0)

	// 一篇靠标签关联，一篇靠系列关联。
	tagMate := insertPost(t, "同标签", "tag-mate", model.PostStatusPublished)
	linkPostTag(t, tagMate.ID, tag.ID)
	seriesMate := insertPost(t, "同系列", "series-mate", model.PostStatusPublished)
	setPostSeries(t, seriesMate.ID, series.ID, 0)

	base := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	setPublishedAt(t, anchor.ID, base)
	setPublishedAt(t, tagMate.ID, base.Add(2*time.Hour))
	setPublishedAt(t, seriesMate.ID, base.Add(time.Hour))

	// 草稿 / 归档 / 回收站即使同标签也不该出现。
	draft := insertPost(t, "草稿", "r-draft", model.PostStatusDraft)
	archived := insertPost(t, "归档", "r-archived", model.PostStatusArchived)
	removed := insertPost(t, "已删除", "r-removed", model.PostStatusPublished)
	updatePost(t, removed.ID, map[string]any{"deleted_at": dbtime.Now()})
	for _, post := range []*model.Post{draft, archived, removed} {
		linkPostTag(t, post.ID, tag.ID)
	}

	items, err := PublicService{}.Related("anchor")
	require.NoError(t, err)
	assert.Equal(t, []string{"tag-mate", "series-mate"}, slugs(items),
		"按发布时间倒序，且排除自身")
}

func TestPublicService_RelatedLimitAndEmptyResult(t *testing.T) {
	setupEnv(t)

	tag := insertTag(t, "Go", "go")
	anchor := insertPost(t, "锚点", "anchor", model.PostStatusPublished)
	linkPostTag(t, anchor.ID, tag.ID)

	base := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 8; i++ {
		post := insertPost(t, "相关", fmt.Sprintf("related-%d", i), model.PostStatusPublished)
		linkPostTag(t, post.ID, tag.ID)
		setPublishedAt(t, post.ID, base.Add(time.Duration(i)*time.Hour))
	}

	items, err := PublicService{}.Related("anchor")
	require.NoError(t, err)
	require.Len(t, items, publicRelatedLimit, "最多 %d 篇", publicRelatedLimit)
	assert.Equal(t, "related-7", items[0].Slug, "取最新的一批")

	// 没有任何关联：空切片而不是错误。
	insertPost(t, "孤立", "lonely", model.PostStatusPublished)
	items, err = PublicService{}.Related("lonely")
	require.NoError(t, err)
	assert.NotNil(t, items, "无关联也要是空切片（JSON 出 []）")
	assert.Empty(t, items)
}

func TestPublicService_RelatedStatusCodes(t *testing.T) {
	setupEnv(t)

	insertPost(t, "草稿", "draft", model.PostStatusDraft)
	insertPost(t, "归档", "archived", model.PostStatusArchived)
	removed := insertPost(t, "已删除", "removed", model.PostStatusPublished)
	updatePost(t, removed.ID, map[string]any{"deleted_at": dbtime.Now()})

	// 与详情同口径：草稿 1002，归档 / 已删除 1007。
	_, err := PublicService{}.Related("draft")
	requireAppError(t, err, errs.ErrorNotFound)
	_, err = PublicService{}.Related("archived")
	requireAppError(t, err, errs.ErrorGone)
	_, err = PublicService{}.Related("removed")
	requireAppError(t, err, errs.ErrorGone)
	_, err = PublicService{}.Related("no-such-post")
	requireAppError(t, err, errs.ErrorNotFound)
}

// ---------------------------------------------------------------------------
// PublicService.Search
// ---------------------------------------------------------------------------

func TestPublicService_SearchHitMissAndEscaping(t *testing.T) {
	setupEnv(t)

	hit := insertPost(t, "Golang 并发模式", "golang-concurrency", model.PostStatusPublished)
	insertPost(t, "Rust 所有权", "rust-ownership", model.PostStatusPublished)
	draft := insertPost(t, "Golang 草稿", "golang-draft", model.PostStatusDraft)
	// LIKE 兜底命中正文纯文本，所以把关键词也放进 content_text。
	updatePost(t, hit.ID, map[string]any{"content_text": "Golang 并发模式 正文内容 并发"})
	updatePost(t, draft.ID, map[string]any{"content_text": "Golang 草稿 正文内容"})

	// ≥3 字走 FTS5（trigram + bm25）。
	items, total, err := PublicService{}.Search("Golang", 1, 10)
	require.NoError(t, err)
	assert.Equal(t, int64(1), total, "草稿不进搜索结果")
	assert.Equal(t, []string{"golang-concurrency"}, slugs(items))

	// 未命中：total 0 + 空切片（不是 nil，也不是错误）。
	items, total, err = PublicService{}.Search("不存在的词", 1, 10)
	require.NoError(t, err)
	assert.Equal(t, int64(0), total)
	assert.NotNil(t, items)
	assert.Empty(t, items)

	// 1–2 字走 LIKE 兜底（trigram 索引不了这么短的串）。
	items, total, err = PublicService{}.Search("并发", 1, 10)
	require.NoError(t, err)
	assert.Equal(t, int64(1), total)
	assert.Equal(t, []string{"golang-concurrency"}, slugs(items))

	// LIKE 通配符被转义：单个 % 不会把整表捞出来。
	items, total, err = PublicService{}.Search("%", 1, 10)
	require.NoError(t, err)
	assert.Equal(t, int64(0), total)
	assert.Empty(t, items)

	// 空关键词：service 不做长度校验（api 层拦 0 字与 >64 字），
	// 落到 LIKE '%%' 会命中全部已发布文章。照现状断言，别误以为是"零结果"。
	items, total, err = PublicService{}.Search("", 1, 10)
	require.NoError(t, err)
	assert.Equal(t, int64(2), total)
	assert.Len(t, items, 2)
}

// ---------------------------------------------------------------------------
// PublicService.Pages / Page
// ---------------------------------------------------------------------------

func TestPublicService_PagesListAndDetail(t *testing.T) {
	setupEnv(t)

	insertPage(t, "beta", "B 页", "<p>B</p>", false)
	alpha := insertPage(t, "alpha", "A 页", "<p>A</p>", false)
	insertPage(t, "gone", "已删页", "<p>gone</p>", true)

	list, err := PublicService{}.Pages()
	require.NoError(t, err)
	require.Len(t, list, 2, "软删页面不进清单")
	assert.Equal(t, []string{"alpha", "beta"}, []string{list[0].Slug, list[1].Slug}, "按 slug 升序")
	assert.Equal(t, "A 页", list[0].Title)
	assert.Equal(t, alpha.UpdatedAt, list[0].UpdatedAt)

	detail, err := PublicService{}.Page("alpha")
	require.NoError(t, err)
	assert.Equal(t, "alpha", detail.Slug)
	assert.Equal(t, "A 页", detail.Title)
	assert.Equal(t, "<p>A</p>", detail.ContentHTML)
	assert.Equal(t, alpha.SEOTitle, detail.SEOTitle)
	assert.Equal(t, alpha.SEODescription, detail.SEODescription)

	_, err = PublicService{}.Page("gone")
	requireAppError(t, err, errs.ErrorGone)

	_, err = PublicService{}.Page("no-such-page")
	requireAppError(t, err, errs.ErrorNotFound)
}

func TestPublicService_PageDecoratesContentImages(t *testing.T) {
	setupEnv(t)

	media := insertMedia(t, mediaStorageKey("page-image"), "png", map[string]string{
		"480": "480.webp", "960": "960.webp",
	}, 960, 640)
	insertPage(t, "about", "关于",
		`<p>插图</p><img src="/uploads/`+media.StorageKey+`/960.webp" alt="插图">`, false)

	detail, err := PublicService{}.Page("about")
	require.NoError(t, err)

	// 静态页正文与文章详情走同一处装饰，否则响应式属性会缺。
	assert.Contains(t, detail.ContentHTML,
		`srcset="/uploads/`+media.StorageKey+`/480.webp 480w, /uploads/`+
			media.StorageKey+`/960.webp 960w"`)
	assert.Contains(t, detail.ContentHTML, `loading="lazy" decoding="async"`)
	assert.Contains(t, detail.ContentHTML,
		`<a class="media-orig" href="/uploads/`+media.StorageKey+`/original.png"`)
}

// ---------------------------------------------------------------------------
// PublicService.Links
// ---------------------------------------------------------------------------

func TestPublicService_LinksOnlyActiveSorted(t *testing.T) {
	setupEnv(t)

	base := insertLink(t, "排序 1 甲", 1, model.LinkStatusActive)
	tie := insertLink(t, "排序 1 乙", 1, model.LinkStatusActive)
	last := insertLink(t, "排序 2", 2, model.LinkStatusActive)
	insertLink(t, "隐藏", 0, model.LinkStatusHidden)

	items, err := PublicService{}.Links()
	require.NoError(t, err)
	require.Len(t, items, 3, "只出 active")
	// sort 升序，其次 id 升序。
	assert.Equal(t, []int64{base.ID, tie.ID, last.ID},
		[]int64{items[0].ID, items[1].ID, items[2].ID})

	// 公开项只出展示字段，不暴露 sort / status。
	assert.Equal(t, model.PublicLinkItem{
		ID:          base.ID,
		Name:        "排序 1 甲",
		URL:         "https://example.com/排序 1 甲",
		Avatar:      "https://example.com/avatar.png",
		Description: "排序 1 甲 的说明",
	}, items[0])
}

// ---------------------------------------------------------------------------
// 故障注入：查询失败必须往上抛，不能静默返回空结果
// ---------------------------------------------------------------------------

func TestPublicService_QueryFailuresAreNotSwallowed(t *testing.T) {
	setupEnv(t)

	insertTag(t, "Go", "go")
	insertSeries(t, "系列", "series")
	insertPost(t, "已发布", "published", model.PostStatusPublished)
	insertPage(t, "about", "关于", "<p>A</p>", false)
	insertLink(t, "友链", 0, model.LinkStatusActive)

	// 把 posts 表整个拿掉：依赖它的聚合必须报错。空列表会被前端当成"站点没有内容"，
	// 所以这里宁可 500 也不能假装成功。
	require.NoError(t, global.DB.Exec(`DROP TABLE posts`).Error)

	_, err := PublicService{}.Site()
	require.Error(t, err, "计数失败要报错")
	_, err = PublicService{}.Tags()
	require.Error(t, err)
	_, err = PublicService{}.Series()
	require.Error(t, err)
	_, err = PublicService{}.Archive()
	require.Error(t, err)
	_, _, err = PublicService{}.Posts(1, 10)
	require.Error(t, err)
	// ≥3 字走 FTS、1–2 字走 LIKE，两条路径都要报错。
	_, _, err = PublicService{}.Search("Golang", 1, 10)
	require.Error(t, err)
	_, _, err = PublicService{}.Search("Go", 1, 10)
	require.Error(t, err)
	_, err = PublicService{}.TagPosts("go", 1, 10)
	require.Error(t, err)
	_, err = PublicService{}.SeriesDetail("series")
	require.Error(t, err)
	_, err = PublicService{}.PostDetail("published")
	require.Error(t, err)
	_, err = PublicService{}.Related("published")
	require.Error(t, err)

	// 没被拆掉的表照常工作：错误只发生在真正查不到的表上。
	detail, err := PublicService{}.Page("about")
	require.NoError(t, err)
	assert.Equal(t, "about", detail.Slug)
	links, err := PublicService{}.Links()
	require.NoError(t, err)
	assert.Len(t, links, 1)
	pages, err := PublicService{}.Pages()
	require.NoError(t, err)
	assert.Len(t, pages, 1)

	// 再拆掉 pages / links / tags / series，覆盖各自的"非 ErrNotFound"分支。
	for _, table := range []string{"pages", "links", "tags", "series"} {
		require.NoError(t, global.DB.Exec(`DROP TABLE `+table).Error)
	}

	_, err = PublicService{}.Page("about")
	require.Error(t, err)
	_, err = PublicService{}.Pages()
	require.Error(t, err)
	_, err = PublicService{}.Links()
	require.Error(t, err)
	_, err = PublicService{}.Tags()
	require.Error(t, err)
	_, err = PublicService{}.Series()
	require.Error(t, err)
	_, err = PublicService{}.TagPosts("go", 1, 10)
	require.Error(t, err, "标签表查不了 → 是查询失败，不是 1002")
	_, err = PublicService{}.SeriesDetail("series")
	require.Error(t, err)
}

func TestPublicService_SettingsTableFailures(t *testing.T) {
	setupEnv(t)

	// settings 表没了：时区读取失败要报错，不能静默用默认时区。
	require.NoError(t, global.DB.Exec(`DROP TABLE settings`).Error)

	_, err := PublicService{}.timezoneName()
	require.Error(t, err)
	_, err = PublicService{}.location()
	require.Error(t, err)
	_, err = PublicService{}.Site()
	require.Error(t, err)
	// Archive 的文章查询没问题，失败发生在解析时区这一步。
	_, err = PublicService{}.Archive()
	require.Error(t, err)
}

func TestPublicService_CountFailuresAreNotSwallowed(t *testing.T) {
	// 四项计数是分别查的：任何一项失败都必须报错，不能一半 0 一半真值。
	for _, table := range []string{"tags", "series", "links"} {
		t.Run(table, func(t *testing.T) {
			setupEnv(t)
			require.NoError(t, global.DB.Exec(`DROP TABLE `+table).Error)

			_, err := PublicService{}.Site()
			require.Error(t, err)
		})
	}
}

func TestPublicService_PostListItemsFailsWhenCoverTableMissing(t *testing.T) {
	setupEnv(t)

	tag := insertTag(t, "Go", "go")
	series := insertSeries(t, "系列", "series")
	post := insertPost(t, "带封面", "with-cover", model.PostStatusPublished)
	linkPostTag(t, post.ID, tag.ID)
	setPostSeries(t, post.ID, series.ID, 0)
	media := insertMedia(t, mediaStorageKey("cover-fail"), "jpg", map[string]string{"480": "480.webp"}, 480, 320)
	updatePost(t, post.ID, map[string]any{"cover_media_id": media.ID})
	// 相关文章至少要有一条，否则 postListItems 会因为空列表提前返回、走不到取封面。
	mate := insertPost(t, "同标签带封面", "cover-mate", model.PostStatusPublished)
	linkPostTag(t, mate.ID, tag.ID)
	updatePost(t, mate.ID, map[string]any{"cover_media_id": media.ID})

	// media 表没了：一次批量取封面失败必须报错，不能把 cover 当 null 悄悄放过。
	require.NoError(t, global.DB.Exec(`DROP TABLE media`).Error)

	_, _, err := PublicService{}.Posts(1, 10)
	require.Error(t, err)
	_, err = PublicService{}.TagPosts("go", 1, 10)
	require.Error(t, err)
	_, err = PublicService{}.SeriesDetail("series")
	require.Error(t, err)
	_, err = PublicService{}.PostDetail("with-cover")
	require.Error(t, err)
	_, err = PublicService{}.Related("with-cover")
	require.Error(t, err)
	_, _, err = PublicService{}.Search("带封面", 1, 10)
	require.Error(t, err)
	_, _, err = PublicService{}.Search("封面", 1, 10)
	require.Error(t, err)
}

func TestPublicService_PostDetailFailsWhenAdjacentQueryBreaks(t *testing.T) {
	setupEnv(t)

	insertPost(t, "已发布", "published", model.PostStatusPublished)

	// 只让"相邻文章"的查询失败：先删掉引用 deleted_at 的部分索引，再把这一列拆掉。
	// 详情自身的查询（SELECT * ... WHERE slug = ?）不引用 deleted_at，所以过得去，
	// 失败点正好落在 FindAdjacentPublishedPosts 上。
	require.NoError(t, global.DB.Exec(`DROP INDEX idx_posts_status_published_at`).Error)
	require.NoError(t, global.DB.Exec(`ALTER TABLE posts DROP COLUMN deleted_at`).Error)

	_, err := PublicService{}.PostDetail("published")
	require.Error(t, err, "相邻文章查不到要报错，而不是把 prev/next 静默当 null")
	assert.Contains(t, err.Error(), "查询相邻文章失败")
}

func TestPublicService_PostListItemsFailsWhenTagTableMissing(t *testing.T) {
	setupEnv(t)

	insertPost(t, "文章", "post", model.PostStatusPublished)

	// post_tags 没了：列表的标签 join 失败要报错，否则列表会静默丢标签。
	require.NoError(t, global.DB.Exec(`DROP TABLE post_tags`).Error)

	_, _, err := PublicService{}.Posts(1, 10)
	require.Error(t, err)
	_, err = PublicService{}.PostDetail("post")
	require.Error(t, err)
	_, err = PublicService{}.Related("post")
	require.Error(t, err, "相关文章的 EXISTS 子查询也依赖 post_tags")
}

// ---------------------------------------------------------------------------
// PublicService.timezoneName / location
// ---------------------------------------------------------------------------

func TestPublicService_TimezoneNameAndLocationFallback(t *testing.T) {
	setupEnv(t)

	public := PublicService{}
	settings := SettingService{}

	// 迁移 000002 种下的默认值。
	name, err := public.timezoneName()
	require.NoError(t, err)
	assert.Equal(t, "Asia/Shanghai", name)
	loc, err := public.location()
	require.NoError(t, err)
	assert.Equal(t, "Asia/Shanghai", loc.String())

	// 缺键 / 空串 / 纯空白都退回缺省值。
	require.NoError(t, global.DB.Exec(
		`DELETE FROM settings WHERE key = ?`, model.SettingSiteTimezone).Error)
	name, err = public.timezoneName()
	require.NoError(t, err)
	assert.Equal(t, "Asia/Shanghai", name)
	for _, raw := range []string{`""`, `"   "`} {
		putRawSetting(t, model.SettingSiteTimezone, raw)
		name, err = public.timezoneName()
		require.NoError(t, err)
		assert.Equal(t, "Asia/Shanghai", name)
	}

	// 合法时区：原样返回，并且 location() 真的换过去。
	require.NoError(t, settings.PutString(model.SettingSiteTimezone, "UTC"))
	name, err = public.timezoneName()
	require.NoError(t, err)
	assert.Equal(t, "UTC", name)
	loc, err = public.location()
	require.NoError(t, err)
	assert.Equal(t, "UTC", loc.String())

	// 疑似缺陷：非法时区名被 timezoneName() 原样返回，只有 location() 会退回默认时区。
	// 于是 /api/site 的 timezone 会输出一个客户端必然解析失败的串，而 /api/archive
	// 已经按 Asia/Shanghai 聚合——同一个设置在两处口径不一致。
	require.NoError(t, settings.PutString(model.SettingSiteTimezone, "Bad/Zone"))
	name, err = public.timezoneName()
	require.NoError(t, err)
	assert.Equal(t, "Bad/Zone", name)

	loc, err = public.location()
	require.NoError(t, err)
	assert.Equal(t, "Asia/Shanghai", loc.String(), "非法时区退回默认")

	info, err := public.Site()
	require.NoError(t, err)
	assert.Equal(t, "Bad/Zone", info.Timezone)
}
