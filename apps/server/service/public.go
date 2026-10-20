package service

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/unicornfairy864/blog-zjut-si/apps/server/global"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/model"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/model/common/dbtime"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/model/common/errs"
)

// PublicService 处理公开端（8640）的只读聚合：站点信息、标签、归档、系列、友链、静态页与相关文章（agent.md §5）。
//
// 列表项一律经 postListItems 补齐封面与标签：封面一次 `WHERE id IN (...)`、标签一次 join，
// 禁止 N+1（§12）。文章状态机由 PostService 判断，这里只消费它的公开查询。
type PublicService struct{}

const (
	// publicDefaultTimezone 是 settings.site_timezone 的缺省值（§4）。
	publicDefaultTimezone = "Asia/Shanghai"
	// publicDefaultContentVersion 是 settings.content_version 的缺省值。
	publicDefaultContentVersion int64 = 1
	// publicRelatedLimit 是相关文章的最大条数（§5）。
	publicRelatedLimit = 6
)

// SiteInfo 是 GET /api/site 的输出。
type SiteInfo struct {
	Timezone       string     `json:"timezone"`
	ImgBaseURL     string     `json:"imgBaseUrl"`
	ContentVersion int64      `json:"contentVersion"`
	Counts         SiteCounts `json:"counts"`
}

// SiteCounts 是站点四项计数；口径与各自公开列表的长度一致。
type SiteCounts struct {
	Posts  int64 `json:"posts"`
	Tags   int64 `json:"tags"`
	Series int64 `json:"series"`
	Links  int64 `json:"links"`
}

// TagDetail 是 GET /api/tags/{slug} 的输出。
type TagDetail struct {
	Tag      model.TagItem              `json:"tag"`
	List     []model.PublicPostListItem `json:"list"`
	Total    int64                      `json:"total"`
	Page     int                        `json:"page"`
	PageSize int                        `json:"pageSize"`
}

// SeriesDetail 是 GET /api/series/{slug} 的输出。
type SeriesDetail struct {
	Series model.SeriesItem           `json:"series"`
	List   []model.PublicPostListItem `json:"list"`
	Total  int64                      `json:"total"`
}

// ArchivePostItem 是归档里的一篇文章。
type ArchivePostItem struct {
	ID          int64       `json:"id"`
	Slug        string      `json:"slug"`
	Title       string      `json:"title"`
	PublishedAt dbtime.Unix `json:"publishedAt"`
}

// ArchiveMonth 是归档里的一个月。
type ArchiveMonth struct {
	Month int               `json:"month"`
	Count int               `json:"count"`
	Posts []ArchivePostItem `json:"posts"`
}

// ArchiveYear 是归档里的一年。
type ArchiveYear struct {
	Year   int            `json:"year"`
	Count  int            `json:"count"`
	Months []ArchiveMonth `json:"months"`
}

// Archive 是 GET /api/archive 的输出。
type Archive struct {
	Total int64         `json:"total"`
	Years []ArchiveYear `json:"years"`
}

// Site 返回站点公开信息与计数；settings 缺键时用契约缺省值（§4）。
func (s PublicService) Site() (SiteInfo, error) {
	timezone, err := s.timezoneName()
	if err != nil {
		return SiteInfo{}, err
	}
	version, err := ServiceGroupApp.Setting.Int64(model.SettingContentVersion, publicDefaultContentVersion)
	if err != nil {
		return SiteInfo{}, err
	}

	posts, err := model.CountPublishedPosts(global.DB)
	if err != nil {
		return SiteInfo{}, fmt.Errorf("统计文章数失败: %w", err)
	}
	tags, err := model.CountPublicTags(global.DB)
	if err != nil {
		return SiteInfo{}, fmt.Errorf("统计标签数失败: %w", err)
	}
	series, err := model.CountSeries(global.DB)
	if err != nil {
		return SiteInfo{}, fmt.Errorf("统计系列数失败: %w", err)
	}
	links, err := model.CountActiveLinks(global.DB)
	if err != nil {
		return SiteInfo{}, fmt.Errorf("统计友链数失败: %w", err)
	}

	return SiteInfo{
		Timezone:       timezone,
		ImgBaseURL:     ServiceGroupApp.Media.baseURL(),
		ContentVersion: version,
		Counts: SiteCounts{
			Posts:  posts,
			Tags:   tags,
			Series: series,
			Links:  links,
		},
	}, nil
}

// Tags 返回公开标签列表：count 为已发布文章数，为 0 的标签不出现。
func (s PublicService) Tags() ([]model.TagWithCount, error) {
	tags, err := model.ListPublicTags(global.DB)
	if err != nil {
		return nil, fmt.Errorf("查询标签失败: %w", err)
	}
	return tags, nil
}

// TagPosts 返回某标签下的公开文章分页；标签不存在 → code 1002（HTTP 200）。
func (s PublicService) TagPosts(slug string, page, pageSize int) (TagDetail, error) {
	tag, err := model.FindTagBySlug(global.DB, slug)
	if err != nil {
		if errors.Is(err, model.ErrNotFound) {
			return TagDetail{}, errs.New(errs.ErrorNotFound, "标签不存在")
		}
		return TagDetail{}, fmt.Errorf("查询标签失败: %w", err)
	}

	posts, total, err := model.ListTagPublishedPosts(global.DB, tag.ID, pageSize, (page-1)*pageSize)
	if err != nil {
		return TagDetail{}, fmt.Errorf("查询标签文章失败: %w", err)
	}
	items, err := s.postListItems(posts)
	if err != nil {
		return TagDetail{}, err
	}
	return TagDetail{Tag: tag.Item(), List: items, Total: total, Page: page, PageSize: pageSize}, nil
}

// Archive 返回归档：仅已发布，按站点时区按年倒序、月倒序（§5）。
func (s PublicService) Archive() (Archive, error) {
	posts, err := model.ListPublishedForArchive(global.DB)
	if err != nil {
		return Archive{}, fmt.Errorf("查询归档失败: %w", err)
	}
	loc, err := s.location()
	if err != nil {
		return Archive{}, err
	}
	return buildArchive(posts, loc), nil
}

// Series 返回公开系列列表；暂无已发布文章的系列也保留（count 为 0）。
func (s PublicService) Series() ([]model.SeriesWithCount, error) {
	items, err := model.ListPublicSeries(global.DB)
	if err != nil {
		return nil, fmt.Errorf("查询系列失败: %w", err)
	}
	return items, nil
}

// SeriesDetail 返回系列详情与其文章：series_order 升序、published_at 升序；
// 系列不存在 → code 1002（HTTP 200）。
func (s PublicService) SeriesDetail(slug string) (SeriesDetail, error) {
	series, err := model.FindSeriesBySlug(global.DB, slug)
	if err != nil {
		if errors.Is(err, model.ErrNotFound) {
			return SeriesDetail{}, errs.New(errs.ErrorNotFound, "系列不存在")
		}
		return SeriesDetail{}, fmt.Errorf("查询系列失败: %w", err)
	}

	posts, err := model.ListSeriesPublishedPosts(global.DB, series.ID)
	if err != nil {
		return SeriesDetail{}, fmt.Errorf("查询系列文章失败: %w", err)
	}
	items, err := s.postListItems(posts)
	if err != nil {
		return SeriesDetail{}, err
	}
	return SeriesDetail{Series: series.Item(), List: items, Total: int64(len(items))}, nil
}

// Links 返回可见友链：sort 升序、id 升序（§5）。
func (s PublicService) Links() ([]model.PublicLinkItem, error) {
	links, err := model.ListActiveLinks(global.DB)
	if err != nil {
		return nil, fmt.Errorf("查询友链失败: %w", err)
	}

	items := make([]model.PublicLinkItem, 0, len(links))
	for i := range links {
		items = append(items, links[i].PublicItem())
	}
	return items, nil
}

// Page 返回静态页详情；不存在 → code 1002，软删 → code 1007（都是 HTTP 200）。
func (s PublicService) Page(slug string) (model.PublicPageDetail, error) {
	page, err := model.FindPageBySlug(global.DB, slug, true)
	if err != nil {
		if errors.Is(err, model.ErrNotFound) {
			return model.PublicPageDetail{}, errs.New(errs.ErrorNotFound, "页面不存在")
		}
		return model.PublicPageDetail{}, fmt.Errorf("查询页面失败: %w", err)
	}
	if page.DeletedAt != nil {
		return model.PublicPageDetail{}, errs.New(errs.ErrorGone, "页面已删除")
	}
	return page.PublicDetail(), nil
}

// Related 返回相关文章：同标签或同系列、排除自身、仅已发布，最多 6 篇；
// 没有关联时返回空数组。文章不存在 / 归档 / 已删除与详情同口径。
func (s PublicService) Related(slug string) ([]model.PublicPostListItem, error) {
	post, err := ServiceGroupApp.Post.PublicBySlug(slug)
	if err != nil {
		return nil, err
	}

	posts, err := model.ListRelatedPublishedPosts(global.DB, post, publicRelatedLimit)
	if err != nil {
		return nil, fmt.Errorf("查询相关文章失败: %w", err)
	}
	return s.postListItems(posts)
}

// Posts 返回公开文章分页（含封面与标签）。
func (s PublicService) Posts(page, pageSize int) ([]model.PublicPostListItem, int64, error) {
	posts, total, err := ServiceGroupApp.Post.PublicList(page, pageSize)
	if err != nil {
		return nil, 0, err
	}
	items, err := s.postListItems(posts)
	if err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

// Search 返回公开搜索结果（含封面与标签）。
func (s PublicService) Search(keyword string, page, pageSize int) ([]model.PublicPostListItem, int64, error) {
	posts, total, err := ServiceGroupApp.Post.Search(keyword, page, pageSize)
	if err != nil {
		return nil, 0, err
	}
	items, err := s.postListItems(posts)
	if err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

// PostDetail 返回公开文章详情：含封面、标签与相邻文章；
// 正文里的本站图片在读取时补响应式属性并包上「查看原图」直链（§10 / §11）。
func (s PublicService) PostDetail(slug string) (*model.PublicPostDetail, error) {
	post, err := ServiceGroupApp.Post.PublicBySlug(slug)
	if err != nil {
		return nil, err
	}

	items, err := s.postListItems([]model.Post{*post})
	if err != nil {
		return nil, err
	}

	prev, next, err := s.adjacent(post)
	if err != nil {
		return nil, err
	}

	detail := post.PublicDetail()
	detail.PublicPostListItem = items[0]
	detail.Prev = prev
	detail.Next = next
	detail.ContentHTML = ServiceGroupApp.Media.DecorateContent(detail.ContentHTML)
	return &detail, nil
}

// postListItems 把文章批量转成公开列表项：一次查封面、一次查标签（禁止 N+1）。
func (s PublicService) postListItems(posts []model.Post) ([]model.PublicPostListItem, error) {
	items := make([]model.PublicPostListItem, 0, len(posts))
	if len(posts) == 0 {
		return items, nil
	}

	mediaByID, err := findCoverMedia(posts)
	if err != nil {
		return nil, err
	}
	tagsByPost, err := findPostTags(posts)
	if err != nil {
		return nil, err
	}

	base := ServiceGroupApp.Media.baseURL()
	for i := range posts {
		item := posts[i].PublicListItem()
		if posts[i].CoverMediaID != nil {
			item.Cover = toPublicCover(mediaByID[*posts[i].CoverMediaID], base)
		}
		if tags, ok := tagsByPost[posts[i].ID]; ok {
			item.Tags = tags
		}
		items = append(items, item)
	}
	return items, nil
}

// adjacent 取详情用的上一篇 / 下一篇；边界为 nil。
func (s PublicService) adjacent(post *model.Post) (*model.PostNeighbor, *model.PostNeighbor, error) {
	prev, next, err := model.FindAdjacentPublishedPosts(global.DB, post)
	if err != nil {
		return nil, nil, fmt.Errorf("查询相邻文章失败: %w", err)
	}
	return toNeighbor(prev), toNeighbor(next), nil
}

// timezoneName 返回站点时区名；settings 缺键时用契约缺省值。
func (s PublicService) timezoneName() (string, error) {
	name, err := ServiceGroupApp.Setting.String(model.SettingSiteTimezone)
	if err != nil {
		return "", err
	}
	if name = strings.TrimSpace(name); name == "" {
		return publicDefaultTimezone, nil
	}
	return name, nil
}

// location 解析站点时区；取值非法或运行环境缺少时区库时退回默认时区。
func (s PublicService) location() (*time.Location, error) {
	name, err := s.timezoneName()
	if err != nil {
		return nil, err
	}
	loc, loadErr := time.LoadLocation(name)
	if loadErr == nil {
		return loc, nil
	}
	global.Log.Warn("站点时区不可用，已退回默认时区", "timezone", name, "err", loadErr)

	if loc, loadErr = time.LoadLocation(publicDefaultTimezone); loadErr == nil {
		return loc, nil
	}
	return time.FixedZone("CST", 8*60*60), nil
}

// findCoverMedia 一次取回这一批文章引用的封面媒体，键为 media.id。
func findCoverMedia(posts []model.Post) (map[int64]*model.Media, error) {
	ids := make([]int64, 0, len(posts))
	seen := make(map[int64]struct{}, len(posts))
	for i := range posts {
		cover := posts[i].CoverMediaID
		if cover == nil {
			continue
		}
		if _, ok := seen[*cover]; ok {
			continue
		}
		seen[*cover] = struct{}{}
		ids = append(ids, *cover)
	}
	if len(ids) == 0 {
		return map[int64]*model.Media{}, nil
	}

	media, err := model.FindMediaByIDs(global.DB, ids)
	if err != nil {
		return nil, fmt.Errorf("查询封面失败: %w", err)
	}
	byID := make(map[int64]*model.Media, len(media))
	for i := range media {
		byID[media[i].ID] = &media[i]
	}
	return byID, nil
}

// findPostTags 一次 join 取回这一批文章的标签，键为 posts.id。
func findPostTags(posts []model.Post) (map[int64][]model.PostTagRef, error) {
	ids := make([]int64, 0, len(posts))
	for i := range posts {
		ids = append(ids, posts[i].ID)
	}
	tags, err := model.FindTagsByPostIDs(global.DB, ids)
	if err != nil {
		return nil, fmt.Errorf("查询文章标签失败: %w", err)
	}
	return tags, nil
}

// toPublicCover 把媒体实体裁剪成公开封面。
//
// URL 复用 toMediaItem，保证与媒体库的 url / variants / origUrl 完全一致（§10）。
func toPublicCover(media *model.Media, base string) *model.PublicCover {
	if media == nil {
		return nil
	}
	item := toMediaItem(media, base)
	return &model.PublicCover{
		SHA256:   item.SHA256,
		Ext:      item.Ext,
		Width:    item.Width,
		Height:   item.Height,
		Alt:      item.Alt,
		Blurhash: item.Blurhash,
		URL:      item.URL,
		OrigURL:  item.OrigURL,
		Variants: item.Variants,
	}
}

// toNeighbor 把实体裁剪成相邻文章引用；nil 保持 nil（边界输出 null）。
func toNeighbor(post *model.Post) *model.PostNeighbor {
	if post == nil {
		return nil
	}
	return &model.PostNeighbor{Slug: post.Slug, Title: post.Title}
}

// buildArchive 按站点时区把已发布文章分成年 / 月；入参必须已按 published_at 倒序。
func buildArchive(posts []model.Post, loc *time.Location) Archive {
	archive := Archive{Total: int64(len(posts)), Years: make([]ArchiveYear, 0, 4)}
	for i := range posts {
		published := posts[i].PublishedAt
		if published == nil {
			continue
		}

		at := published.Time().In(loc)
		year, month := at.Year(), int(at.Month())

		if len(archive.Years) == 0 || archive.Years[len(archive.Years)-1].Year != year {
			archive.Years = append(archive.Years, ArchiveYear{Year: year, Months: make([]ArchiveMonth, 0, 12)})
		}
		currentYear := &archive.Years[len(archive.Years)-1]
		if len(currentYear.Months) == 0 || currentYear.Months[len(currentYear.Months)-1].Month != month {
			currentYear.Months = append(currentYear.Months,
				ArchiveMonth{Month: month, Posts: make([]ArchivePostItem, 0, 8)})
		}

		currentMonth := &currentYear.Months[len(currentYear.Months)-1]
		currentMonth.Posts = append(currentMonth.Posts, ArchivePostItem{
			ID:          posts[i].ID,
			Slug:        posts[i].Slug,
			Title:       posts[i].Title,
			PublishedAt: *published,
		})
		currentMonth.Count++
		currentYear.Count++
	}
	return archive
}
