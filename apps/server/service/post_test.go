// D18：service/post.go 与 service/redirect.go 的单元测试。
//
// 为什么每个用例都自己调 setupEnv：global.DB / global.Config / global.Log 都是进程级单例，
// 用例之间只能靠"换一套临时库"隔离，所以这里一律不写 t.Parallel()（并行会互相踩数据）。
package service

import (
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/unicornfairy864/blog-zjut-si/apps/server/core/render"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/global"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/model"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/model/common/dbtime"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/model/common/errs"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/model/common/request"
)

// ptrString / ptrInt64 / ptrInt / ptrBool / ptrInt64Slice 让 PATCH 入参写起来短一点。
func ptrString(v string) *string { return &v }

func ptrInt64(v int64) *int64 { return &v }

func ptrInt(v int) *int { return &v }

func ptrBool(v bool) *bool { return &v }

func ptrInt64Slice(v []int64) *[]int64 { return &v }

// manyIDs 造 n 个连续正整数，用来触达"标签数量上限"这类边界。
func manyIDs(n int) []int64 {
	ids := make([]int64, 0, n)
	for i := 1; i <= n; i++ {
		ids = append(ids, int64(i))
	}
	return ids
}

// insertMediaRow 直插一条 ready 媒体：给 Get 的封面 / OG 图回显造数据，
// 走 Media.Upload 需要真实图片文件，与 post.go 的覆盖面无关。
func insertMediaRow(t *testing.T, sha, key, ext string) *model.Media {
	t.Helper()

	now := dbtime.Now()
	media := &model.Media{
		SHA256:        sha,
		StorageDriver: model.StorageDriverLocal,
		StorageKey:    key,
		Mime:          "image/webp",
		Ext:           ext,
		VariantsJSON:  "{}",
		Status:        model.MediaStatusReady,
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	require.NoError(t, global.DB.Create(media).Error)
	require.NotZero(t, media.ID)
	return media
}

// findPostOrFail 从库里重读一篇文章：断言"确实落库了"，而不是只看 service 的返回值。
func findPostOrFail(t *testing.T, id int64) *model.Post {
	t.Helper()

	post, err := model.FindPostByID(global.DB, id, false)
	require.NoError(t, err)
	return post
}

// mustTagIDs 读回一篇文章的标签 id（按标签名升序），用于断言关联表内容。
func mustTagIDs(t *testing.T, postID int64) []int64 {
	t.Helper()

	ids, err := model.ListTagIDsOfPost(global.DB, postID)
	require.NoError(t, err)
	return ids
}

// requireAppError / appErrorOf 定义在 main_test.go（多个测试文件共用）。

// assertWrappedError 断言错误是普通错误（不是业务错误）且带上了上下文。
//
// 故障注入场景专用：只有 model.ErrNotFound 才允许翻成 1002，基础设施失败必须原样包出来，
// 否则"库坏了"会被调用方当成"文章不存在"。
func assertWrappedError(t *testing.T, err error, contains string) {
	t.Helper()

	require.Error(t, err)
	_, ok := errs.As(err)
	assert.False(t, ok, "查询/写入失败不该被当成业务错误: %v", err)
	assert.Contains(t, err.Error(), contains)
}

// assertFieldError 断言业务错误码，并确认 Details 里点名了出错的字段（字段错误是稳定契约）。
func assertFieldError(t *testing.T, err error, code int, field string) {
	t.Helper()

	appErr := appErrorOf(t, err, code)
	details, ok := appErr.Details.([]request.FieldError)
	require.True(t, ok, "Details 应当是 []request.FieldError，实际: %T", appErr.Details)

	fields := make([]string, 0, len(details))
	for _, item := range details {
		fields = append(fields, item.Field)
	}
	assert.Contains(t, fields, field)
}

func TestPostService_CreateDraftPersistsRendering(t *testing.T) {
	setupEnv(t)
	actor := testActor(t)
	svc := PostService{}

	md := "# Hello World\n\n这是正文内容。"
	want := render.Markdown(md)

	post, err := svc.Create(PostCreateInput{
		Title:         "Hello World",
		ContentMD:     md,
		IsPinned:      true,
		AllowComments: true,
		Lang:          "en-US",
	}, actor)
	require.NoError(t, err)
	require.NotZero(t, post.ID)

	// 新建一律是草稿：没填 slug 就按标题生成，且不带发布时间。
	assert.Equal(t, "hello-world", post.Slug)
	assert.Equal(t, model.PostStatusDraft, post.Status)
	assert.Nil(t, post.PublishedAt)
	assert.Equal(t, 1, post.Revision)
	assert.NotNil(t, post.TagIDs, "无标签时应是空数组而不是 null")
	assert.Empty(t, post.TagIDs)

	// 重读数据库：渲染产物、字数与开关项都必须真的落库。
	stored := findPostOrFail(t, post.ID)
	assert.Equal(t, want.HTML, stored.ContentHTML)
	assert.Equal(t, want.Text, stored.ContentText)
	assert.Equal(t, want.WordCount, stored.WordCount)
	assert.Equal(t, want.ReadingTime, stored.ReadingTime)
	assert.Positive(t, stored.WordCount)
	assert.Equal(t, autoExcerpt(want.Text), stored.Excerpt)
	assert.True(t, stored.IsPinned)
	assert.True(t, stored.AllowComments)
	assert.Equal(t, "en-US", stored.Lang)

	// 草稿对公开端不可见。
	_, err = svc.PublicBySlug("hello-world")
	requireAppError(t, err, errs.ErrorNotFound)

	assert.EqualValues(t, 1, countRows(t, "audit_logs", "action = ? AND target_id = ?",
		AuditActionPostCreate, strconv.FormatInt(post.ID, 10)))
}

func TestPostService_CreateTitleAndSlugValidation(t *testing.T) {
	setupEnv(t)
	actor := testActor(t)
	svc := PostService{}

	_, err := svc.Create(PostCreateInput{Title: "   "}, actor)
	requireAppError(t, err, errs.ErrorValidation)

	_, err = svc.Create(PostCreateInput{Title: strings.Repeat("字", postTitleMaxLen+1)}, actor)
	requireAppError(t, err, errs.ErrorValidation)

	// 显式 slug 走 §4 的字符集与长度校验：大写、空格、首尾连字符都不合法。
	for _, slug := range []string{
		"Bad-Slug", "has space", "-leading", "trailing-", strings.Repeat("a", postSlugMaxLen+1),
	} {
		_, err = svc.Create(PostCreateInput{Title: "合法标题", Slug: slug}, actor)
		requireAppError(t, err, errs.ErrorValidation)
	}

	// 显式 slug 合法时原样采用。
	post, err := svc.Create(PostCreateInput{Title: "合法标题", Slug: "explicit-slug"}, actor)
	require.NoError(t, err)
	assert.Equal(t, "explicit-slug", post.Slug)

	// 自动 slug 超过 40 字符要截断，并丢掉截断留下的尾连字符。
	long := strings.Repeat("a", postSlugAutoLen-1) + " b"
	post, err = svc.Create(PostCreateInput{Title: long}, actor)
	require.NoError(t, err)
	assert.Equal(t, strings.Repeat("a", postSlugAutoLen-1), post.Slug)

	// 中文标题没有拼音库（§17.2）：按 §11 的退路用站点时区时间戳。
	post, err = svc.Create(PostCreateInput{Title: "中文标题"}, actor)
	require.NoError(t, err)
	assert.Regexp(t, regexp.MustCompile(`^post-\d{8}-\d{4}$`), post.Slug)
}

func TestPostService_CreateAutoSlugSuffixOnCollision(t *testing.T) {
	setupEnv(t)
	actor := testActor(t)
	svc := PostService{}

	first, err := svc.Create(PostCreateInput{Title: "Hello World"}, actor)
	require.NoError(t, err)
	assert.Equal(t, "hello-world", first.Slug)

	// 同标题再建一篇：自动 slug 追加 -2，而不是报冲突（§11）。
	second, err := svc.Create(PostCreateInput{Title: "Hello World"}, actor)
	require.NoError(t, err)
	assert.Equal(t, "hello-world-2", second.Slug)
}

func TestPostService_CreateAutoSlugExhausted(t *testing.T) {
	setupEnv(t)
	actor := testActor(t)

	// 占满 base 与 base-2…base-50：自动生成无路可走时必须报 2001，而不是静默重名。
	insertPost(t, "占用", "hello-world", model.PostStatusDraft)
	for i := 2; i <= postSlugCollisionRetries; i++ {
		insertPost(t, "占用", "hello-world-"+strconv.Itoa(i), model.PostStatusDraft)
	}

	_, err := PostService{}.Create(PostCreateInput{Title: "Hello World"}, actor)
	requireAppError(t, err, errs.ErrorSlugConflict)
}

func TestPostService_CreateSlugConflict(t *testing.T) {
	setupEnv(t)
	actor := testActor(t)

	insertPost(t, "已有", "taken-slug", model.PostStatusDraft)

	_, err := PostService{}.Create(PostCreateInput{Title: "新文章", Slug: "taken-slug"}, actor)
	requireAppError(t, err, errs.ErrorSlugConflict)
}

func TestPostService_CreateTagsDedupeAndPersist(t *testing.T) {
	setupEnv(t)
	actor := testActor(t)
	svc := PostService{}

	tagA := insertTag(t, "a标签", "tag-a")
	tagB := insertTag(t, "b标签", "tag-b")
	series := insertSeries(t, "系列", "series-a")

	// 重复 id 去重、非正数丢弃，并保留调用方给的顺序。
	post, err := svc.Create(PostCreateInput{
		Title:       "带标签的文章",
		TagIDs:      []int64{tagB.ID, 0, -1, tagB.ID, tagA.ID},
		SeriesID:    &series.ID,
		SeriesOrder: 7,
	}, actor)
	require.NoError(t, err)
	assert.ElementsMatch(t, []int64{tagA.ID, tagB.ID}, post.TagIDs)

	assert.EqualValues(t, 2, countRows(t, "post_tags", "post_id = ?", post.ID))

	stored := findPostOrFail(t, post.ID)
	require.NotNil(t, stored.SeriesID)
	assert.Equal(t, series.ID, *stored.SeriesID)
	assert.Equal(t, 7, stored.SeriesOrder)
}

func TestPostService_CreateRejectsBadTagIDs(t *testing.T) {
	setupEnv(t)
	actor := testActor(t)
	svc := PostService{}

	// 超过 20 个标签：报 1003 并在 Details 里点名 tagIds，且不能落库半截文章。
	_, err := svc.Create(PostCreateInput{Title: "标签过多", TagIDs: manyIDs(postTagMaxCount + 1)}, actor)
	assertFieldError(t, err, errs.ErrorValidation, "tagIds")
	assert.EqualValues(t, 0, countRows(t, "posts", ""))

	// 不存在的标签 id：同样 1003，并且事务回滚（post_tags 没有外键，必须自己校验）。
	_, err = svc.Create(PostCreateInput{Title: "标签不存在", TagIDs: []int64{999999}}, actor)
	assertFieldError(t, err, errs.ErrorValidation, "tagIds")
	assert.EqualValues(t, 0, countRows(t, "posts", ""))
	assert.EqualValues(t, 0, countRows(t, "post_tags", ""))
}

func TestPostService_CreateRejectsUnknownSeries(t *testing.T) {
	setupEnv(t)
	actor := testActor(t)

	missing := int64(424242)
	_, err := PostService{}.Create(PostCreateInput{Title: "系列不存在", SeriesID: &missing}, actor)
	assertFieldError(t, err, errs.ErrorValidation, "seriesId")
	assert.EqualValues(t, 0, countRows(t, "posts", ""))
}

func TestPostService_CreateCanonicalURLAndExcerpt(t *testing.T) {
	setupEnv(t)
	actor := testActor(t)
	svc := PostService{}

	// 空 canonicalUrl 是合法的"留空"；填了必须是 http(s)。
	for _, bad := range []string{"example.com/a", "ftp://example.com", "javascript:alert(1)"} {
		_, err := svc.Create(PostCreateInput{Title: "链接非法", CanonicalURL: bad}, actor)
		requireAppError(t, err, errs.ErrorValidation)
	}

	post, err := svc.Create(PostCreateInput{
		Title:        "链接合法",
		ContentMD:    "正文",
		CanonicalURL: "  https://example.com/a  ",
		Excerpt:      "  手写摘要  ",
	}, actor)
	require.NoError(t, err)
	assert.Equal(t, "https://example.com/a", post.CanonicalURL)
	assert.Equal(t, "手写摘要", post.Excerpt)

	// 没填 lang 用默认值。
	assert.Equal(t, model.DefaultPostLang, findPostOrFail(t, post.ID).Lang)
}

func TestPostService_CreateSkipsLengthValidation(t *testing.T) {
	setupEnv(t)
	actor := testActor(t)

	// 疑似缺陷：Update 对 lang / seoTitle / seoDescription 都兜了长度上限，
	// Create 却只在 api 层 binding 拦（常量注释声称"service 侧再兜一次"），
	// 于是 service 直调可以写进超长值，两个写入口的校验口径不一致。
	post, err := PostService{}.Create(PostCreateInput{
		Title:          "超长字段",
		Lang:           strings.Repeat("x", postLangMaxLen+1),
		SEOTitle:       strings.Repeat("s", postSEOTitleMaxLen+1),
		SEODescription: strings.Repeat("d", postSEODescriptionMaxLen+1),
	}, actor)
	require.NoError(t, err)

	stored := findPostOrFail(t, post.ID)
	assert.Len(t, []rune(stored.Lang), postLangMaxLen+1)
	assert.Len(t, []rune(stored.SEOTitle), postSEOTitleMaxLen+1)
	assert.Len(t, []rune(stored.SEODescription), postSEODescriptionMaxLen+1)
}

func TestPostService_UpdateFieldsAndClear(t *testing.T) {
	setupEnv(t)
	actor := testActor(t)
	svc := PostService{}

	cover := insertMediaRow(t, strings.Repeat("a", 64), "aa/cover", "webp")
	og := insertMediaRow(t, strings.Repeat("b", 64), "bb/og", "webp")
	series := insertSeries(t, "系列", "series-u")

	post, err := svc.Create(PostCreateInput{Title: "原题", ContentMD: "旧正文"}, actor)
	require.NoError(t, err)

	updated, err := svc.Update(post.ID, PostUpdateInput{
		Title:          ptrString("  新标题  "),
		Excerpt:        ptrString("手写摘要"),
		ContentMD:      ptrString("# 新正文\n\nhello world"),
		CoverMediaID:   &cover.ID,
		OGMediaID:      &og.ID,
		SeriesID:       &series.ID,
		SeriesOrder:    ptrInt(3),
		IsPinned:       ptrBool(true),
		AllowComments:  ptrBool(false),
		SEOTitle:       ptrString("  SEO 标题 "),
		SEODescription: ptrString("SEO 描述"),
		CanonicalURL:   ptrString("https://example.com/p"),
		Lang:           ptrString("en-US"),
	}, actor)
	require.NoError(t, err)

	want := render.Markdown("# 新正文\n\nhello world")
	stored := findPostOrFail(t, post.ID)
	assert.Equal(t, "新标题", stored.Title)
	assert.Equal(t, "手写摘要", stored.Excerpt)
	assert.Equal(t, want.HTML, stored.ContentHTML)
	assert.Equal(t, want.WordCount, stored.WordCount)
	assert.Equal(t, want.ReadingTime, stored.ReadingTime)
	require.NotNil(t, stored.CoverMediaID)
	assert.Equal(t, cover.ID, *stored.CoverMediaID)
	require.NotNil(t, stored.OGMediaID)
	assert.Equal(t, og.ID, *stored.OGMediaID)
	require.NotNil(t, stored.SeriesID)
	assert.Equal(t, series.ID, *stored.SeriesID)
	assert.Equal(t, 3, stored.SeriesOrder)
	assert.True(t, stored.IsPinned)
	assert.False(t, stored.AllowComments)
	assert.Equal(t, "SEO 标题", stored.SEOTitle)
	assert.Equal(t, "SEO 描述", stored.SEODescription)
	assert.Equal(t, "https://example.com/p", stored.CanonicalURL)
	assert.Equal(t, "en-US", stored.Lang)

	// 正常路径的返回值走 Get：封面 / OG 图引用一并回显。
	require.NotNil(t, updated.Cover)
	assert.Equal(t, cover.ID, updated.Cover.ID)
	require.NotNil(t, updated.OGImage)
	assert.Equal(t, og.ID, updated.OGImage.ID)

	// 外键传 0 = 显式清空（置 NULL），这是 PostUpdateInput 的约定。
	_, err = svc.Update(post.ID, PostUpdateInput{
		CoverMediaID: ptrInt64(0),
		OGMediaID:    ptrInt64(0),
		SeriesID:     ptrInt64(0),
	}, actor)
	require.NoError(t, err)

	stored = findPostOrFail(t, post.ID)
	assert.Nil(t, stored.CoverMediaID)
	assert.Nil(t, stored.OGMediaID)
	assert.Nil(t, stored.SeriesID)
}

func TestPostService_UpdateLangFallsBackToDefault(t *testing.T) {
	setupEnv(t)
	actor := testActor(t)
	svc := PostService{}

	post, err := svc.Create(PostCreateInput{Title: "语言", Lang: "en-US"}, actor)
	require.NoError(t, err)

	// 传空串 = 回到站点默认语言，而不是写进一个空 lang（空 lang 会让前端语言切换失效）。
	updated, err := svc.Update(post.ID, PostUpdateInput{Lang: ptrString("  ")}, actor)
	require.NoError(t, err)
	assert.Equal(t, model.DefaultPostLang, updated.Lang)
}

func TestPostService_UpdateTagIDsPointerSemantics(t *testing.T) {
	setupEnv(t)
	actor := testActor(t)
	svc := PostService{}

	tagA := insertTag(t, "a", "tag-a")
	tagB := insertTag(t, "b", "tag-b")
	tagC := insertTag(t, "c", "tag-c")

	post, err := svc.Create(PostCreateInput{Title: "标签替换", TagIDs: []int64{tagA.ID, tagB.ID}}, actor)
	require.NoError(t, err)
	require.EqualValues(t, 2, countRows(t, "post_tags", "post_id = ?", post.ID))

	// nil = 不改：只改标题时标签原样保留。
	_, err = svc.Update(post.ID, PostUpdateInput{Title: ptrString("改标题")}, actor)
	require.NoError(t, err)
	assert.EqualValues(t, 2, countRows(t, "post_tags", "post_id = ?", post.ID))

	// [] = 清空。
	_, err = svc.Update(post.ID, PostUpdateInput{TagIDs: ptrInt64Slice([]int64{})}, actor)
	require.NoError(t, err)
	assert.EqualValues(t, 0, countRows(t, "post_tags", "post_id = ?", post.ID))

	// 有值 = 整体替换（重复 id 先归一化）。
	updated, err := svc.Update(post.ID, PostUpdateInput{
		TagIDs: ptrInt64Slice([]int64{tagC.ID, tagC.ID, tagA.ID}),
	}, actor)
	require.NoError(t, err)
	assert.EqualValues(t, 2, countRows(t, "post_tags", "post_id = ?", post.ID))
	assert.ElementsMatch(t, []int64{tagA.ID, tagC.ID}, updated.TagIDs)

	// 未知 id：报 1003，且已有关联不被改动（校验与替换在同一个事务里）。
	_, err = svc.Update(post.ID, PostUpdateInput{TagIDs: ptrInt64Slice([]int64{tagA.ID, 999999})}, actor)
	assertFieldError(t, err, errs.ErrorValidation, "tagIds")
	assert.ElementsMatch(t, []int64{tagA.ID, tagC.ID}, mustTagIDs(t, post.ID))

	// 数量超限同样 1003。
	_, err = svc.Update(post.ID, PostUpdateInput{TagIDs: ptrInt64Slice(manyIDs(postTagMaxCount + 1))}, actor)
	assertFieldError(t, err, errs.ErrorValidation, "tagIds")
}

func TestPostService_UpdateSlugRedirectRules(t *testing.T) {
	setupEnv(t)
	actor := testActor(t)
	svc := PostService{}

	// 草稿改 slug：直接生效，不写跳转（旧地址本来就没公开过）。
	draft := insertPost(t, "草稿", "draft-slug", model.PostStatusDraft)
	updated, err := svc.Update(draft.ID, PostUpdateInput{Slug: ptrString("draft-slug-2")}, actor)
	require.NoError(t, err)
	assert.Equal(t, "draft-slug-2", updated.Slug)
	assert.EqualValues(t, 0, countRows(t, "redirects", ""))

	// 同名 slug 不算变更，也不需要跳转。
	_, err = svc.Update(draft.ID, PostUpdateInput{Slug: ptrString("draft-slug-2")}, actor)
	require.NoError(t, err)
	assert.EqualValues(t, 0, countRows(t, "redirects", ""))

	// 已发布文章 slug 锁定：不建 301 就改 → 2004。
	published := insertPost(t, "已发布", "pub-slug", model.PostStatusPublished)
	_, err = svc.Update(published.ID, PostUpdateInput{Slug: ptrString("pub-slug-2")}, actor)
	requireAppError(t, err, errs.ErrorSlugImmutable)
	assert.Equal(t, "pub-slug", findPostOrFail(t, published.ID).Slug)

	// 带上 CreateRedirect → 写 301，旧路径指向新路径。
	_, err = svc.Update(published.ID, PostUpdateInput{Slug: ptrString("pub-slug-2"), CreateRedirect: true}, actor)
	require.NoError(t, err)
	assert.Equal(t, "pub-slug-2", findPostOrFail(t, published.ID).Slug)
	assert.EqualValues(t, 1, countRows(t, "redirects",
		"from_path = ? AND to_path = ? AND status = 301", "/posts/pub-slug", "/posts/pub-slug-2"))

	// 已归档同样锁定（isPostSlugLocked 覆盖 published 与 archived）。
	archived := insertPost(t, "已归档", "arch-slug", model.PostStatusArchived)
	_, err = svc.Update(archived.ID, PostUpdateInput{Slug: ptrString("arch-slug-2")}, actor)
	requireAppError(t, err, errs.ErrorSlugImmutable)

	// 撞上别人的 slug → 2001。
	_, err = svc.Update(draft.ID, PostUpdateInput{Slug: ptrString("pub-slug-2")}, actor)
	requireAppError(t, err, errs.ErrorSlugConflict)

	// 非法字符集 → 1003。
	_, err = svc.Update(draft.ID, PostUpdateInput{Slug: ptrString("Bad Slug")}, actor)
	requireAppError(t, err, errs.ErrorValidation)
}

func TestPostService_UpdateStatusTransitions(t *testing.T) {
	setupEnv(t)
	actor := testActor(t)
	svc := PostService{}

	// 已发布 → 草稿：published_at 必须清空，审计动作是 unpublish。
	published := insertPost(t, "已发布", "st-pub", model.PostStatusPublished)
	updated, err := svc.Update(published.ID, PostUpdateInput{Status: ptrString(model.PostStatusDraft)}, actor)
	require.NoError(t, err)
	assert.Equal(t, model.PostStatusDraft, updated.Status)
	assert.Nil(t, findPostOrFail(t, published.ID).PublishedAt)
	assert.EqualValues(t, 1, countRows(t, "audit_logs", "action = ?", AuditActionPostUnpublish))

	// 草稿 → 归档：只有已发布文章可以归档 → 1004。
	_, err = svc.Update(published.ID, PostUpdateInput{Status: ptrString(model.PostStatusArchived)}, actor)
	requireAppError(t, err, errs.ErrorConflict)

	// 已发布 → 归档：允许，审计动作是 archive，published_at 保留（归档不是取消发布）。
	other := insertPost(t, "另一篇", "st-pub-2", model.PostStatusPublished)
	updated, err = svc.Update(other.ID, PostUpdateInput{Status: ptrString(model.PostStatusArchived)}, actor)
	require.NoError(t, err)
	assert.Equal(t, model.PostStatusArchived, updated.Status)
	assert.NotNil(t, findPostOrFail(t, other.ID).PublishedAt)
	assert.EqualValues(t, 1, countRows(t, "audit_logs", "action = ?", AuditActionPostArchive))

	// 发布与定时发布只能走专用接口 → 1003。
	for _, status := range []string{model.PostStatusPublished, model.PostStatusScheduled} {
		_, err = svc.Update(published.ID, PostUpdateInput{Status: ptrString(status)}, actor)
		requireAppError(t, err, errs.ErrorValidation)
	}

	// 未知状态 → 1003。
	_, err = svc.Update(published.ID, PostUpdateInput{Status: ptrString("bogus")}, actor)
	requireAppError(t, err, errs.ErrorValidation)

	// 状态没变化时不算改动：不写审计、不打事务。
	before := countRows(t, "audit_logs", "")
	_, err = svc.Update(published.ID, PostUpdateInput{Status: ptrString(model.PostStatusDraft)}, actor)
	require.NoError(t, err)
	assert.EqualValues(t, before, countRows(t, "audit_logs", ""))
}

func TestPostService_UpdateValidation(t *testing.T) {
	setupEnv(t)
	actor := testActor(t)
	svc := PostService{}

	post := insertPost(t, "校验", "val-slug", model.PostStatusDraft)

	cases := []struct {
		name string
		in   PostUpdateInput
	}{
		{"标题为空", PostUpdateInput{Title: ptrString("   ")}},
		{"标题过长", PostUpdateInput{Title: ptrString(strings.Repeat("字", postTitleMaxLen+1))}},
		{"seoTitle 过长", PostUpdateInput{SEOTitle: ptrString(strings.Repeat("s", postSEOTitleMaxLen+1))}},
		{"seoDescription 过长", PostUpdateInput{SEODescription: ptrString(strings.Repeat("d", postSEODescriptionMaxLen+1))}},
		{"lang 过长", PostUpdateInput{Lang: ptrString(strings.Repeat("x", postLangMaxLen+1))}},
		{"canonicalUrl 非法", PostUpdateInput{CanonicalURL: ptrString("not-a-url")}},
		{"tagIds 过多", PostUpdateInput{TagIDs: ptrInt64Slice(manyIDs(postTagMaxCount + 1))}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := svc.Update(post.ID, tc.in, actor)
			requireAppError(t, err, errs.ErrorValidation)
		})
	}

	// 校验失败不该留下半截写入。
	stored := findPostOrFail(t, post.ID)
	assert.Equal(t, "校验", stored.Title)
	assert.Empty(t, stored.SEOTitle)
}

func TestPostService_UpdateNotFoundAndNoop(t *testing.T) {
	setupEnv(t)
	actor := testActor(t)
	svc := PostService{}

	_, err := svc.Update(999999, PostUpdateInput{Title: ptrString("不存在")}, actor)
	requireAppError(t, err, errs.ErrorNotFound)

	// 软删的文章等同不存在。
	post := insertPost(t, "已删", "gone-slug", model.PostStatusDraft)
	require.NoError(t, svc.Delete(post.ID, actor))
	_, err = svc.Update(post.ID, PostUpdateInput{Title: ptrString("改不动")}, actor)
	requireAppError(t, err, errs.ErrorNotFound)

	// 全 nil 入参 = 空操作：直接返回实体，不写审计。
	live := insertPost(t, "原样", "noop-slug", model.PostStatusDraft)
	got, err := svc.Update(live.ID, PostUpdateInput{}, actor)
	require.NoError(t, err)
	assert.Equal(t, "原样", got.Title)
	assert.EqualValues(t, 0, countRows(t, "audit_logs", "action = ?", AuditActionPostUpdate))

	// 空操作也必须返回和正常路径一样的形状：改过字段时 tagIds 是 []、cover/ogImage 是对象，
	// 空操作时不能退化成 null（D18 修复：空操作同样走 Get）。
	assert.NotNil(t, got.TagIDs)
	assert.Empty(t, got.TagIDs)
	assert.Nil(t, got.Cover)
	assert.Nil(t, got.OGImage)
}

func TestPostService_UpdateExcerptRecompute(t *testing.T) {
	setupEnv(t)
	actor := testActor(t)
	svc := PostService{}

	// 摘要此前是自动生成的：正文一变，摘要跟着重算。
	auto := insertPost(t, "自动摘要", "ex-auto", model.PostStatusDraft)
	oldAuto := autoExcerpt(auto.ContentText)
	require.NoError(t, global.DB.Exec(`UPDATE posts SET excerpt = ? WHERE id = ?`, oldAuto, auto.ID).Error)

	newMD := "换了正文 hello"
	_, err := svc.Update(auto.ID, PostUpdateInput{ContentMD: ptrString(newMD)}, actor)
	require.NoError(t, err)
	assert.Equal(t, autoExcerpt(render.Markdown(newMD).Text), findPostOrFail(t, auto.ID).Excerpt)

	// 手写摘要不受正文变更影响。
	manual := insertPost(t, "手写摘要", "ex-manual", model.PostStatusDraft)
	require.NoError(t, global.DB.Exec(`UPDATE posts SET excerpt = ? WHERE id = ?`, "我手写的", manual.ID).Error)

	_, err = svc.Update(manual.ID, PostUpdateInput{ContentMD: ptrString("又换正文")}, actor)
	require.NoError(t, err)
	assert.Equal(t, "我手写的", findPostOrFail(t, manual.ID).Excerpt)

	// 显式传摘要：以传入值为准（两边空白都裁掉）。
	_, err = svc.Update(manual.ID, PostUpdateInput{Excerpt: ptrString("  新的手写  ")}, actor)
	require.NoError(t, err)
	assert.Equal(t, "新的手写", findPostOrFail(t, manual.ID).Excerpt)
}

func TestPostService_GetTagIDsAndMediaRefs(t *testing.T) {
	setupEnv(t)
	actor := testActor(t)
	svc := PostService{}

	cover := insertMediaRow(t, strings.Repeat("c", 64), "cc/cover", "jpg")
	og := insertMediaRow(t, strings.Repeat("d", 64), "dd/og", "png")

	post, err := svc.Create(PostCreateInput{
		Title:        "详情",
		CoverMediaID: &cover.ID,
		OGMediaID:    &og.ID,
	}, actor)
	require.NoError(t, err)

	got, err := svc.Get(post.ID)
	require.NoError(t, err)
	assert.NotNil(t, got.TagIDs, "无标签时 tagIds 是空数组而不是 null")
	assert.Empty(t, got.TagIDs)
	require.NotNil(t, got.Cover)
	assert.Equal(t, cover.ID, got.Cover.ID)
	assert.NotEmpty(t, got.Cover.URL)
	require.NotNil(t, got.OGImage)
	assert.Equal(t, og.ID, got.OGImage.ID)

	// 外键指向的媒体被删掉后：回显 null，而不是让详情接口报错。
	require.NoError(t, model.DeleteMedia(global.DB, cover.ID))
	require.NoError(t, model.DeleteMedia(global.DB, og.ID))

	got, err = svc.Get(post.ID)
	require.NoError(t, err)
	assert.Nil(t, got.Cover)
	assert.Nil(t, got.OGImage)

	// 不存在的 id → 1002。
	_, err = svc.Get(999999)
	requireAppError(t, err, errs.ErrorNotFound)

	// 标签回显按标签名升序。
	tagB := insertTag(t, "b", "g-tag-b")
	tagA := insertTag(t, "a", "g-tag-a")
	_, err = svc.Update(post.ID, PostUpdateInput{TagIDs: ptrInt64Slice([]int64{tagB.ID, tagA.ID})}, actor)
	require.NoError(t, err)

	got, err = svc.Get(post.ID)
	require.NoError(t, err)
	assert.Equal(t, []int64{tagA.ID, tagB.ID}, got.TagIDs)
}

func TestPostService_ListFilterKeywordPaging(t *testing.T) {
	setupEnv(t)
	svc := PostService{}

	insertPost(t, "Alpha 一", "list-alpha", model.PostStatusDraft)
	insertPost(t, "Beta 二", "list-beta", model.PostStatusPublished)
	insertPost(t, "Gamma 三", "list-gamma", model.PostStatusArchived)
	insertPost(t, "Delta 四", "list-delta", model.PostStatusScheduled)

	all, total, err := svc.List("", "", 1, 20)
	require.NoError(t, err)
	assert.Len(t, all, 4)
	assert.EqualValues(t, 4, total)

	// status 为空与 all 等价：都不筛状态。
	all, total, err = svc.List("all", "", 1, 20)
	require.NoError(t, err)
	assert.Len(t, all, 4)
	assert.EqualValues(t, 4, total)

	drafts, total, err := svc.List(model.PostStatusDraft, "", 1, 20)
	require.NoError(t, err)
	assert.Len(t, drafts, 1)
	assert.EqualValues(t, 1, total)
	assert.Equal(t, "list-alpha", drafts[0].Slug)

	_, _, err = svc.List("bogus", "", 1, 20)
	requireAppError(t, err, errs.ErrorValidation)

	// keyword 同时匹配标题与 slug。
	byTitle, total, err := svc.List("", "gamma", 1, 20)
	require.NoError(t, err)
	assert.EqualValues(t, 1, total)
	require.Len(t, byTitle, 1)
	assert.Equal(t, "list-gamma", byTitle[0].Slug)

	bySlug, total, err := svc.List("", "list-beta", 1, 20)
	require.NoError(t, err)
	assert.EqualValues(t, 1, total)
	require.Len(t, bySlug, 1)
	assert.Equal(t, "list-beta", bySlug[0].Slug)

	// LIKE 通配符被转义：搜 % 不会把整表捞出来。
	_, total, err = svc.List("", "%", 1, 20)
	require.NoError(t, err)
	assert.EqualValues(t, 0, total)

	// 分页：total 是命中总数，list 只给这一页。
	page1, total, err := svc.List("", "", 1, 2)
	require.NoError(t, err)
	assert.Len(t, page1, 2)
	assert.EqualValues(t, 4, total)

	page2, total, err := svc.List("", "", 2, 2)
	require.NoError(t, err)
	assert.Len(t, page2, 2)
	assert.EqualValues(t, 4, total)

	page3, _, err := svc.List("", "", 3, 2)
	require.NoError(t, err)
	assert.Empty(t, page3)
}

func TestPostService_DeleteSoftDelete(t *testing.T) {
	setupEnv(t)
	actor := testActor(t)
	svc := PostService{}

	post := insertPost(t, "待删", "del-slug", model.PostStatusPublished)

	require.NoError(t, svc.Delete(post.ID, actor))

	// 软删：行还在，deleted_at 被写上；管理端列表与详情都不再可见。
	assert.EqualValues(t, 1, countRows(t, "posts", "id = ? AND deleted_at IS NOT NULL", post.ID))
	_, total, err := svc.List("", "", 1, 20)
	require.NoError(t, err)
	assert.EqualValues(t, 0, total)

	_, err = svc.Get(post.ID)
	requireAppError(t, err, errs.ErrorNotFound)

	// 公开端对已删除文章返回 410（知道它存在过），而不是 404。
	_, err = svc.PublicBySlug("del-slug")
	requireAppError(t, err, errs.ErrorGone)

	// 重复删除 → 1002，且不重复写审计。
	requireAppError(t, svc.Delete(post.ID, actor), errs.ErrorNotFound)
	assert.EqualValues(t, 1, countRows(t, "audit_logs", "action = ?", AuditActionPostDelete))
}

func TestPostService_PublishLifecycle(t *testing.T) {
	setupEnv(t)
	actor := testActor(t)
	svc := PostService{}

	md := "# 发布\n\n正文内容。"
	post, err := svc.Create(PostCreateInput{Title: "发布流程", ContentMD: md}, actor)
	require.NoError(t, err)
	require.Equal(t, 1, post.Revision)

	published, err := svc.Publish(post.ID, "首次发布", actor)
	require.NoError(t, err)
	assert.Equal(t, model.PostStatusPublished, published.Status)
	require.NotNil(t, published.PublishedAt)
	assert.Equal(t, 2, published.Revision)

	stored := findPostOrFail(t, post.ID)
	require.NotNil(t, stored.PublishedAt)
	assert.WithinDuration(t, time.Now(), stored.PublishedAt.Time(), time.Minute)
	assert.Equal(t, render.Markdown(md).HTML, stored.ContentHTML)
	assert.EqualValues(t, 1, countRows(t, "post_revisions",
		"post_id = ? AND revision_no = 2 AND note = ?", post.ID, "首次发布"))

	// 发布后公开端可见。
	public, err := svc.PublicBySlug(post.Slug)
	require.NoError(t, err)
	assert.Equal(t, post.ID, public.ID)

	// 内容没变再发布一次：按 §5「重复发布」返回 1004，也不该多写版本。
	_, err = svc.Publish(post.ID, "重复", actor)
	requireAppError(t, err, errs.ErrorConflict)
	assert.EqualValues(t, 1, countRows(t, "post_revisions", "post_id = ?", post.ID))

	// 取消发布：回草稿、清 published_at；公开端立刻不可见。
	unpublished, err := svc.Unpublish(post.ID, actor)
	require.NoError(t, err)
	assert.Equal(t, model.PostStatusDraft, unpublished.Status)
	assert.Nil(t, findPostOrFail(t, post.ID).PublishedAt)
	_, err = svc.PublicBySlug(post.Slug)
	requireAppError(t, err, errs.ErrorNotFound)

	// 已是草稿再取消 → 1004。
	_, err = svc.Unpublish(post.ID, actor)
	requireAppError(t, err, errs.ErrorConflict)

	// 归档文章不能发布 → 1004。
	archived := insertPost(t, "归档", "pub-archived", model.PostStatusArchived)
	_, err = svc.Publish(archived.ID, "", actor)
	requireAppError(t, err, errs.ErrorConflict)
}

func TestPostService_PublishContentChangeKeepsPublishedAt(t *testing.T) {
	setupEnv(t)
	actor := testActor(t)
	svc := PostService{}

	post, err := svc.Create(PostCreateInput{Title: "再发布", ContentMD: "第一版"}, actor)
	require.NoError(t, err)
	_, err = svc.Publish(post.ID, "第一版", actor)
	require.NoError(t, err)

	// 把发布时间挪到过去：验证"再发布"只补版本，不会把文章顶到列表最前。
	old := dbtime.Now().Add(-2 * time.Hour)
	require.NoError(t, global.DB.Exec(
		`UPDATE posts SET published_at = ? WHERE id = ?`, int64(old), post.ID).Error)

	_, err = svc.Update(post.ID, PostUpdateInput{ContentMD: ptrString("第二版")}, actor)
	require.NoError(t, err)

	republished, err := svc.Publish(post.ID, "线上改字", actor)
	require.NoError(t, err)
	assert.Equal(t, 3, republished.Revision)
	require.NotNil(t, republished.PublishedAt)
	assert.Equal(t, int64(old), int64(*republished.PublishedAt))
	assert.EqualValues(t, 2, countRows(t, "post_revisions", "post_id = ?", post.ID))
}

func TestPostService_PublishPublishedWithoutPublishedAt(t *testing.T) {
	setupEnv(t)
	actor := testActor(t)
	svc := PostService{}

	// 历史脏数据：状态是已发布却没有 published_at，也没有任何版本快照。
	post := insertPost(t, "缺时间", "no-pub-at", model.PostStatusPublished)
	require.NoError(t, global.DB.Exec(`UPDATE posts SET published_at = NULL WHERE id = ?`, post.ID).Error)

	// 没有版本可比（跳过"重复发布"判断），也没有旧发布时间可保留：补当前时间并记一个新版本。
	republished, err := svc.Publish(post.ID, "补时间", actor)
	require.NoError(t, err)
	require.NotNil(t, republished.PublishedAt)
	assert.WithinDuration(t, time.Now(), republished.PublishedAt.Time(), time.Minute)
	assert.Equal(t, 2, republished.Revision)
}

func TestPostService_PublishUnpublishNotFound(t *testing.T) {
	setupEnv(t)
	actor := testActor(t)
	svc := PostService{}

	// 文章不存在 → 1002（D18 修复）：此前 model.ErrNotFound 被包成普通错误，
	// 调用方拿到的是 1006 内部错误，与 Get / Update / Delete / Revisions 的口径不一致。
	requireAppError(t, func() error { _, err := svc.Publish(999999, "", actor); return err }(), errs.ErrorNotFound)
	requireAppError(t, func() error { _, err := svc.Unpublish(999999, actor); return err }(), errs.ErrorNotFound)

	// 软删的文章同样按不存在处理。
	post := insertPost(t, "已删", "publish-gone", model.PostStatusDraft)
	require.NoError(t, svc.Delete(post.ID, actor))
	requireAppError(t, func() error { _, err := svc.Publish(post.ID, "", actor); return err }(), errs.ErrorNotFound)
	requireAppError(t, func() error { _, err := svc.Unpublish(post.ID, actor); return err }(), errs.ErrorNotFound)
}

func TestPostService_PreviewMatchesPublished(t *testing.T) {
	setupEnv(t)
	actor := testActor(t)
	svc := PostService{}

	md := "## 小标题\n\n预览与发布必须一致。\n\n- 甲\n- 乙\n"
	preview := svc.Preview(md, "")
	want := render.Markdown(md)

	assert.Equal(t, want.HTML, preview.HTML)
	assert.Equal(t, want.Text, preview.Text)
	assert.Equal(t, want.WordCount, preview.WordCount)
	assert.Equal(t, want.ReadingTime, preview.ReadingTime)
	assert.Equal(t, autoExcerpt(want.Text), preview.Excerpt)
	assert.Positive(t, preview.WordCount)

	// 显式摘要优先，两边空白裁掉。
	assert.Equal(t, "手写", svc.Preview(md, "  手写  ").Excerpt)

	// §16 G2：预览 HTML 必须与发布后落库的 content_html 逐字一致。
	post, err := svc.Create(PostCreateInput{Title: "预览一致性", ContentMD: md}, actor)
	require.NoError(t, err)
	_, err = svc.Publish(post.ID, "发布", actor)
	require.NoError(t, err)

	stored := findPostOrFail(t, post.ID)
	assert.Equal(t, preview.HTML, stored.ContentHTML)
	assert.Equal(t, preview.WordCount, stored.WordCount)
	assert.Equal(t, preview.ReadingTime, stored.ReadingTime)
}

func TestPostService_Revisions(t *testing.T) {
	setupEnv(t)
	actor := testActor(t)
	svc := PostService{}

	post, err := svc.Create(PostCreateInput{Title: "版本", ContentMD: "v1"}, actor)
	require.NoError(t, err)
	_, err = svc.Publish(post.ID, "v1", actor)
	require.NoError(t, err)

	// 更新正文不写版本：只有发布（与回滚）才留快照。
	_, err = svc.Update(post.ID, PostUpdateInput{ContentMD: ptrString("v2")}, actor)
	require.NoError(t, err)
	assert.EqualValues(t, 1, countRows(t, "post_revisions", "post_id = ?", post.ID))

	_, err = svc.Publish(post.ID, "v2", actor)
	require.NoError(t, err)

	revisions, err := svc.Revisions(post.ID)
	require.NoError(t, err)
	require.Len(t, revisions, 2)
	assert.Equal(t, 3, revisions[0].RevisionNo) // 倒序：最新在前
	assert.Equal(t, 2, revisions[1].RevisionNo)
	assert.Equal(t, "v2", revisions[0].ContentMD)
	assert.Equal(t, "v2", revisions[0].Note)

	_, err = svc.Revisions(999999)
	requireAppError(t, err, errs.ErrorNotFound)
}

func TestPostService_SearchAndPublicList(t *testing.T) {
	setupEnv(t)
	svc := PostService{}

	published := insertPost(t, "Hello World", "hello-world", model.PostStatusPublished)
	insertPost(t, "Hello Draft", "hello-draft", model.PostStatusDraft)
	insertPost(t, "Hello Archived", "hello-archived", model.PostStatusArchived)

	// ≥3 字走 FTS5 trigram：只出已发布。
	posts, total, err := svc.Search("hello", 1, 20)
	require.NoError(t, err)
	assert.EqualValues(t, 1, total)
	require.Len(t, posts, 1)
	assert.Equal(t, published.ID, posts[0].ID)

	// 1–2 字走 LIKE 兜底（trigram 索引覆盖不到这么短的串）。
	posts, total, err = svc.Search("he", 1, 20)
	require.NoError(t, err)
	assert.EqualValues(t, 1, total)
	require.Len(t, posts, 1)
	assert.Equal(t, published.ID, posts[0].ID)

	// 搜不到时返回空切片与 0，而不是 nil 或报错。
	posts, total, err = svc.Search("zzzz", 1, 20)
	require.NoError(t, err)
	assert.EqualValues(t, 0, total)
	assert.NotNil(t, posts)
	assert.Empty(t, posts)

	// 公开列表：草稿与归档都不可见，置顶优先。
	second := insertPost(t, "Second Post", "second-post", model.PostStatusPublished)
	require.NoError(t, model.UpdatePostFields(global.DB, second.ID, map[string]any{"is_pinned": true}))

	items, total, err := svc.PublicList(1, 20)
	require.NoError(t, err)
	assert.EqualValues(t, 2, total)
	require.Len(t, items, 2)
	assert.Equal(t, second.ID, items[0].ID)

	page1, total, err := svc.PublicList(1, 1)
	require.NoError(t, err)
	assert.Len(t, page1, 1)
	assert.EqualValues(t, 2, total)
}

func TestPostService_PublicBySlugVisibility(t *testing.T) {
	setupEnv(t)
	actor := testActor(t)
	svc := PostService{}

	draft := insertPost(t, "草稿", "vis-draft", model.PostStatusDraft)
	published := insertPost(t, "已发布", "vis-published", model.PostStatusPublished)
	archived := insertPost(t, "归档", "vis-archived", model.PostStatusArchived)
	scheduled := insertPost(t, "定时", "vis-scheduled", model.PostStatusScheduled)
	deleted := insertPost(t, "已删", "vis-deleted", model.PostStatusPublished)

	got, err := svc.PublicBySlug("vis-published")
	require.NoError(t, err)
	assert.Equal(t, published.ID, got.ID)

	// 草稿与定时文章不泄露存在性：404。
	for _, slug := range []string{"vis-draft", "vis-scheduled"} {
		_, err = svc.PublicBySlug(slug)
		requireAppError(t, err, errs.ErrorNotFound)
	}

	// 归档：410。
	_, err = svc.PublicBySlug("vis-archived")
	requireAppError(t, err, errs.ErrorGone)

	// 回收站：410。
	require.NoError(t, svc.Delete(deleted.ID, actor))
	_, err = svc.PublicBySlug("vis-deleted")
	requireAppError(t, err, errs.ErrorGone)

	// 完全不存在：404。
	_, err = svc.PublicBySlug("nope")
	requireAppError(t, err, errs.ErrorNotFound)

	assert.NotZero(t, draft.ID)
	assert.NotZero(t, archived.ID)
	assert.NotZero(t, scheduled.ID)
}

func TestPostService_AttachTaxonomy(t *testing.T) {
	setupEnv(t)
	actor := testActor(t)
	svc := PostService{}

	tagA := insertTag(t, "alpha", "at-tag-a")
	tagB := insertTag(t, "beta", "at-tag-b")
	series := insertSeries(t, "系列名", "at-series")

	withTaxonomy, err := svc.Create(PostCreateInput{
		Title:    "有标签有系列",
		TagIDs:   []int64{tagA.ID, tagB.ID},
		SeriesID: &series.ID,
	}, actor)
	require.NoError(t, err)

	plain, err := svc.Create(PostCreateInput{Title: "无标签无系列"}, actor)
	require.NoError(t, err)

	items := []model.PostListItem{withTaxonomy.ListItem(), plain.ListItem()}
	require.NoError(t, svc.AttachTaxonomy(items))

	require.Len(t, items[0].Tags, 2)
	assert.Equal(t, "alpha", items[0].Tags[0].Name)
	assert.Equal(t, "beta", items[0].Tags[1].Name)
	assert.Equal(t, "系列名", items[0].SeriesName)

	// 列表项没有标签时必须是空切片而不是 nil（JSON 里是 [] 而不是 null）。
	require.NotNil(t, items[1].Tags)
	assert.Empty(t, items[1].Tags)
	assert.Empty(t, items[1].SeriesName)

	// 空列表直接返回，不打库。
	require.NoError(t, svc.AttachTaxonomy(nil))
}

func TestContentService_RedirectFor(t *testing.T) {
	setupEnv(t)
	svc := ContentService{}

	// 未命中返回 (nil, nil)：公开边缘靠它决定"继续找静态文件"。
	hit, err := svc.RedirectFor("/posts/never")
	require.NoError(t, err)
	assert.Nil(t, hit)

	require.NoError(t, model.EnsureRedirect(global.DB, "/posts/old", "/posts/new", 301, dbtime.Now()))

	hit, err = svc.RedirectFor("/posts/old")
	require.NoError(t, err)
	require.NotNil(t, hit)
	assert.Equal(t, "/posts/new", hit.ToPath)
	assert.Equal(t, 301, hit.Status)

	// 命中顺手把 hits +1（累加失败只记日志，不影响跳转本身）。
	assert.EqualValues(t, 1, countRows(t, "redirects", "from_path = ? AND hits = 1", "/posts/old"))

	_, err = svc.RedirectFor("/posts/old")
	require.NoError(t, err)
	assert.EqualValues(t, 1, countRows(t, "redirects", "from_path = ? AND hits = 2", "/posts/old"))
}

// TestPostService_DatabaseErrorsAreSurfaced 用故障注入确认错误语义：
// 只有 model.ErrNotFound 才允许翻成 1002/1007，其余基础设施失败必须原样包成普通错误，
// 否则"库坏了"会被前端当成"文章不存在"。
//
// 注入手段只有两种，都不改被测代码：DROP TABLE 制造查询失败；
// BEFORE UPDATE 触发器制造写入失败（软删与字段更新走的都是 UPDATE）。
func TestPostService_DatabaseErrorsAreSurfaced(t *testing.T) {
	setupEnv(t)
	actor := testActor(t)
	svc := PostService{}

	tag := insertTag(t, "fault", "fault-tag")
	series := insertSeries(t, "故障系列", "fault-series")
	post, err := svc.Create(PostCreateInput{
		Title:    "故障注入",
		TagIDs:   []int64{tag.ID},
		SeriesID: &series.ID,
	}, actor)
	require.NoError(t, err)

	// 封面引用的媒体查不动：详情要报错，而不是静默当成"没设封面"。
	cover := insertMediaRow(t, strings.Repeat("e", 64), "ee/cover", "webp")
	withCover, err := svc.Create(PostCreateInput{Title: "封面", CoverMediaID: &cover.ID}, actor)
	require.NoError(t, err)
	require.NoError(t, global.DB.Exec(`DROP TABLE media`).Error)
	_, err = svc.Get(withCover.ID)
	assertWrappedError(t, err, "查询媒体失败")

	// post_tags 查不动：详情、列表补齐、标签替换都要报错。
	require.NoError(t, global.DB.Exec(`DROP TABLE post_tags`).Error)
	_, err = svc.Get(post.ID)
	assertWrappedError(t, err, "查询文章标签失败")
	assertWrappedError(t, svc.AttachTaxonomy([]model.PostListItem{post.ListItem()}), "查询文章标签失败")
	_, err = svc.Update(post.ID, PostUpdateInput{TagIDs: ptrInt64Slice([]int64{tag.ID})}, actor)
	assertWrappedError(t, err, "保存文章失败")

	// series 查不动：不能把"查询失败"当成 seriesId 不存在（那是 1003）。
	require.NoError(t, global.DB.Exec(`DROP TABLE series`).Error)
	_, err = svc.Create(PostCreateInput{Title: "系列故障", SeriesID: &series.ID}, actor)
	assertWrappedError(t, err, "创建文章失败")
	_, err = svc.Update(post.ID, PostUpdateInput{SeriesID: &series.ID}, actor)
	assertWrappedError(t, err, "保存文章失败")

	// tags 查不动：标签存在性校验失败同样要暴露出来。
	require.NoError(t, global.DB.Exec(`DROP TABLE tags`).Error)
	_, err = svc.Create(PostCreateInput{Title: "标签故障", TagIDs: []int64{tag.ID}}, actor)
	assertWrappedError(t, err, "创建文章失败")

	// 写失败必须报错，不能返回"删除成功 / 已发布"。
	published := insertPost(t, "已发布", "fault-pub", model.PostStatusPublished)
	require.NoError(t, global.DB.Exec(
		`CREATE TRIGGER posts_block_update BEFORE UPDATE ON posts BEGIN SELECT RAISE(ABORT, 'blocked'); END`).Error)
	assertWrappedError(t, svc.Delete(post.ID, actor), "删除文章失败")
	_, err = svc.Update(post.ID, PostUpdateInput{Title: ptrString("改不动")}, actor)
	assertWrappedError(t, err, "保存文章失败")
	_, err = svc.Publish(post.ID, "", actor)
	assertWrappedError(t, err, "发布文章失败")
	_, err = svc.Unpublish(published.ID, actor)
	assertWrappedError(t, err, "取消发布失败")
	require.NoError(t, global.DB.Exec(`DROP TRIGGER posts_block_update`).Error)

	// 定时任务表查不动：回草稿与取消发布都要撤任务，撤不掉就得报错。
	require.NoError(t, global.DB.Exec(`DROP TABLE jobs`).Error)
	_, err = svc.Publish(post.ID, "", actor)
	assertWrappedError(t, err, "发布文章失败")
	_, err = svc.Unpublish(published.ID, actor)
	assertWrappedError(t, err, "取消发布失败")
	_, err = svc.Update(published.ID, PostUpdateInput{Status: ptrString(model.PostStatusDraft)}, actor)
	assertWrappedError(t, err, "保存文章失败")

	// 版本表查不动：重复发布判断与版本列表都要报错。
	require.NoError(t, global.DB.Exec(`DROP TABLE post_revisions`).Error)
	_, err = svc.Publish(published.ID, "", actor)
	assertWrappedError(t, err, "发布文章失败")
	_, err = svc.Revisions(published.ID)
	assertWrappedError(t, err, "查询版本失败")

	// 跳转表查不动：已发布文章改 slug 必须写 301，写不进去就不能改。
	require.NoError(t, global.DB.Exec(`DROP TABLE redirects`).Error)
	_, err = svc.Update(published.ID, PostUpdateInput{Slug: ptrString("fault-pub-2"), CreateRedirect: true}, actor)
	assertWrappedError(t, err, "保存文章失败")

	// posts 查不动：只有 ErrNotFound 才是 1002。
	require.NoError(t, global.DB.Exec(`DROP TABLE posts`).Error)
	_, err = svc.Update(post.ID, PostUpdateInput{Title: ptrString("改")}, actor)
	assertWrappedError(t, err, "查询文章失败")
	_, err = svc.PublicBySlug("fault-pub")
	assertWrappedError(t, err, "查询文章失败")
	_, err = svc.Create(PostCreateInput{Title: "显式", Slug: "fault-slug"}, actor)
	assertWrappedError(t, err, "检查 slug 失败")
	_, err = svc.Create(PostCreateInput{Title: "Zed"}, actor)
	assertWrappedError(t, err, "检查 slug 失败")
	_, _, err = svc.List("", "", 1, 20)
	require.Error(t, err)
}

// TestPostHelpers_EdgeBranches 覆盖 service 入口走不到的纯函数分支。
//
// 直调的理由：truncateRunes 的 max<=0 是防御分支（post.go 里只传 postExcerptLimit），
// normalizeTagIDs 的非正数过滤在 service 层也被上游挡住一部分，只有直调才能稳定命中。
func TestPostHelpers_EdgeBranches(t *testing.T) {
	// slugify：折叠非 ASCII 字母数字、丢掉首尾连字符、截断 40 字符。
	assert.Equal(t, "hello-world", slugify("  Hello, World!  "))
	assert.Equal(t, "", slugify("中文标题"))
	assert.Equal(t, strings.Repeat("a", postSlugAutoLen), slugify(strings.Repeat("a", postSlugAutoLen+10)))
	assert.Equal(t, "", slugify("---"))

	assert.NoError(t, validatePostSlug("a-b1"))
	assert.Error(t, validatePostSlug(""))
	assert.Error(t, validatePostSlug("a--b"))
	assert.Error(t, validatePostSlug(strings.Repeat("a", postSlugMaxLen+1)))

	// 按字符而非字节截断，避免切坏中文。
	assert.Equal(t, "中文", truncateRunes("中文字", 2))
	assert.Equal(t, "abc", truncateRunes("abc", 0))
	assert.Equal(t, "abc", truncateRunes("abc", 5))
	assert.Len(t, []rune(autoExcerpt(strings.Repeat("字", postExcerptLimit+20))), postExcerptLimit)
	assert.Equal(t, "a b", autoExcerpt("  a \n\n b  "))
	assert.Equal(t, "手写", excerptOrAuto("  手写  ", "正文"))
	assert.Equal(t, "正文", excerptOrAuto("", "正文"))

	// clearableID：0 与非正数都表示"清空"。
	assert.Nil(t, clearableID(0))
	assert.Nil(t, clearableID(-3))
	require.NotNil(t, clearableID(9))
	assert.Equal(t, int64(9), *clearableID(9))

	// normalizeTagIDs：丢非正数、去重、保留顺序、超限报 1003。
	normalized, err := normalizeTagIDs([]int64{0, -1, 5, 3, 5})
	require.NoError(t, err)
	assert.Equal(t, []int64{5, 3}, normalized)

	empty, err := normalizeTagIDs(nil)
	require.NoError(t, err)
	assert.NotNil(t, empty)

	_, err = normalizeTagIDs(manyIDs(postTagMaxCount + 1))
	assertFieldError(t, err, errs.ErrorValidation, "tagIds")

	// isPostSlugLocked：只有已发布与已归档锁定 slug。
	assert.False(t, isPostSlugLocked(model.PostStatusDraft))
	assert.False(t, isPostSlugLocked(model.PostStatusScheduled))
	assert.True(t, isPostSlugLocked(model.PostStatusPublished))
	assert.True(t, isPostSlugLocked(model.PostStatusArchived))

	// postPublicPath 是写 redirects 用的对外路径。
	assert.Equal(t, "/posts/x", postPublicPath("x"))
}
