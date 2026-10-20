package service

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"gorm.io/gorm"

	"github.com/unicornfairy864/blog-zjut-si/apps/server/core/render"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/global"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/model"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/model/common/dbtime"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/model/common/errs"
)

// PostService 处理文章 CRUD、状态机与版本快照（agent.md §6 / §11）。
//
// 状态迁移只在这里判断；api 层不出现 status 字符串比较。
type PostService struct{}

const (
	// postRevisionKeep 是每篇文章保留的版本数（§4 post_revisions）。
	postRevisionKeep = 50
	// postExcerptLimit 是自动摘要的字符数（§11：正文前 160 字纯文本）。
	postExcerptLimit = 160
	// postSlugMaxLen 是 slug 的最大长度（§4：1–120）。
	postSlugMaxLen = 120
	// postSlugAutoLen 是标题自动生成 slug 时截取的长度（§11：前 40 字符）。
	postSlugAutoLen = 40
	// postTitleMaxLen 是标题的最大字符数。
	postTitleMaxLen = 200
	// postSEOTitleMaxLen / postSEODescriptionMaxLen / postCanonicalURLMaxLen / postLangMaxLen
	// 与 §5 的 binding 上限一致，service 侧再兜一次（binding 只作用于 api 层）。
	postSEOTitleMaxLen       = 200
	postSEODescriptionMaxLen = 500
	postCanonicalURLMaxLen   = 500
	postLangMaxLen           = 20
	// postSlugCollisionRetries 是自动 slug 撞车后的编号重试上限。
	postSlugCollisionRetries = 50
)

var (
	// postSlugPattern 是 §4 的 slug 字符集：仅小写字母、数字与连字符，且不以连字符开头结尾。
	postSlugPattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)
	// postSlugStrip 把标题里的非 ASCII 字母数字折叠成连字符。
	postSlugStrip = regexp.MustCompile(`[^a-z0-9]+`)
	// postSiteLocation 用于自动 slug 的时间戳（站点时区）。
	postSiteLocation = sync.OnceValue(func() *time.Location {
		loc, err := time.LoadLocation("Asia/Shanghai")
		if err != nil {
			return time.FixedZone("CST", 8*60*60)
		}
		return loc
	})
)

// PostActor 是写操作的审计上下文；service 不接触 *gin.Context。
type PostActor struct {
	AdminID int64
	IP      string
}

// PostCreateInput 是新建文章的入参；新建一律为草稿。
type PostCreateInput struct {
	Slug           string
	Title          string
	Excerpt        string
	ContentMD      string
	CoverMediaID   *int64
	OGMediaID      *int64
	IsPinned       bool
	AllowComments  bool
	SeriesID       *int64
	SeriesOrder    int
	SEOTitle       string
	SEODescription string
	CanonicalURL   string
	Lang           string
}

// PostUpdateInput 是更新入参；nil 表示"不修改"，与 PATCH 语义一致。
//
// coverMediaId / ogMediaId / seriesId 传 0 表示显式清空（置 NULL）。
type PostUpdateInput struct {
	Slug           *string
	Title          *string
	Excerpt        *string
	ContentMD      *string
	CoverMediaID   *int64
	OGMediaID      *int64
	IsPinned       *bool
	AllowComments  *bool
	SeriesID       *int64
	SeriesOrder    *int
	SEOTitle       *string
	SEODescription *string
	CanonicalURL   *string
	Lang           *string
	Status         *string
	CreateRedirect bool
}

// Create 新建草稿；slug 未填时按标题生成，中文标题退化为时间戳（§11）。
func (s PostService) Create(in PostCreateInput, actor PostActor) (*model.Post, error) {
	title := strings.TrimSpace(in.Title)
	if title == "" {
		return nil, errs.New(errs.ErrorValidation, "标题不能为空")
	}
	if utf8.RuneCountInString(title) > postTitleMaxLen {
		return nil, errs.New(errs.ErrorValidation, "标题过长")
	}

	now := dbtime.Now()
	rendered := render.Markdown(in.ContentMD)

	slug, err := s.resolveCreateSlug(strings.TrimSpace(in.Slug), title, now)
	if err != nil {
		return nil, err
	}

	lang := strings.TrimSpace(in.Lang)
	if lang == "" {
		lang = model.DefaultPostLang
	}
	if err := validateCanonicalURL(strings.TrimSpace(in.CanonicalURL)); err != nil {
		return nil, err
	}

	post := &model.Post{
		Slug:           slug,
		Title:          title,
		Excerpt:        excerptOrAuto(in.Excerpt, rendered.Text),
		ContentMD:      in.ContentMD,
		ContentHTML:    rendered.HTML,
		ContentText:    rendered.Text,
		CoverMediaID:   in.CoverMediaID,
		Status:         model.PostStatusDraft,
		CreatedAt:      now,
		UpdatedAt:      now,
		WordCount:      rendered.WordCount,
		ReadingTime:    rendered.ReadingTime,
		IsPinned:       in.IsPinned,
		AllowComments:  in.AllowComments,
		SeriesID:       in.SeriesID,
		SeriesOrder:    in.SeriesOrder,
		SEOTitle:       strings.TrimSpace(in.SEOTitle),
		SEODescription: strings.TrimSpace(in.SEODescription),
		CanonicalURL:   strings.TrimSpace(in.CanonicalURL),
		OGMediaID:      in.OGMediaID,
		Revision:       1,
		Lang:           lang,
	}

	if err := global.DB.Transaction(func(tx *gorm.DB) error {
		if err := model.CreatePost(tx, post); err != nil {
			return err
		}
		return ServiceGroupApp.Audit.WriteTx(tx, AuditInput{
			AdminID:    actor.AdminID,
			Action:     AuditActionPostCreate,
			TargetType: "post",
			TargetID:   strconv.FormatInt(post.ID, 10),
			Meta:       map[string]any{"slug": post.Slug, "title": post.Title},
			IP:         actor.IP,
		})
	}); err != nil {
		if model.IsUniqueViolation(err) {
			return nil, errs.New(errs.ErrorSlugConflict, "slug 已存在，请更换")
		}
		return nil, fmt.Errorf("创建文章失败: %w", err)
	}
	return post, nil
}

// List 返回一页文章；status 为空或 all 表示全部。
func (s PostService) List(status, keyword string, page, pageSize int) ([]model.Post, int64, error) {
	status = strings.TrimSpace(status)
	switch status {
	case "", "all":
		status = ""
	case model.PostStatusDraft, model.PostStatusScheduled, model.PostStatusPublished, model.PostStatusArchived:
	default:
		return nil, 0, errs.New(errs.ErrorValidation, "文章状态筛选取值非法")
	}
	return model.ListPosts(global.DB, model.PostListFilter{
		Status:  status,
		Keyword: strings.TrimSpace(keyword),
		Limit:   pageSize,
		Offset:  (page - 1) * pageSize,
	})
}

// Get 返回文章详情；回收站中的文章不可见。
func (s PostService) Get(id int64) (*model.Post, error) {
	post, err := model.FindPostByID(global.DB, id, false)
	if err != nil {
		return nil, s.notFoundOr(err, "查询文章失败")
	}
	return post, nil
}

// Update 局部更新文章；nil 字段保持不变。
func (s PostService) Update(id int64, in PostUpdateInput, actor PostActor) (*model.Post, error) {
	post, err := model.FindPostByID(global.DB, id, false)
	if err != nil {
		return nil, s.notFoundOr(err, "查询文章失败")
	}

	now := dbtime.Now()
	fields := map[string]any{"updated_at": now}
	changed := make([]string, 0, 8)
	oldAutoExcerpt := autoExcerpt(post.ContentText)

	if in.Title != nil {
		title := strings.TrimSpace(*in.Title)
		if title == "" {
			return nil, errs.New(errs.ErrorValidation, "标题不能为空")
		}
		if utf8.RuneCountInString(title) > postTitleMaxLen {
			return nil, errs.New(errs.ErrorValidation, "标题过长")
		}
		if title != post.Title {
			post.Title = title
			fields["title"] = title
			changed = append(changed, "title")
		}
	}

	if in.ContentMD != nil {
		post.ContentMD = *in.ContentMD
		rendered := render.Markdown(post.ContentMD)
		post.ContentHTML = rendered.HTML
		post.ContentText = rendered.Text
		post.WordCount = rendered.WordCount
		post.ReadingTime = rendered.ReadingTime
		fields["content_md"] = post.ContentMD
		fields["content_html"] = post.ContentHTML
		fields["content_text"] = post.ContentText
		fields["word_count"] = post.WordCount
		fields["reading_time"] = post.ReadingTime
		changed = append(changed, "contentMd")
	}

	switch {
	case in.Excerpt != nil:
		excerpt := excerptOrAuto(*in.Excerpt, post.ContentText)
		post.Excerpt = excerpt
		fields["excerpt"] = excerpt
		changed = append(changed, "excerpt")
	case in.ContentMD != nil && (post.Excerpt == "" || post.Excerpt == oldAutoExcerpt):
		// 摘要此前是自动生成的，正文变了就跟着重算；手写摘要保持不动。
		post.Excerpt = autoExcerpt(post.ContentText)
		fields["excerpt"] = post.Excerpt
		changed = append(changed, "excerpt")
	}

	redirect, slugChanged, err := s.applySlug(post, in, fields, now)
	if err != nil {
		return nil, err
	}
	if slugChanged {
		changed = append(changed, "slug")
	}

	// 以下字段此前被漏掉：接口层收下了却没人写库，表现是"改了没反应"。
	// 0 表示显式清空（置 NULL），与 PostUpdateInput 的约定一致。
	if in.CoverMediaID != nil {
		post.CoverMediaID = clearableID(*in.CoverMediaID)
		fields["cover_media_id"] = post.CoverMediaID
		changed = append(changed, "coverMediaId")
	}
	if in.OGMediaID != nil {
		post.OGMediaID = clearableID(*in.OGMediaID)
		fields["og_media_id"] = post.OGMediaID
		changed = append(changed, "ogMediaId")
	}
	if in.SeriesID != nil {
		post.SeriesID = clearableID(*in.SeriesID)
		fields["series_id"] = post.SeriesID
		changed = append(changed, "seriesId")
	}
	if in.SeriesOrder != nil {
		post.SeriesOrder = *in.SeriesOrder
		fields["series_order"] = post.SeriesOrder
		changed = append(changed, "seriesOrder")
	}
	if in.IsPinned != nil {
		post.IsPinned = *in.IsPinned
		fields["is_pinned"] = post.IsPinned
		changed = append(changed, "isPinned")
	}
	if in.AllowComments != nil {
		post.AllowComments = *in.AllowComments
		fields["allow_comments"] = post.AllowComments
		changed = append(changed, "allowComments")
	}
	if in.SEOTitle != nil {
		value := strings.TrimSpace(*in.SEOTitle)
		if utf8.RuneCountInString(value) > postSEOTitleMaxLen {
			return nil, errs.New(errs.ErrorValidation, "seoTitle 不能超过 200 个字符")
		}
		post.SEOTitle = value
		fields["seo_title"] = value
		changed = append(changed, "seoTitle")
	}
	if in.SEODescription != nil {
		value := strings.TrimSpace(*in.SEODescription)
		if utf8.RuneCountInString(value) > postSEODescriptionMaxLen {
			return nil, errs.New(errs.ErrorValidation, "seoDescription 不能超过 500 个字符")
		}
		post.SEODescription = value
		fields["seo_description"] = value
		changed = append(changed, "seoDescription")
	}
	if in.CanonicalURL != nil {
		value := strings.TrimSpace(*in.CanonicalURL)
		if err := validateCanonicalURL(value); err != nil {
			return nil, err
		}
		post.CanonicalURL = value
		fields["canonical_url"] = value
		changed = append(changed, "canonicalUrl")
	}
	if in.Lang != nil {
		value := strings.TrimSpace(*in.Lang)
		if value == "" {
			value = model.DefaultPostLang
		}
		if utf8.RuneCountInString(value) > postLangMaxLen {
			return nil, errs.New(errs.ErrorValidation, "lang 不能超过 20 个字符")
		}
		post.Lang = value
		fields["lang"] = value
		changed = append(changed, "lang")
	}

	statusChanged := false
	if in.Status != nil {
		status := strings.TrimSpace(*in.Status)
		if status != post.Status {
			if err := applyPostStatus(post, status, fields); err != nil {
				return nil, err
			}
			statusChanged = true
			changed = append(changed, "status")
		}
	}

	if len(changed) == 0 {
		return post, nil
	}

	action := AuditActionPostUpdate
	if statusChanged {
		switch post.Status {
		case model.PostStatusArchived:
			action = AuditActionPostArchive
		case model.PostStatusDraft:
			action = AuditActionPostUnpublish
		}
	}

	if err := global.DB.Transaction(func(tx *gorm.DB) error {
		if redirect != nil {
			if err := model.EnsureRedirect(tx, redirect.FromPath, redirect.ToPath, redirect.Status, now); err != nil {
				return err
			}
		}
		// 退回草稿要撤掉待执行的定时任务，否则到点会把它又发出去（§5）。
		if statusChanged && post.Status == model.PostStatusDraft {
			if err := cancelScheduledJobs(tx, post.ID); err != nil {
				return err
			}
		}
		if err := model.UpdatePostFields(tx, post.ID, fields); err != nil {
			return err
		}
		return ServiceGroupApp.Audit.WriteTx(tx, AuditInput{
			AdminID:    actor.AdminID,
			Action:     action,
			TargetType: "post",
			TargetID:   strconv.FormatInt(post.ID, 10),
			Meta:       map[string]any{"fields": changed},
			IP:         actor.IP,
		})
	}); err != nil {
		if model.IsUniqueViolation(err) {
			return nil, errs.New(errs.ErrorSlugConflict, "slug 已存在，请更换")
		}
		return nil, fmt.Errorf("保存文章失败: %w", err)
	}

	return s.Get(id)
}

// Delete 把文章移入回收站；30 天后由 cleanup 物理删除（§13）。
func (s PostService) Delete(id int64, actor PostActor) error {
	post, err := model.FindPostByID(global.DB, id, false)
	if err != nil {
		return s.notFoundOr(err, "查询文章失败")
	}

	now := dbtime.Now()
	if err := global.DB.Transaction(func(tx *gorm.DB) error {
		if err := model.SoftDeletePost(tx, post.ID, now); err != nil {
			return err
		}
		return ServiceGroupApp.Audit.WriteTx(tx, AuditInput{
			AdminID:    actor.AdminID,
			Action:     AuditActionPostDelete,
			TargetType: "post",
			TargetID:   strconv.FormatInt(post.ID, 10),
			Meta:       map[string]any{"slug": post.Slug},
			IP:         actor.IP,
		})
	}); err != nil {
		return fmt.Errorf("删除文章失败: %w", err)
	}
	return nil
}

// Publish 把草稿发布上线：写版本快照、重算渲染与计数、revision+1（§6）。
//
// 与定时发布共用 publishInTx；手动发布还会撤掉可能存在的定时任务，避免到点重复发布。
// 已发布文章只有在**内容相对上一版本有变化**时才允许再发布——那等于"为线上改动记一个新版本"，
// 内容没变仍按 §5 的「重复发布」返回 409，这样版本历史与回滚对线上改字同样有效。
func (s PostService) Publish(id int64, note string, actor PostActor) (*model.Post, error) {
	var updated *model.Post
	if err := global.DB.Transaction(func(tx *gorm.DB) error {
		post, err := model.FindPostByID(tx, id, false)
		if err != nil {
			return err
		}
		if post.Status == model.PostStatusArchived {
			return errs.New(errs.ErrorConflict, "已归档文章请先取消归档再发布")
		}

		now := dbtime.Now()
		at := now
		if post.Status == model.PostStatusPublished {
			latest, err := model.ListPostRevisions(tx, post.ID, 1)
			if err != nil {
				return err
			}
			if len(latest) > 0 && latest[0].Title == post.Title && latest[0].ContentMD == post.ContentMD {
				return errs.New(errs.ErrorConflict, "文章已处于发布状态")
			}
			// 保留原发布时间：重新发布只是补一个版本，不该把文章顶到列表最前。
			if post.PublishedAt != nil {
				at = *post.PublishedAt
			}
		}

		if err := cancelScheduledJobs(tx, post.ID); err != nil {
			return err
		}
		if err := publishInTx(tx, post, note, at, now, actor); err != nil {
			return err
		}

		updated = post
		return nil
	}); err != nil {
		if _, ok := errs.As(err); ok {
			return nil, err
		}
		return nil, fmt.Errorf("发布文章失败: %w", err)
	}
	return s.Get(updated.ID)
}

// Unpublish 把已发布/已归档/已定时文章退回草稿，并清空发布时间。
//
// 定时文章退回草稿时，待执行的 publish_scheduled 任务一并撤掉（§5）。
func (s PostService) Unpublish(id int64, actor PostActor) (*model.Post, error) {
	var updated *model.Post
	if err := global.DB.Transaction(func(tx *gorm.DB) error {
		post, err := model.FindPostByID(tx, id, false)
		if err != nil {
			return err
		}
		if post.Status == model.PostStatusDraft {
			return errs.New(errs.ErrorConflict, "文章已是草稿状态")
		}

		if err := cancelScheduledJobs(tx, post.ID); err != nil {
			return err
		}
		now := dbtime.Now()
		if err := model.UpdatePostFields(tx, post.ID, map[string]any{
			"status":       model.PostStatusDraft,
			"published_at": nil,
			"updated_at":   now,
		}); err != nil {
			return err
		}
		if err := ServiceGroupApp.Audit.WriteTx(tx, AuditInput{
			AdminID:    actor.AdminID,
			Action:     AuditActionPostUnpublish,
			TargetType: "post",
			TargetID:   strconv.FormatInt(post.ID, 10),
			IP:         actor.IP,
		}); err != nil {
			return err
		}
		updated = post
		return nil
	}); err != nil {
		if _, ok := errs.As(err); ok {
			return nil, err
		}
		return nil, fmt.Errorf("取消发布失败: %w", err)
	}
	return s.Get(updated.ID)
}

// PostPreview 是编辑器预览的产物；与发布时落库的渲染完全同源（§8）。
type PostPreview struct {
	HTML        string
	Text        string
	WordCount   int
	ReadingTime int
	Excerpt     string
}

// Preview 渲染尚未保存的 Markdown，不写库。
func (s PostService) Preview(contentMD, excerpt string) PostPreview {
	rendered := render.Markdown(contentMD)
	return PostPreview{
		HTML:        rendered.HTML,
		Text:        rendered.Text,
		WordCount:   rendered.WordCount,
		ReadingTime: rendered.ReadingTime,
		Excerpt:     excerptOrAuto(excerpt, rendered.Text),
	}
}

// PublicList 返回已发布文章的一页；草稿、定时、归档与回收站一律不可见。
func (s PostService) PublicList(page, pageSize int) ([]model.Post, int64, error) {
	return model.ListPublishedPosts(global.DB, model.PublicPostFilter{
		Limit:  pageSize,
		Offset: (page - 1) * pageSize,
	})
}

// PublicBySlug 返回公开文章详情：归档/已删除 → 410，草稿/定时 → 404（§11）。
func (s PostService) PublicBySlug(slug string) (*model.Post, error) {
	post, err := model.FindPostBySlug(global.DB, slug, true)
	if err != nil {
		if errors.Is(err, model.ErrNotFound) {
			return nil, errs.New(errs.ErrorNotFound, "文章不存在")
		}
		return nil, fmt.Errorf("查询文章失败: %w", err)
	}

	switch {
	case post.DeletedAt != nil:
		return nil, errs.New(errs.ErrorGone, "文章已删除")
	case post.Status == model.PostStatusArchived:
		return nil, errs.New(errs.ErrorGone, "文章已归档")
	case post.Status != model.PostStatusPublished:
		// 草稿与定时文章不泄露存在性。
		return nil, errs.New(errs.ErrorNotFound, "文章不存在")
	}
	return post, nil
}

// Search 公开搜索；关键词长度由 api 层校验。
func (s PostService) Search(keyword string, page, pageSize int) ([]model.Post, int64, error) {
	return model.SearchPublishedPosts(global.DB, keyword, pageSize, (page-1)*pageSize)
}

// Revisions 返回文章的版本列表（最多 50 条）。
func (s PostService) Revisions(id int64) ([]model.PostRevision, error) {
	if _, err := model.FindPostByID(global.DB, id, false); err != nil {
		return nil, s.notFoundOr(err, "查询文章失败")
	}
	revisions, err := model.ListPostRevisions(global.DB, id, postRevisionKeep)
	if err != nil {
		return nil, fmt.Errorf("查询版本失败: %w", err)
	}
	return revisions, nil
}

// applySlug 处理 slug 变更：校验字符集、查重，已发布文章还要求同时写 301 跳转（2004）。
//
// 返回值：需要写入的跳转（可能为 nil）、slug 是否真的变化。
func (s PostService) applySlug(post *model.Post, in PostUpdateInput, fields map[string]any, now dbtime.Unix) (*model.Redirect, bool, error) {
	if in.Slug == nil {
		return nil, false, nil
	}
	slug := strings.TrimSpace(*in.Slug)
	if slug == post.Slug {
		return nil, false, nil
	}
	if err := validatePostSlug(slug); err != nil {
		return nil, false, err
	}

	exists, err := model.PostSlugExists(global.DB, slug, post.ID)
	if err != nil {
		return nil, false, fmt.Errorf("检查 slug 失败: %w", err)
	}
	if exists {
		return nil, false, errs.New(errs.ErrorSlugConflict, "slug 已存在，请更换")
	}

	var redirect *model.Redirect
	if isPostSlugLocked(post.Status) {
		if !in.CreateRedirect {
			return nil, false, errs.New(errs.ErrorSlugImmutable, "已发布文章的 slug 修改必须同时创建 301 跳转")
		}
		redirect = &model.Redirect{
			FromPath:  postPublicPath(post.Slug),
			ToPath:    postPublicPath(slug),
			Status:    301,
			CreatedAt: now,
		}
	}

	post.Slug = slug
	fields["slug"] = slug
	return redirect, true, nil
}

// resolveCreateSlug 决定新建文章的 slug：手填优先且冲突即 2001，自动生成则追加序号。
func (s PostService) resolveCreateSlug(input, title string, now dbtime.Unix) (string, error) {
	if input != "" {
		if err := validatePostSlug(input); err != nil {
			return "", err
		}
		exists, err := model.PostSlugExists(global.DB, input, 0)
		if err != nil {
			return "", fmt.Errorf("检查 slug 失败: %w", err)
		}
		if exists {
			return "", errs.New(errs.ErrorSlugConflict, "slug 已存在，请更换")
		}
		return input, nil
	}

	base := slugify(title)
	if base == "" {
		// 中文标题没有拼音库（§17.2 白名单未登记），按 §11 的退路用时间戳。
		base = "post-" + now.Time().In(postSiteLocation()).Format("20060102-1504")
	}

	candidate := base
	for i := 2; i <= postSlugCollisionRetries; i++ {
		exists, err := model.PostSlugExists(global.DB, candidate, 0)
		if err != nil {
			return "", fmt.Errorf("检查 slug 失败: %w", err)
		}
		if !exists {
			return candidate, nil
		}
		suffix := "-" + strconv.Itoa(i)
		trimmed := strings.TrimRight(base, "-")
		if len(trimmed)+len(suffix) > postSlugMaxLen {
			trimmed = strings.TrimRight(trimmed[:postSlugMaxLen-len(suffix)], "-")
		}
		candidate = trimmed + suffix
	}
	return "", errs.New(errs.ErrorSlugConflict, "无法生成唯一的 slug，请手动填写")
}

// notFoundOr 把 model.ErrNotFound 翻成 404/1002，其余错误原样包装。
func (s PostService) notFoundOr(err error, action string) error {
	if errors.Is(err, model.ErrNotFound) {
		return errs.New(errs.ErrorNotFound, "文章不存在")
	}
	return fmt.Errorf("%s: %w", action, err)
}

// applyPostStatus 校验 PATCH 里的状态迁移；发布与定时发布必须走专用接口。
func applyPostStatus(post *model.Post, status string, fields map[string]any) error {
	switch status {
	case model.PostStatusDraft:
		fields["status"] = model.PostStatusDraft
		fields["published_at"] = nil
		post.Status = model.PostStatusDraft
		post.PublishedAt = nil
	case model.PostStatusArchived:
		if post.Status != model.PostStatusPublished {
			return errs.New(errs.ErrorConflict, "只有已发布文章可以归档")
		}
		fields["status"] = model.PostStatusArchived
		post.Status = model.PostStatusArchived
	case model.PostStatusPublished, model.PostStatusScheduled:
		return errs.New(errs.ErrorValidation, "发布与定时发布请使用专用接口")
	default:
		return errs.New(errs.ErrorValidation, "文章状态取值非法")
	}
	return nil
}

// clearableID 把 0 翻成 nil（显式清空外键），其余原样返回。
func clearableID(id int64) *int64 {
	if id <= 0 {
		return nil
	}
	return &id
}

// validateCanonicalURL 校验 canonicalUrl：留空表示用默认（本站链接），填了必须是 http(s)。
//
// 空串是合法的"留空"值：前端每次保存都会带上该字段，空值不该被当成非法链接（§5）。
func validateCanonicalURL(value string) error {
	if value == "" {
		return nil
	}
	if !isHTTPURL(value) || utf8.RuneCountInString(value) > postCanonicalURLMaxLen {
		return errs.New(errs.ErrorValidation,
			"canonicalUrl 必须以 http:// 或 https:// 开头，且不超过 500 个字符")
	}
	return nil
}

// validatePostSlug 校验 §4 的 slug 约束。
func validatePostSlug(slug string) error {
	if slug == "" || len(slug) > postSlugMaxLen {
		return errs.New(errs.ErrorValidation, fmt.Sprintf("slug 长度必须在 1–%d 之间", postSlugMaxLen))
	}
	if !postSlugPattern.MatchString(slug) {
		return errs.New(errs.ErrorValidation, "slug 只能包含小写字母、数字与连字符，且不能以连字符开头或结尾")
	}
	return nil
}

// slugify 把标题尽量转成 ASCII slug；中文标题会得到空串，交给调用方兜底。
func slugify(title string) string {
	s := postSlugStrip.ReplaceAllString(strings.ToLower(strings.TrimSpace(title)), "-")
	s = strings.Trim(s, "-")
	if len(s) > postSlugAutoLen {
		s = strings.TrimRight(s[:postSlugAutoLen], "-")
	}
	return s
}

// isPostSlugLocked 报告该状态下 slug 是否已锁定（发布后不可变，§6）。
func isPostSlugLocked(status string) bool {
	return status == model.PostStatusPublished || status == model.PostStatusArchived
}

// postPublicPath 是文章对外路径；改 slug 时写 redirects 用。
func postPublicPath(slug string) string { return "/posts/" + slug }

// excerptOrAuto 用手写摘要，空则从正文纯文本截取 160 字。
func excerptOrAuto(excerpt, text string) string {
	if e := strings.TrimSpace(excerpt); e != "" {
		return e
	}
	return autoExcerpt(text)
}

// autoExcerpt 折叠空白后截取前 160 字符。
func autoExcerpt(text string) string {
	return truncateRunes(strings.Join(strings.Fields(text), " "), postExcerptLimit)
}

// truncateRunes 按字符（而非字节）截断，避免切坏中文。
func truncateRunes(s string, max int) string {
	if max <= 0 || utf8.RuneCountInString(s) <= max {
		return s
	}
	return string([]rune(s)[:max])
}
