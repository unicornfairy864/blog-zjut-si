package service

import (
	"fmt"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/unicornfairy864/blog-zjut-si/apps/server/global"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/model"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/model/common/dbtime"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/model/common/errs"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/utils"
)

// statsClockOf 取站点时区下的时钟；测试用它算"今天"（stats_daily 按上海日期切日）。
func statsClockOf(t *testing.T, s StatsService) statsClock {
	t.Helper()

	clock, err := s.clock()
	require.NoError(t, err)
	return clock
}

// assertStatsErrCode 断言业务错误码。
func assertStatsErrCode(t *testing.T, err error, code int) {
	t.Helper()

	require.Error(t, err)
	appErr, ok := errs.As(err)
	require.True(t, ok, "应为业务错误，实际: %v", err)
	assert.Equal(t, code, appErr.Code)
}

// statsDailyRow 读一行 stats_daily，缺行直接失败。
func statsDailyRow(t *testing.T, date string, postID int64) *model.StatsDaily {
	t.Helper()

	row, err := model.FindStatsDaily(global.DB, date, postID)
	require.NoError(t, err, "读取 stats_daily(%s, %d)", date, postID)
	return row
}

// statsPostViewCount / statsPostLikeCount 读 posts 上的冗余计数列。
func statsPostViewCount(t *testing.T, postID int64) int {
	t.Helper()

	var count int
	require.NoError(t, global.DB.Raw(`SELECT view_count FROM posts WHERE id = ?`, postID).Scan(&count).Error)
	return count
}

func statsPostLikeCount(t *testing.T, postID int64) int {
	t.Helper()

	var count int
	require.NoError(t, global.DB.Raw(`SELECT like_count FROM posts WHERE id = ?`, postID).Scan(&count).Error)
	return count
}

// statsInsertMedia 直插一条媒体，给看板的状态计数造数据。
func statsInsertMedia(t *testing.T, status string) *model.Media {
	t.Helper()

	now := dbtime.Now()
	media := &model.Media{
		SHA256:        utils.SHA256Hex(fmt.Sprintf("%s-%d", status, int64(now))),
		StorageDriver: model.StorageDriverLocal,
		StorageKey:    status + ".jpg",
		Status:        status,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	require.NoError(t, global.DB.Create(media).Error)
	require.NotZero(t, media.ID)
	return media
}

func TestStatsService_RecordViewCountsPVAndDedupesVisitor(t *testing.T) {
	setupEnv(t)

	post := insertPost(t, "打点", "view-post", model.PostStatusPublished)
	svc := ServiceGroupApp.Stats
	clock := statsClockOf(t, svc)

	first, err := svc.RecordView(ViewInput{Path: "/posts/view-post", Slug: "view-post"}, "visitor-1")
	require.NoError(t, err)
	assert.True(t, first.Recorded)
	require.NotNil(t, first.ViewCount)
	assert.Equal(t, int64(1), *first.ViewCount) // 打点前读到的 0 + 1

	// 明细每次都写；同时落 stats_daily 的文章行与 post_id=0 的全站行。
	assert.Equal(t, int64(1), countRows(t, "page_views", "post_id = ?", post.ID))
	assert.Equal(t, 1, statsPostViewCount(t, post.ID))
	assert.Equal(t, int64(1), statsDailyRow(t, clock.date, post.ID).PV)
	assert.Equal(t, int64(1), statsDailyRow(t, clock.date, post.ID).UV)
	assert.Equal(t, int64(1), statsDailyRow(t, clock.date, model.StatsSitePostID).PV)
	assert.Equal(t, int64(1), statsDailyRow(t, clock.date, model.StatsSitePostID).UV)

	// 第二个访客：PV 与 UV 都 +1。
	second, err := svc.RecordView(ViewInput{Path: "/posts/view-post", Slug: "view-post"}, "visitor-2")
	require.NoError(t, err)
	assert.True(t, second.Recorded)
	require.NotNil(t, second.ViewCount)
	assert.Equal(t, int64(2), *second.ViewCount)
	assert.Equal(t, int64(2), statsDailyRow(t, clock.date, post.ID).PV)
	assert.Equal(t, int64(2), statsDailyRow(t, clock.date, post.ID).UV)
	assert.Equal(t, int64(2), statsDailyRow(t, clock.date, model.StatsSitePostID).UV)

	// 同一访客 10 分钟内再次打点：明细仍写，但 PV 不 +1、Recorded=false。
	third, err := svc.RecordView(ViewInput{Path: "/posts/view-post", Slug: "view-post"}, "visitor-1")
	require.NoError(t, err)
	assert.False(t, third.Recorded)
	require.NotNil(t, third.ViewCount)
	assert.Equal(t, int64(2), *third.ViewCount) // 重新读到的当前阅读数
	assert.Equal(t, int64(3), countRows(t, "page_views", "post_id = ?", post.ID))
	assert.Equal(t, 2, statsPostViewCount(t, post.ID))
	assert.Equal(t, int64(2), statsDailyRow(t, clock.date, post.ID).PV)

	// 把该访客的明细回拨到 10 分钟窗口外：同一访客应重新计 PV（去重窗口 = 10 分钟）。
	require.NoError(t, global.DB.Exec(
		`UPDATE page_views SET created_at = ? WHERE visitor_hash = ?`,
		int64(dbtime.Now().Add(-11*time.Minute)), utils.HashVisitor("visitor-1")).Error)

	fourth, err := svc.RecordView(ViewInput{Path: "/posts/view-post", Slug: "view-post"}, "visitor-1")
	require.NoError(t, err)
	assert.True(t, fourth.Recorded)
	require.NotNil(t, fourth.ViewCount)
	assert.Equal(t, int64(3), *fourth.ViewCount)
	assert.Equal(t, int64(3), statsDailyRow(t, clock.date, post.ID).PV)
	assert.Equal(t, int64(2), statsDailyRow(t, clock.date, post.ID).UV) // 去重后仍是 2 个访客
	// 明细只落访客哈希，不落明文种子。
	assert.Equal(t, int64(0), countRows(t, "page_views", "visitor_hash = ?", "visitor-1"))
}

func TestStatsService_RecordViewNonArticlePagesAndReferrer(t *testing.T) {
	setupEnv(t)

	svc := ServiceGroupApp.Stats
	clock := statsClockOf(t, svc)

	// 空 slug：只计全站行，ViewCount 为 null。
	res, err := svc.RecordView(ViewInput{Path: "/about", Referrer: "https://Example.com/a?b=1"}, "anon-1")
	require.NoError(t, err)
	assert.True(t, res.Recorded)
	assert.Nil(t, res.ViewCount)
	assert.Equal(t, int64(1), countRows(t, "page_views", "post_id IS NULL AND path = ?", "/about"))

	// referrer 只存 host（不存完整 URL），并统一小写（D18 修复）：DNS 域名大小写不敏感，
	// 不归一的话 "Example.com" 与 "example.com" 会被当成两个来源，来源统计凭空多一份。
	var host string
	require.NoError(t, global.DB.Raw(`SELECT referrer_host FROM page_views WHERE path = ?`, "/about").Scan(&host).Error)
	assert.Equal(t, "example.com", host)

	// 同 visitor 同 path 10 分钟内去重；换 path 则另计。
	samePath, err := svc.RecordView(ViewInput{Path: "/about"}, "anon-1")
	require.NoError(t, err)
	assert.False(t, samePath.Recorded)
	otherPath, err := svc.RecordView(ViewInput{Path: "/contact"}, "anon-1")
	require.NoError(t, err)
	assert.True(t, otherPath.Recorded)
	assert.Equal(t, int64(2), statsDailyRow(t, clock.date, model.StatsSitePostID).PV)
	assert.Equal(t, int64(1), statsDailyRow(t, clock.date, model.StatsSitePostID).UV)
	// 非文章页不产生文章行。
	assert.Equal(t, int64(0), countRows(t, "stats_daily", "post_id <> ?", model.StatsSitePostID))

	// path 为空 → 归一为 "/"；解析不了的 referrer 落空串。
	_, err = svc.RecordView(ViewInput{Path: "   ", Referrer: "://bad"}, "anon-2")
	require.NoError(t, err)
	assert.Equal(t, int64(1), countRows(t, "page_views", "path = ?", "/"))
	require.NoError(t, global.DB.Raw(`SELECT referrer_host FROM page_views WHERE visitor_hash = ?`,
		utils.HashVisitor("anon-2")).Scan(&host).Error)
	assert.Empty(t, host)

	// 超长 path 按字符截断到 statsPathMaxLen(200)，中文不能被切坏。
	_, err = svc.RecordView(ViewInput{Path: "/" + strings.Repeat("路", 300)}, "anon-3")
	require.NoError(t, err)
	var storedPath string
	require.NoError(t, global.DB.Raw(`SELECT path FROM page_views WHERE visitor_hash = ?`,
		utils.HashVisitor("anon-3")).Scan(&storedPath).Error)
	assert.Equal(t, 200, utf8.RuneCountInString(storedPath))
	assert.True(t, strings.HasPrefix(storedPath, "/路"))

	// 超长 referrer host 同样截断到 statsReferrerHostMaxLen(100)。
	_, err = svc.RecordView(ViewInput{
		Path:     "/long-referrer",
		Referrer: "https://" + strings.Repeat("a", 120) + ".com/x",
	}, "anon-4")
	require.NoError(t, err)
	require.NoError(t, global.DB.Raw(`SELECT referrer_host FROM page_views WHERE visitor_hash = ?`,
		utils.HashVisitor("anon-4")).Scan(&host).Error)
	assert.Equal(t, 100, utf8.RuneCountInString(host))

	// 文章不存在 / 未发布 / 已归档：打点降级为只计全站，不失败（状态变化不该让打点报错）。
	draft := insertPost(t, "草稿", "draft-post", model.PostStatusDraft)
	archived := insertPost(t, "归档", "archived-post", model.PostStatusArchived)
	for i, slug := range []string{"no-such-post", "draft-post", "archived-post"} {
		path := "/p/" + slug
		res, err := svc.RecordView(ViewInput{Path: path, Slug: slug}, fmt.Sprintf("degrade-%d", i))
		require.NoError(t, err, "slug=%s", slug)
		assert.True(t, res.Recorded, "slug=%s", slug)
		assert.Nil(t, res.ViewCount, "slug=%s", slug)
		assert.Equal(t, int64(1), countRows(t, "page_views", "post_id IS NULL AND path = ?", path))
	}
	assert.Zero(t, statsPostViewCount(t, draft.ID))
	assert.Zero(t, statsPostViewCount(t, archived.ID))
}

func TestStatsService_RecordViewUsesSiteTimezoneDate(t *testing.T) {
	setupEnv(t)

	// 站点时区改成 UTC+14：stats_daily 的 date 必须跟着站点时区走。
	require.NoError(t, ServiceGroupApp.Setting.PutString(model.SettingSiteTimezone, "Pacific/Kiritimati"))
	svc := ServiceGroupApp.Stats
	clock := statsClockOf(t, svc)
	assert.Equal(t, "Pacific/Kiritimati", clock.loc.String())
	assert.Equal(t, time.Now().In(clock.loc).Format(statsDateLayout), clock.date)

	_, err := svc.RecordView(ViewInput{Path: "/tz"}, "tz-visitor")
	require.NoError(t, err)
	assert.Equal(t, int64(1), countRows(t, "stats_daily", "date = ?", clock.date))
	assert.Equal(t, int64(0), countRows(t, "stats_daily", "date <> ?", clock.date))

	// 时区值非法 → 回退到契约默认时区 Asia/Shanghai，而不是让打点失败。
	require.NoError(t, ServiceGroupApp.Setting.PutString(model.SettingSiteTimezone, "Not/AZone"))
	clock = statsClockOf(t, svc)
	assert.Equal(t, "Asia/Shanghai", clock.loc.String())

	// 时区键整行缺失 → 同样落到契约缺省值（空串不能拿去 LoadLocation）。
	require.NoError(t, global.DB.Exec(`DELETE FROM settings WHERE key = ?`, model.SettingSiteTimezone).Error)
	clock = statsClockOf(t, svc)
	assert.Equal(t, "Asia/Shanghai", clock.loc.String())

	// 时区值不是 JSON 字符串 → 读设置失败，clock() 必须把错误透出去（不能静默用默认时区）。
	require.NoError(t, ServiceGroupApp.Setting.PutString(model.SettingSiteTimezone, "Asia/Shanghai"))
	putRawSetting(t, model.SettingSiteTimezone, "1")
	_, err = svc.clock()
	require.Error(t, err)
}

func TestStatsService_LikeUnlikeIdempotentAndSyncsPostCount(t *testing.T) {
	setupEnv(t)

	post := insertPost(t, "点赞", "like-post", model.PostStatusPublished)
	svc := ServiceGroupApp.Stats
	before := post.UpdatedAt

	first, err := svc.Like("like-post", "liker-1")
	require.NoError(t, err)
	assert.True(t, first.Liked)
	assert.Equal(t, int64(1), first.LikeCount)
	assert.Equal(t, int64(1), countRows(t, "likes", "post_id = ?", post.ID))
	assert.Equal(t, 1, statsPostLikeCount(t, post.ID))

	// 幂等：同访客重复点赞不重复计数。
	again, err := svc.Like("like-post", "liker-1")
	require.NoError(t, err)
	assert.True(t, again.Liked)
	assert.Equal(t, int64(1), again.LikeCount)
	assert.Equal(t, int64(1), countRows(t, "likes", "post_id = ?", post.ID))

	// 第二个访客。
	other, err := svc.Like("like-post", "liker-2")
	require.NoError(t, err)
	assert.Equal(t, int64(2), other.LikeCount)
	assert.Equal(t, 2, statsPostLikeCount(t, post.ID))

	// 点赞不该动 posts.updated_at（同步计数用 UpdateColumn）。
	reloaded, err := model.FindPostByID(global.DB, post.ID, true)
	require.NoError(t, err)
	assert.Equal(t, before, reloaded.UpdatedAt)

	// 取消点赞：计数回落；重复取消同样幂等。
	unlike, err := svc.Unlike("like-post", "liker-1")
	require.NoError(t, err)
	assert.False(t, unlike.Liked)
	assert.Equal(t, int64(1), unlike.LikeCount)
	assert.Equal(t, 1, statsPostLikeCount(t, post.ID))

	unlike, err = svc.Unlike("like-post", "liker-1")
	require.NoError(t, err)
	assert.Equal(t, int64(1), unlike.LikeCount)

	unlike, err = svc.Unlike("like-post", "liker-2")
	require.NoError(t, err)
	assert.False(t, unlike.Liked)
	assert.Zero(t, unlike.LikeCount)
	assert.Zero(t, statsPostLikeCount(t, post.ID))
	assert.Equal(t, int64(0), countRows(t, "likes", "post_id = ?", post.ID))
}

func TestStatsService_InteractionOnMissingOrUnpublishedPost(t *testing.T) {
	setupEnv(t)

	insertPost(t, "草稿", "draft-post", model.PostStatusDraft)
	insertPost(t, "归档", "archived-post", model.PostStatusArchived)
	svc := ServiceGroupApp.Stats

	// 文章不存在 / 草稿 → 1002；归档 → 1007（与详情同口径）。
	_, err := svc.Like("no-such-post", "v")
	assertStatsErrCode(t, err, errs.ErrorNotFound)
	_, err = svc.Like("draft-post", "v")
	assertStatsErrCode(t, err, errs.ErrorNotFound)
	_, err = svc.Like("archived-post", "v")
	assertStatsErrCode(t, err, errs.ErrorGone)

	_, err = svc.Unlike("no-such-post", "v")
	assertStatsErrCode(t, err, errs.ErrorNotFound)
	_, err = svc.Unlike("draft-post", "v")
	assertStatsErrCode(t, err, errs.ErrorNotFound)
	_, err = svc.Unlike("archived-post", "v")
	assertStatsErrCode(t, err, errs.ErrorGone)

	_, err = svc.PostStats("no-such-post", "v")
	assertStatsErrCode(t, err, errs.ErrorNotFound)
	_, err = svc.PostStats("draft-post", "v")
	assertStatsErrCode(t, err, errs.ErrorNotFound)
	_, err = svc.PostStats("archived-post", "v")
	assertStatsErrCode(t, err, errs.ErrorGone)

	// 失败的点赞 / 取消不该留下 likes 行。
	assert.Equal(t, int64(0), countRows(t, "likes", ""))
}

func TestStatsService_PostStatsReportsVisitorState(t *testing.T) {
	setupEnv(t)

	insertPost(t, "统计", "stats-post", model.PostStatusPublished)
	svc := ServiceGroupApp.Stats

	_, err := svc.RecordView(ViewInput{Path: "/posts/stats-post", Slug: "stats-post"}, "reader-1")
	require.NoError(t, err)
	_, err = svc.Like("stats-post", "reader-1")
	require.NoError(t, err)

	mine, err := svc.PostStats("stats-post", "reader-1")
	require.NoError(t, err)
	assert.Equal(t, int64(1), mine.ViewCount)
	assert.Equal(t, int64(1), mine.LikeCount)
	assert.True(t, mine.Liked)

	// 另一个访客：计数相同，但"我点过没"是 false（访客态）。
	other, err := svc.PostStats("stats-post", "reader-2")
	require.NoError(t, err)
	assert.Equal(t, int64(1), other.ViewCount)
	assert.Equal(t, int64(1), other.LikeCount)
	assert.False(t, other.Liked)
}

func TestStatsService_OverviewAggregatesDashboardCounts(t *testing.T) {
	setupEnv(t)

	pub1 := insertPost(t, "已发布一", "pub-1", model.PostStatusPublished)
	pub2 := insertPost(t, "已发布二", "pub-2", model.PostStatusPublished)
	insertPost(t, "草稿", "draft-1", model.PostStatusDraft)
	insertPost(t, "定时", "sched-1", model.PostStatusScheduled)
	insertPost(t, "归档", "arch-1", model.PostStatusArchived)
	// 回收站文章：算 deleted，不算 published。
	trashed := insertPost(t, "回收站", "trash-1", model.PostStatusPublished)
	require.NoError(t, global.DB.Model(trashed).Update("deleted_at", dbtime.Now()).Error)

	// 评论：2 pending / 1 approved / 1 spam。
	commentInsert(t, pub1.ID, nil, nil, model.CommentStatusPending, "待审一")
	commentInsert(t, pub1.ID, nil, nil, model.CommentStatusPending, "待审二")
	commentInsert(t, pub1.ID, nil, nil, model.CommentStatusApproved, "已过审")
	commentInsert(t, pub1.ID, nil, nil, model.CommentStatusSpam, "垃圾")

	// 媒体：ready / processing / failed 各一。
	statsInsertMedia(t, model.MediaStatusReady)
	statsInsertMedia(t, model.MediaStatusProcessing)
	statsInsertMedia(t, model.MediaStatusFailed)

	svc := ServiceGroupApp.Stats
	clock := statsClockOf(t, svc)

	// 一次打点 + 今天一个赞 + 昨天一个赞。
	_, err := svc.RecordView(ViewInput{Path: "/posts/pub-1", Slug: "pub-1"}, "ov-visitor")
	require.NoError(t, err)
	_, err = svc.Like("pub-1", "ov-liker")
	require.NoError(t, err)
	require.NoError(t, global.DB.Exec(
		`INSERT INTO likes (post_id, visitor_hash, created_at) VALUES (?, ?, ?)`,
		pub2.ID, utils.HashVisitor("old-liker"), int64(clock.start.Add(-time.Hour))).Error)

	overview, err := svc.Overview()
	require.NoError(t, err)

	assert.Equal(t, int64(2), overview.Posts.Published)
	assert.Equal(t, int64(1), overview.Posts.Draft)
	assert.Equal(t, int64(1), overview.Posts.Scheduled)
	assert.Equal(t, int64(1), overview.Posts.Archived)
	assert.Equal(t, int64(1), overview.Posts.Deleted)

	assert.Equal(t, int64(2), overview.Comments.Pending)
	assert.Equal(t, int64(1), overview.Comments.Approved)
	assert.Equal(t, int64(1), overview.Comments.Spam)

	assert.Equal(t, int64(1), overview.Media.Ready)
	assert.Equal(t, int64(1), overview.Media.Processing)
	assert.Equal(t, int64(1), overview.Media.Failed)

	assert.Equal(t, int64(1), overview.Views.Total)
	assert.Equal(t, int64(1), overview.Views.Today)
	assert.Equal(t, int64(1), overview.Views.UVToday)

	assert.Equal(t, int64(2), overview.Likes.Total)
	assert.Equal(t, int64(1), overview.Likes.Today)

	// Top 榜只出已发布文章，按 view_count 倒序。
	require.NotEmpty(t, overview.TopPosts)
	assert.Equal(t, "pub-1", overview.TopPosts[0].Slug)
	assert.Equal(t, int64(1), overview.TopPosts[0].ViewCount)
	assert.Equal(t, int64(1), overview.TopPosts[0].LikeCount)
}

func TestStatsService_DailyRangeFilterAndValidation(t *testing.T) {
	setupEnv(t)

	svc := ServiceGroupApp.Stats
	clock := statsClockOf(t, svc)
	day := func(offset int) string { return clock.now.AddDate(0, 0, offset).Format(statsDateLayout) }

	// 今天 / 昨天 / 40 天前各一行全站数据；UV 只给昨天填，验证合计。
	require.NoError(t, model.UpsertStatsDailyPV(global.DB, day(0), model.StatsSitePostID, 5))
	require.NoError(t, model.UpsertStatsDailyPV(global.DB, day(-1), model.StatsSitePostID, 3))
	require.NoError(t, model.UpdateStatsDailyUV(global.DB, day(-1), model.StatsSitePostID, 2))
	require.NoError(t, model.UpsertStatsDailyPV(global.DB, day(-40), model.StatsSitePostID, 7))
	// 文章行不该混进日粒度列表（只出 post_id=0 的全站行）。
	require.NoError(t, model.UpsertStatsDailyPV(global.DB, day(0), 12345, 9))

	// 缺省区间：最近 30 天（含今天）。
	daily, err := svc.Daily("", "")
	require.NoError(t, err)
	assert.Equal(t, day(-29), daily.From)
	assert.Equal(t, day(0), daily.To)
	assert.Equal(t, int64(8), daily.TotalPV) // 5 + 3；40 天前那行在区间外
	assert.Equal(t, int64(2), daily.TotalUV)
	require.Len(t, daily.List, 2)
	assert.Equal(t, day(-1), daily.List[0].Date) // 按日期升序
	assert.Equal(t, int64(3), daily.List[0].PV)
	assert.Equal(t, int64(2), daily.List[0].UV)
	assert.Equal(t, day(0), daily.List[1].Date)
	assert.Equal(t, int64(5), daily.List[1].PV)

	// 显式区间：只取昨天。
	daily, err = svc.Daily(day(-1), day(-1))
	require.NoError(t, err)
	assert.Equal(t, day(-1), daily.From)
	assert.Equal(t, day(-1), daily.To)
	assert.Equal(t, int64(3), daily.TotalPV)
	require.Len(t, daily.List, 1)

	// 无数据的未来区间：空数组、合计 0，不报错。
	daily, err = svc.Daily(day(5), day(6))
	require.NoError(t, err)
	assert.Zero(t, daily.TotalPV)
	assert.Zero(t, daily.TotalUV)
	assert.NotNil(t, daily.List)
	assert.Empty(t, daily.List)

	// 区间边界：366 天（含首尾）允许，367 天拒绝。
	_, err = svc.Daily(day(-365), day(0))
	require.NoError(t, err)
	_, err = svc.Daily(day(-366), day(0))
	assertStatsErrCode(t, err, errs.ErrorValidation)
	assert.Contains(t, err.Error(), "366")

	// 非法日期参数：from / to 分别报字段名。
	_, err = svc.Daily("2024-13-01", day(0))
	assertStatsErrCode(t, err, errs.ErrorValidation)
	assert.Contains(t, err.Error(), "from")
	_, err = svc.Daily(day(-1), "not-a-date")
	assertStatsErrCode(t, err, errs.ErrorValidation)
	assert.Contains(t, err.Error(), "to")

	// to 早于 from。
	_, err = svc.Daily(day(0), day(-1))
	assertStatsErrCode(t, err, errs.ErrorValidation)
	assert.Contains(t, err.Error(), "不能早于")
}

func TestStatsService_ClockTodayRowAndViewTargetEdges(t *testing.T) {
	setupEnv(t)

	svc := ServiceGroupApp.Stats
	clock := statsClockOf(t, svc)

	// 默认站点时区来自迁移种下的 settings.site_timezone；日界是当天 00:00 与次日 00:00。
	assert.Equal(t, "Asia/Shanghai", clock.loc.String())
	assert.Equal(t, clock.now.Format(statsDateLayout), clock.date)
	assert.Equal(t, 24*time.Hour, clock.end.Sub(clock.start))
	assert.Equal(t, 0, clock.start.Time().In(clock.loc).Hour())
	assert.Equal(t, clock.date, clock.start.Time().In(clock.loc).Format(statsDateLayout))

	// 当天没有行 → 0/0，不报错。
	pv, uv := svc.todayRow(clock)
	assert.Zero(t, pv)
	assert.Zero(t, uv)

	require.NoError(t, model.UpsertStatsDailyPV(global.DB, clock.date, model.StatsSitePostID, 4))
	require.NoError(t, model.UpdateStatsDailyUV(global.DB, clock.date, model.StatsSitePostID, 2))
	pv, uv = svc.todayRow(clock)
	assert.Equal(t, int64(4), pv)
	assert.Equal(t, int64(2), uv)

	// viewTarget：空 slug、非文章页、草稿、归档、不存在都降级为 nil（只计全站）。
	published := insertPost(t, "目标", "target-post", model.PostStatusPublished)
	insertPost(t, "草稿", "target-draft", model.PostStatusDraft)
	insertPost(t, "归档", "target-archived", model.PostStatusArchived)
	for _, slug := range []string{"", "   ", "missing-post", "target-draft", "target-archived"} {
		post, err := svc.viewTarget(slug)
		require.NoError(t, err, "slug=%q", slug)
		assert.Nil(t, post, "slug=%q 应降级为只计全站", slug)
	}

	// 命中已发布文章（slug 前后空白会被裁掉）。
	post, err := svc.viewTarget("  target-post  ")
	require.NoError(t, err)
	require.NotNil(t, post)
	assert.Equal(t, published.ID, post.ID)
}
