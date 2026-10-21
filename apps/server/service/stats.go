package service

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/unicornfairy864/blog-zjut-si/apps/server/global"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/model"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/model/common/dbtime"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/model/common/errs"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/utils"
)

// StatsService 处理点赞、PV/UV 与统计看板（agent.md §5 / §16 G5）。
//
// 口径（§5「互动与统计口径」）：
//   - 点赞幂等，`posts.like_count` 每次变更后按 likes 表重算；
//   - 每次打点都写 page_views 明细，但同 visitor 同目标 10 分钟内 PV 只 +1；
//   - UV 不增量近似，按上海日期 `COUNT(DISTINCT visitor_hash)` 重算；
//   - 计入时同时写 stats_daily 的文章行与 post_id=0 全站行。
type StatsService struct{}

const (
	// statsViewDedupeWindow 是同 visitor 同目标的 PV 去重窗口（§5）。
	statsViewDedupeWindow = 10 * time.Minute
	// statsPathMaxLen 是落库 path 的字符上限。
	statsPathMaxLen = 200
	// statsReferrerHostMaxLen 是落库 referrer host 的字符上限。
	statsReferrerHostMaxLen = 100
	// statsDateLayout 是 stats_daily.date 的格式（上海日期）。
	statsDateLayout = "2006-01-02"
	// statsEarliestDate 用于「累计」查询的下界，比任何真实日期都小。
	statsEarliestDate = "0000-01-01"
	// statsDefaultRangeDays 是管理端日粒度接口的缺省区间长度（§5）。
	statsDefaultRangeDays = 30
	// statsMaxRangeDays 是管理端日粒度接口允许的最大区间（§5）。
	statsMaxRangeDays = 366
	// statsTopPostLimit 是看板热门文章的条数。
	statsTopPostLimit = 5
)

// LikeState 是点赞 / 取消点赞的输出（§5）。
type LikeState struct {
	Liked     bool  `json:"liked"`
	LikeCount int64 `json:"likeCount"`
}

// PostStats 是 GET /posts/{slug}/stats 的输出（访客态，§5）。
type PostStats struct {
	ViewCount int64 `json:"viewCount"`
	LikeCount int64 `json:"likeCount"`
	Liked     bool  `json:"liked"`
}

// ViewInput 是一次 PV 打点；格式校验在 api 层完成。
type ViewInput struct {
	Path     string
	Slug     string
	Referrer string
}

// ViewResult 是打点结果；ViewCount 仅在命中已发布文章时非空（§5）。
type ViewResult struct {
	Recorded  bool   `json:"recorded"`
	ViewCount *int64 `json:"viewCount"`
}

// AdminStatsOverview 是 GET /api/admin/stats/overview 的输出（§5）。
type AdminStatsOverview struct {
	Posts    AdminPostCounts       `json:"posts"`
	Comments AdminCommentCounts    `json:"comments"`
	Media    AdminMediaCounts      `json:"media"`
	Views    AdminViewCounts       `json:"views"`
	Likes    AdminLikeCounts       `json:"likes"`
	TopPosts []model.TopViewedPost `json:"topPosts"`
}

// AdminPostCounts 是文章状态计数。
type AdminPostCounts struct {
	Published int64 `json:"published"`
	Draft     int64 `json:"draft"`
	Scheduled int64 `json:"scheduled"`
	Archived  int64 `json:"archived"`
	Deleted   int64 `json:"deleted"`
}

// AdminCommentCounts 是评论状态计数。
type AdminCommentCounts struct {
	Pending  int64 `json:"pending"`
	Approved int64 `json:"approved"`
	Spam     int64 `json:"spam"`
}

// AdminMediaCounts 是媒体状态计数。
type AdminMediaCounts struct {
	Ready      int64 `json:"ready"`
	Processing int64 `json:"processing"`
	Failed     int64 `json:"failed"`
}

// AdminViewCounts 是 PV / UV 计数。
type AdminViewCounts struct {
	Total   int64 `json:"total"`
	Today   int64 `json:"today"`
	UVToday int64 `json:"uvToday"`
}

// AdminLikeCounts 是点赞计数。
type AdminLikeCounts struct {
	Total int64 `json:"total"`
	Today int64 `json:"today"`
}

// AdminStatsDaily 是 GET /api/admin/stats/daily 的输出（§5）。
type AdminStatsDaily struct {
	From    string             `json:"from"`
	To      string             `json:"to"`
	TotalPV int64              `json:"totalPv"`
	TotalUV int64              `json:"totalUv"`
	List    []AdminStatsDayRow `json:"list"`
}

// AdminStatsDayRow 是日粒度里的一天。
type AdminStatsDayRow struct {
	Date string `json:"date"`
	PV   int64  `json:"pv"`
	UV   int64  `json:"uv"`
}

// statsClock 是一次统计写入所需的时间上下文：站点时区、当天日期与日界。
type statsClock struct {
	loc   *time.Location
	now   time.Time
	date  string
	start dbtime.Unix
	end   dbtime.Unix
}

// Like 点赞（幂等）：已点过不重复计数，返回最新的点赞数（§5）。
func (s StatsService) Like(slug, visitorSeed string) (LikeState, error) {
	post, err := ServiceGroupApp.Post.PublicBySlug(slug)
	if err != nil {
		return LikeState{}, err
	}

	visitorHash := utils.HashVisitor(visitorSeed)
	if _, err := model.InsertLike(global.DB, post.ID, visitorHash, dbtime.Now()); err != nil {
		return LikeState{}, fmt.Errorf("写入点赞失败: %w", err)
	}

	count, err := model.SyncPostLikeCount(global.DB, post.ID)
	if err != nil {
		return LikeState{}, fmt.Errorf("重算点赞数失败: %w", err)
	}
	return LikeState{Liked: true, LikeCount: count}, nil
}

// Unlike 取消点赞（幂等）：没点过也返回 200，返回最新的点赞数（§5）。
func (s StatsService) Unlike(slug, visitorSeed string) (LikeState, error) {
	post, err := ServiceGroupApp.Post.PublicBySlug(slug)
	if err != nil {
		return LikeState{}, err
	}

	visitorHash := utils.HashVisitor(visitorSeed)
	if _, err := model.DeleteLike(global.DB, post.ID, visitorHash); err != nil {
		return LikeState{}, fmt.Errorf("取消点赞失败: %w", err)
	}

	count, err := model.SyncPostLikeCount(global.DB, post.ID)
	if err != nil {
		return LikeState{}, fmt.Errorf("重算点赞数失败: %w", err)
	}
	return LikeState{Liked: false, LikeCount: count}, nil
}

// PostStats 返回某文章的访客态计数：阅读数、点赞数与「我点过没」（§5）。
//
// 文章不存在 / 草稿 → 404/1002，归档 / 已删除 → 410/1007，与详情同口径。
func (s StatsService) PostStats(slug, visitorSeed string) (PostStats, error) {
	post, err := ServiceGroupApp.Post.PublicBySlug(slug)
	if err != nil {
		return PostStats{}, err
	}

	likeCount, err := model.CountLikes(global.DB, post.ID)
	if err != nil {
		return PostStats{}, fmt.Errorf("统计点赞数失败: %w", err)
	}
	liked, err := model.HasLiked(global.DB, post.ID, utils.HashVisitor(visitorSeed))
	if err != nil {
		return PostStats{}, fmt.Errorf("查询点赞状态失败: %w", err)
	}

	return PostStats{
		ViewCount: int64(post.ViewCount),
		LikeCount: likeCount,
		Liked:     liked,
	}, nil
}

// RecordView 记录一次 PV：明细每次落库，计数按 10 分钟窗口去重（§5 / §16 G5）。
//
// slug 命中已发布文章时计入该文与全站，否则只计全站；文章状态变化（归档/删除）
// 不该让打点失败，此时降级为只计全站行。
func (s StatsService) RecordView(in ViewInput, visitorSeed string) (ViewResult, error) {
	clock, err := s.clock()
	if err != nil {
		return ViewResult{}, err
	}

	path := truncateRunes(strings.TrimSpace(in.Path), statsPathMaxLen)
	if path == "" {
		path = "/"
	}
	visitorHash := utils.HashVisitor(visitorSeed)

	post, err := s.viewTarget(in.Slug)
	if err != nil {
		return ViewResult{}, err
	}
	var postID *int64
	if post != nil {
		id := post.ID
		postID = &id
	}

	dedupeSince := dbtime.FromTime(clock.now.Add(-statsViewDedupeWindow))
	result := ViewResult{ViewCount: viewCountOf(post)}

	err = global.DB.Transaction(func(tx *gorm.DB) error {
		recent, err := model.HasRecentView(tx, postID, path, visitorHash, dedupeSince)
		if err != nil {
			return fmt.Errorf("查询访问去重失败: %w", err)
		}

		view := model.PageView{
			PostID:       postID,
			Path:         path,
			VisitorHash:  visitorHash,
			ReferrerHost: referrerHostOf(in.Referrer),
			CreatedAt:    dbtime.Now(),
		}
		if err := model.InsertPageView(tx, view); err != nil {
			return fmt.Errorf("写入访问明细失败: %w", err)
		}
		if recent {
			return nil
		}

		if postID != nil {
			if err := model.IncrementPostViewCount(tx, *postID); err != nil {
				return fmt.Errorf("更新阅读数失败: %w", err)
			}
		}
		if err := model.UpsertStatsDailyPV(tx, clock.date, model.StatsSitePostID, 1); err != nil {
			return fmt.Errorf("更新全站 PV 失败: %w", err)
		}
		if postID != nil {
			if err := model.UpsertStatsDailyPV(tx, clock.date, *postID, 1); err != nil {
				return fmt.Errorf("更新文章 PV 失败: %w", err)
			}
		}
		if err := refreshUV(tx, clock, postID); err != nil {
			return err
		}

		result.Recorded = true
		if post != nil {
			count := int64(post.ViewCount) + 1
			result.ViewCount = &count
		}
		return nil
	})
	if err != nil {
		return ViewResult{}, err
	}
	return result, nil
}

// Overview 汇总看板：状态计数 + 今日/累计 PV·UV + 点赞 + Top 5 文章（§5）。
func (s StatsService) Overview() (AdminStatsOverview, error) {
	clock, err := s.clock()
	if err != nil {
		return AdminStatsOverview{}, err
	}

	postRows, err := model.CountPostsByStatus(global.DB)
	if err != nil {
		return AdminStatsOverview{}, fmt.Errorf("统计文章状态失败: %w", err)
	}
	deleted, err := model.CountDeletedPosts(global.DB)
	if err != nil {
		return AdminStatsOverview{}, fmt.Errorf("统计回收站失败: %w", err)
	}

	commentRows, err := model.CountCommentsByStatus(global.DB)
	if err != nil {
		return AdminStatsOverview{}, fmt.Errorf("统计评论状态失败: %w", err)
	}
	mediaRows, err := model.CountMediaByStatus(global.DB)
	if err != nil {
		return AdminStatsOverview{}, fmt.Errorf("统计媒体状态失败: %w", err)
	}

	totalPV, _, err := model.SumStatsDaily(global.DB, statsEarliestDate, clock.date)
	if err != nil {
		return AdminStatsOverview{}, fmt.Errorf("汇总 PV 失败: %w", err)
	}
	todayPV, todayUV := s.todayRow(clock)

	totalLikes, err := model.CountLikesSince(global.DB, 0)
	if err != nil {
		return AdminStatsOverview{}, fmt.Errorf("统计点赞失败: %w", err)
	}
	todayLikes, err := model.CountLikesSince(global.DB, clock.start)
	if err != nil {
		return AdminStatsOverview{}, fmt.Errorf("统计今日点赞失败: %w", err)
	}

	topPosts, err := model.ListTopViewedPosts(global.DB, statsTopPostLimit)
	if err != nil {
		return AdminStatsOverview{}, fmt.Errorf("统计热门文章失败: %w", err)
	}

	overview := AdminStatsOverview{
		Comments: AdminCommentCounts{},
		Media:    AdminMediaCounts{},
		Views: AdminViewCounts{
			Total:   totalPV,
			Today:   todayPV,
			UVToday: todayUV,
		},
		Likes:    AdminLikeCounts{Total: totalLikes, Today: todayLikes},
		TopPosts: topPosts,
	}
	overview.Posts.Deleted = deleted
	for _, row := range postRows {
		switch row.Status {
		case model.PostStatusPublished:
			overview.Posts.Published = row.Total
		case model.PostStatusDraft:
			overview.Posts.Draft = row.Total
		case model.PostStatusScheduled:
			overview.Posts.Scheduled = row.Total
		case model.PostStatusArchived:
			overview.Posts.Archived = row.Total
		}
	}
	for _, row := range commentRows {
		switch row.Status {
		case model.CommentStatusPending:
			overview.Comments.Pending = row.Total
		case model.CommentStatusApproved:
			overview.Comments.Approved = row.Total
		case model.CommentStatusSpam:
			overview.Comments.Spam = row.Total
		}
	}
	for _, row := range mediaRows {
		switch row.Status {
		case model.MediaStatusReady:
			overview.Media.Ready = row.Total
		case model.MediaStatusProcessing:
			overview.Media.Processing = row.Total
		case model.MediaStatusFailed:
			overview.Media.Failed = row.Total
		}
	}
	return overview, nil
}

// Daily 返回 [from, to] 的全站日粒度行；空串用缺省区间（最近 30 天），
// 区间长度超过 366 天或日期非法 → 400 / 1003（§5）。
func (s StatsService) Daily(from, to string) (AdminStatsDaily, error) {
	clock, err := s.clock()
	if err != nil {
		return AdminStatsDaily{}, err
	}

	today := clock.now
	if to == "" {
		to = clock.date
	}
	if from == "" {
		from = today.AddDate(0, 0, -(statsDefaultRangeDays - 1)).Format(statsDateLayout)
	}

	fromTime, err := time.ParseInLocation(statsDateLayout, from, clock.loc)
	if err != nil {
		return AdminStatsDaily{}, errs.New(errs.ErrorValidation, "from 必须是 YYYY-MM-DD")
	}
	toTime, err := time.ParseInLocation(statsDateLayout, to, clock.loc)
	if err != nil {
		return AdminStatsDaily{}, errs.New(errs.ErrorValidation, "to 必须是 YYYY-MM-DD")
	}
	if toTime.Before(fromTime) {
		return AdminStatsDaily{}, errs.New(errs.ErrorValidation, "to 不能早于 from")
	}
	if toTime.Sub(fromTime) > time.Duration(statsMaxRangeDays-1)*24*time.Hour {
		return AdminStatsDaily{}, errs.New(errs.ErrorValidation, "查询区间不能超过 366 天")
	}

	rows, err := model.ListStatsDaily(global.DB, from, to)
	if err != nil {
		return AdminStatsDaily{}, fmt.Errorf("查询日粒度失败: %w", err)
	}
	totalPV, totalUV, err := model.SumStatsDaily(global.DB, from, to)
	if err != nil {
		return AdminStatsDaily{}, fmt.Errorf("汇总日粒度失败: %w", err)
	}

	list := make([]AdminStatsDayRow, 0, len(rows))
	for i := range rows {
		list = append(list, AdminStatsDayRow{Date: rows[i].Date, PV: rows[i].PV, UV: rows[i].UV})
	}
	return AdminStatsDaily{
		From:    from,
		To:      to,
		TotalPV: totalPV,
		TotalUV: totalUV,
		List:    list,
	}, nil
}

// viewTarget 把 slug 解析成打点目标：命中已发布文章返回该文，其余情况返回 nil（只计全站）。
func (s StatsService) viewTarget(slug string) (*model.Post, error) {
	slug = strings.TrimSpace(slug)
	if slug == "" {
		return nil, nil
	}

	post, err := ServiceGroupApp.Post.PublicBySlug(slug)
	if err == nil {
		return post, nil
	}
	if appErr, ok := errs.As(err); ok &&
		(appErr.Code == errs.ErrorNotFound || appErr.Code == errs.ErrorGone) {
		return nil, nil
	}
	return nil, err
}

// todayRow 读当天的全站行；缺行时返回 0 / 0。
func (s StatsService) todayRow(clock statsClock) (pv, uv int64) {
	row, err := model.FindStatsDaily(global.DB, clock.date, model.StatsSitePostID)
	if err != nil {
		if !errors.Is(err, model.ErrNotFound) {
			global.Log.Warn("读取当日统计失败", "date", clock.date, "err", err)
		}
		return 0, 0
	}
	return row.PV, row.UV
}

// clock 取站点时区下的当前时刻与当天日界（§4：stats_daily 按上海时区切日）。
func (s StatsService) clock() (statsClock, error) {
	loc, err := ServiceGroupApp.Public.location()
	if err != nil {
		return statsClock{}, err
	}

	now := time.Now().In(loc)
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	return statsClock{
		loc:   loc,
		now:   now,
		date:  now.Format(statsDateLayout),
		start: dbtime.FromTime(start),
		end:   dbtime.FromTime(start.AddDate(0, 0, 1)),
	}, nil
}

// refreshUV 按当前明细重算当天 UV（文章行与全站行），不做增量近似（§5）。
func refreshUV(tx *gorm.DB, clock statsClock, postID *int64) error {
	if postID != nil {
		uv, err := model.CountDistinctVisitors(tx, postID, clock.start, clock.end)
		if err != nil {
			return fmt.Errorf("重算文章 UV 失败: %w", err)
		}
		if err := model.UpdateStatsDailyUV(tx, clock.date, *postID, uv); err != nil {
			return fmt.Errorf("写入文章 UV 失败: %w", err)
		}
	}

	siteUV, err := model.CountDistinctVisitors(tx, nil, clock.start, clock.end)
	if err != nil {
		return fmt.Errorf("重算全站 UV 失败: %w", err)
	}
	if err := model.UpdateStatsDailyUV(tx, clock.date, model.StatsSitePostID, siteUV); err != nil {
		return fmt.Errorf("写入全站 UV 失败: %w", err)
	}
	return nil
}

// viewCountOf 取打点后该文章的阅读数；未命中文章时为 nil（响应里是 null）。
func viewCountOf(post *model.Post) *int64 {
	if post == nil {
		return nil
	}
	count := int64(post.ViewCount)
	return &count
}

// referrerHostOf 只保留 referrer 的 host（§4：page_views 不存完整 URL）。
//
// host 统一小写：DNS 域名本身大小写不敏感，不归一的话 `Example.com` 与 `example.com`
// 会被当成两个来源，来源统计凭空多一份。
func referrerHostOf(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	parsed, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return truncateRunes(strings.ToLower(parsed.Hostname()), statsReferrerHostMaxLen)
}
