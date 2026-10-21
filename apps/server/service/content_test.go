// service/content.go 的单元测试（任务 D18）：标签 / 系列 / 友链 / 跳转 / 附件与站点设置。
//
// 为什么不写 t.Parallel()：`global.DB`、`global.Config` 是进程级单例，`setupEnv` 每个用例
// 换一套临时库，并行会互相踩数据（与 main_test.go 的约定一致）。
package service

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/unicornfairy864/blog-zjut-si/apps/server/global"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/model"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/model/common/dbtime"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/model/common/errs"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/model/common/request"
)

// contentStr / contentInt / contentInt64 取指针字面量；"nil = 不修改"是 content.go 的 PUT 语义。
func contentStr(value string) *string { return &value }

func contentInt(value int) *int { return &value }

func contentInt64(value int64) *int64 { return &value }

func contentBool(value bool) *bool { return &value }

// contentAppError 断言 err 是业务错误并取出 code / details。
func contentAppError(t *testing.T, err error) *errs.AppError {
	t.Helper()

	require.Error(t, err)
	appErr, ok := errs.As(err)
	require.True(t, ok, "期望业务错误，实际是 %v", err)
	return appErr
}

// contentFieldErrors 取出 1003 错误 data 里的字段错误数组（contentFieldError 的固定形状）。
func contentFieldErrors(t *testing.T, err error) []request.FieldError {
	t.Helper()

	appErr := contentAppError(t, err)
	assert.Equal(t, errs.ErrorValidation, appErr.Code)
	fields, ok := appErr.Details.([]request.FieldError)
	require.True(t, ok, "期望字段错误数组，实际是 %T", appErr.Details)
	return fields
}

// contentFieldNames 只取字段名，便于用 ElementsMatch / Contains 断言。
func contentFieldNames(fields []request.FieldError) []string {
	names := make([]string, 0, len(fields))
	for _, field := range fields {
		names = append(names, field.Field)
	}
	return names
}

// contentPutRawSetting 直写 settings.value_json（不存在则插入），用来构造脏数据。
func contentPutRawSetting(t *testing.T, key, raw string) {
	t.Helper()

	require.NoError(t, global.DB.Exec(
		`INSERT INTO settings (key, value_json, updated_at) VALUES (?, ?, ?)
		 ON CONFLICT(key) DO UPDATE SET value_json = excluded.value_json`,
		key, raw, int64(dbtime.Now()),
	).Error)
}

// contentBlockWrites 给某张表装一个"一写就报错"的触发器。
//
// 预检查与唯一索引挡住了绝大多数写失败，这些防御分支只能靠人为制造 DB 错误来覆盖；
// 每个用例一套临时库，触发器不会外溢到别的用例。
func contentBlockWrites(t *testing.T, table, event string) {
	t.Helper()

	require.NoError(t, global.DB.Exec(
		"CREATE TRIGGER "+contentTriggerName(table, event)+" BEFORE "+event+" ON "+table+
			" BEGIN SELECT RAISE(ABORT, 'boom'); END").Error)
}

// contentDropTrigger 拆掉 contentBlockWrites 装的触发器。
func contentDropTrigger(t *testing.T, table, event string) {
	t.Helper()

	require.NoError(t, global.DB.Exec("DROP TRIGGER "+contentTriggerName(table, event)).Error)
}

// contentTriggerName 生成触发器名（表名 + 事件，避免同表多事件撞名）。
func contentTriggerName(table, event string) string {
	return "content_test_block_" + table + "_" + strings.ToLower(event)
}

// contentAuditMeta 读最近一条指定动作的审计 meta_json。
func contentAuditMeta(t *testing.T, action string) string {
	t.Helper()

	var meta string
	row := global.DB.Raw(
		"SELECT meta_json FROM audit_logs WHERE action = ? ORDER BY id DESC LIMIT 1", action,
	).Row()
	require.NoError(t, row.Scan(&meta))
	return meta
}

// ---------------------------------------------------------------- 标签 ----

// TestContentService_CreateTag 正常路径：裁剪空白、按 name 生成 slug、手填 slug 优先、写审计。
func TestContentService_CreateTag(t *testing.T) {
	setupEnv(t)
	actor := testActor(t)
	svc := ContentService{}

	tag, err := svc.CreateTag(TaxonomyInput{
		Name:        contentStr("  Go 语言  "),
		Description: contentStr("  并发与工具链  "),
	}, actor)
	require.NoError(t, err)
	assert.NotZero(t, tag.ID)
	assert.Equal(t, "Go 语言", tag.Name)
	// slugify 只保留 ASCII 字母数字：中文部分被折叠成连字符后裁掉，剩下 "go"。
	assert.Equal(t, "go", tag.Slug)
	assert.Equal(t, "并发与工具链", tag.Description)
	assert.NotZero(t, int64(tag.CreatedAt))

	assert.Equal(t, int64(1), countRows(t, "tags", "id = ?", tag.ID))
	assert.Equal(t, int64(1), countRows(t, "audit_logs",
		"action = ? AND target_id = ?", AuditActionTagCreate, strconv.FormatInt(tag.ID, 10)))

	// 手填 slug 优先于 name 生成。
	second, err := svc.CreateTag(TaxonomyInput{Name: contentStr("Rust"), Slug: contentStr(" rust-lang ")}, actor)
	require.NoError(t, err)
	assert.Equal(t, "rust-lang", second.Slug)
}

// TestContentService_CreateTagRejectsDuplicates 重名 → 1004，重 slug → 2001。
func TestContentService_CreateTagRejectsDuplicates(t *testing.T) {
	setupEnv(t)
	actor := testActor(t)
	svc := ContentService{}

	_, err := svc.CreateTag(TaxonomyInput{Name: contentStr("Go")}, actor)
	require.NoError(t, err)

	_, err = svc.CreateTag(TaxonomyInput{Name: contentStr("Go"), Slug: contentStr("go-2")}, actor)
	appErr := contentAppError(t, err)
	assert.Equal(t, errs.ErrorConflict, appErr.Code)
	assert.Equal(t, "名称已存在", appErr.Message)

	_, err = svc.CreateTag(TaxonomyInput{Name: contentStr("Golang"), Slug: contentStr("go")}, actor)
	appErr = contentAppError(t, err)
	assert.Equal(t, errs.ErrorSlugConflict, appErr.Code)
	assert.Contains(t, appErr.Message, "slug 已存在")

	// 失败的两次都没落库。
	assert.Equal(t, int64(1), countRows(t, "tags", ""))
}

// TestContentService_CreateTagValidatesFields 字段级校验统一回 1003 且 data 带字段数组。
func TestContentService_CreateTagValidatesFields(t *testing.T) {
	setupEnv(t)
	actor := testActor(t)
	svc := ContentService{}

	// name 缺失。
	_, err := svc.CreateTag(TaxonomyInput{}, actor)
	fields := contentFieldErrors(t, err)
	require.Len(t, fields, 1)
	assert.Equal(t, "name", fields[0].Field)

	// name 超长（按字符数，不是字节数）。
	_, err = svc.CreateTag(TaxonomyInput{Name: contentStr(strings.Repeat("字", taxonomyNameMaxLen+1))}, actor)
	assert.Equal(t, "name", contentFieldErrors(t, err)[0].Field)

	// description 超长。
	_, err = svc.CreateTag(TaxonomyInput{
		Name:        contentStr("ok"),
		Description: contentStr(strings.Repeat("字", taxonomyDescriptionMaxLen+1)),
	}, actor)
	assert.Equal(t, "description", contentFieldErrors(t, err)[0].Field)

	// 中文名 + 空 slug：slugify 得到空串，必须手填（否则中文分类会全撞同一个 slug）。
	_, err = svc.CreateTag(TaxonomyInput{Name: contentStr("中文标签")}, actor)
	appErr := contentAppError(t, err)
	assert.Equal(t, errs.ErrorValidation, appErr.Code)
	assert.Nil(t, appErr.Details) // 这条是单条消息，不是字段数组
	assert.Contains(t, appErr.Message, "slug 不能为空")

	// 手填非法 slug（大写 / 下划线都不在 §4 字符集里）。
	_, err = svc.CreateTag(TaxonomyInput{Name: contentStr("Bad Slug"), Slug: contentStr("Bad_Slug")}, actor)
	assert.Equal(t, errs.ErrorValidation, contentAppError(t, err).Code)

	assert.Equal(t, int64(0), countRows(t, "tags", ""))
}

// TestContentService_UpdateTag 局部更新：nil 不改、改 name 不动 slug、空 slug 按当前 name 重算。
func TestContentService_UpdateTag(t *testing.T) {
	setupEnv(t)
	actor := testActor(t)
	svc := ContentService{}

	tag, err := svc.CreateTag(TaxonomyInput{Name: contentStr("Go"), Description: contentStr("旧描述")}, actor)
	require.NoError(t, err)

	// 全 nil：原样返回，也不该留下审计（没改就没写）。
	same, err := svc.UpdateTag(tag.ID, TaxonomyInput{}, actor)
	require.NoError(t, err)
	assert.Equal(t, tag.Name, same.Name)
	assert.Equal(t, int64(0), countRows(t, "audit_logs", "action = ?", AuditActionTagUpdate))

	// 改描述。
	updated, err := svc.UpdateTag(tag.ID, TaxonomyInput{Description: contentStr(" 新描述 ")}, actor)
	require.NoError(t, err)
	assert.Equal(t, "新描述", updated.Description)
	assert.Equal(t, "go", updated.Slug)
	assert.Equal(t, int64(1), countRows(t, "audit_logs", "action = ?", AuditActionTagUpdate))

	// 改 name 不会自动重算 slug（slug 的 nil 语义是"不修改"）。
	updated, err = svc.UpdateTag(tag.ID, TaxonomyInput{Name: contentStr("Golang")}, actor)
	require.NoError(t, err)
	assert.Equal(t, "Golang", updated.Name)
	assert.Equal(t, "go", updated.Slug)

	// 手填新 slug。
	updated, err = svc.UpdateTag(tag.ID, TaxonomyInput{Slug: contentStr("golang-lang")}, actor)
	require.NoError(t, err)
	assert.Equal(t, "golang-lang", updated.Slug)

	// 空 slug：按当前 name 重新生成。
	updated, err = svc.UpdateTag(tag.ID, TaxonomyInput{Slug: contentStr("")}, actor)
	require.NoError(t, err)
	assert.Equal(t, "golang", updated.Slug)

	// 与现值相同的 name/description 不算变更。
	before := countRows(t, "audit_logs", "action = ?", AuditActionTagUpdate)
	updated, err = svc.UpdateTag(tag.ID, TaxonomyInput{Name: contentStr("Golang"), Description: contentStr("新描述")}, actor)
	require.NoError(t, err)
	assert.Equal(t, "golang", updated.Slug)
	assert.Equal(t, before, countRows(t, "audit_logs", "action = ?", AuditActionTagUpdate))
}

// TestContentService_UpdateTagErrors 更新路径的错误码与校验。
func TestContentService_UpdateTagErrors(t *testing.T) {
	setupEnv(t)
	actor := testActor(t)
	svc := ContentService{}

	other, err := svc.CreateTag(TaxonomyInput{Name: contentStr("Other"), Slug: contentStr("other")}, actor)
	require.NoError(t, err)
	tag, err := svc.CreateTag(TaxonomyInput{Name: contentStr("Target"), Slug: contentStr("target")}, actor)
	require.NoError(t, err)

	// 不存在 → 1002（HTTP 200 的业务错误）。
	_, err = svc.UpdateTag(999999, TaxonomyInput{Name: contentStr("x")}, actor)
	assert.Equal(t, errs.ErrorNotFound, contentAppError(t, err).Code)

	// 撞名 → 1004；撞 slug → 2001。
	_, err = svc.UpdateTag(tag.ID, TaxonomyInput{Name: contentStr(other.Name)}, actor)
	assert.Equal(t, errs.ErrorConflict, contentAppError(t, err).Code)
	_, err = svc.UpdateTag(tag.ID, TaxonomyInput{Slug: contentStr(other.Slug)}, actor)
	assert.Equal(t, errs.ErrorSlugConflict, contentAppError(t, err).Code)

	// 名称非法 / 描述非法。
	_, err = svc.UpdateTag(tag.ID, TaxonomyInput{Name: contentStr("")}, actor)
	assert.Equal(t, "name", contentFieldErrors(t, err)[0].Field)
	_, err = svc.UpdateTag(tag.ID, TaxonomyInput{Description: contentStr(strings.Repeat("x", taxonomyDescriptionMaxLen+1))}, actor)
	assert.Equal(t, "description", contentFieldErrors(t, err)[0].Field)

	// 先改成中文名，再用空 slug 触发"中文名请手填"。
	_, err = svc.UpdateTag(tag.ID, TaxonomyInput{Name: contentStr("中文名"), Slug: contentStr("zh-name")}, actor)
	require.NoError(t, err)
	_, err = svc.UpdateTag(tag.ID, TaxonomyInput{Slug: contentStr("")}, actor)
	appErr := contentAppError(t, err)
	assert.Equal(t, errs.ErrorValidation, appErr.Code)
	assert.Contains(t, appErr.Message, "slug 不能为空")
}

// TestContentService_DeleteTagKeepsPosts 删标签只断关系，不删文章（§5）。
func TestContentService_DeleteTagKeepsPosts(t *testing.T) {
	setupEnv(t)
	actor := testActor(t)
	svc := ContentService{}

	post := insertPost(t, "文章", "tag-delete-post", model.PostStatusPublished)
	tag, err := svc.CreateTag(TaxonomyInput{Name: contentStr("Go")}, actor)
	require.NoError(t, err)
	require.NoError(t, global.DB.Exec("INSERT INTO post_tags (post_id, tag_id) VALUES (?, ?)", post.ID, tag.ID).Error)

	require.NoError(t, svc.DeleteTag(tag.ID, actor))

	assert.Equal(t, int64(0), countRows(t, "tags", "id = ?", tag.ID))
	assert.Equal(t, int64(0), countRows(t, "post_tags", "tag_id = ?", tag.ID))
	assert.Equal(t, int64(1), countRows(t, "posts", "id = ?", post.ID))
	assert.Equal(t, int64(1), countRows(t, "audit_logs", "action = ?", AuditActionTagDelete))

	err = svc.DeleteTag(tag.ID, actor)
	assert.Equal(t, errs.ErrorNotFound, contentAppError(t, err).Code)
}

// TestContentService_TagsCountsUndeletedPosts 列表的 count 口径：未删除文章数（含草稿）。
func TestContentService_TagsCountsUndeletedPosts(t *testing.T) {
	setupEnv(t)
	actor := testActor(t)
	svc := ContentService{}

	published := insertPost(t, "已发布", "tags-count-published", model.PostStatusPublished)
	draft := insertPost(t, "草稿", "tags-count-draft", model.PostStatusDraft)
	removed := insertPost(t, "已删除", "tags-count-removed", model.PostStatusPublished)
	require.NoError(t, global.DB.Exec(
		"UPDATE posts SET deleted_at = ? WHERE id = ?", int64(dbtime.Now()), removed.ID).Error)

	goTag, err := svc.CreateTag(TaxonomyInput{Name: contentStr("Go")}, actor)
	require.NoError(t, err)
	emptyTag, err := svc.CreateTag(TaxonomyInput{Name: contentStr("Empty")}, actor)
	require.NoError(t, err)

	// 已发布、草稿、已软删的文章都还挂着这个标签。
	for _, postID := range []int64{published.ID, draft.ID, removed.ID} {
		require.NoError(t, global.DB.Exec("INSERT INTO post_tags (post_id, tag_id) VALUES (?, ?)", postID, goTag.ID).Error)
	}

	items, err := svc.Tags()
	require.NoError(t, err)
	require.Len(t, items, 2)

	byID := make(map[int64]model.AdminTagItem, len(items))
	for _, item := range items {
		byID[item.ID] = item
	}
	assert.Equal(t, int64(2), byID[goTag.ID].Count) // 草稿计入，软删不计
	assert.Equal(t, int64(0), byID[emptyTag.ID].Count)
	assert.Equal(t, "Empty", items[0].Name) // 按名称升序
	assert.Equal(t, "Go", items[1].Name)
}

// ---------------------------------------------------------------- 系列 ----

// TestContentService_SeriesErrors 系列的创建 / 更新 / 删除错误路径。
func TestContentService_SeriesErrors(t *testing.T) {
	setupEnv(t)
	actor := testActor(t)
	svc := ContentService{}

	first, err := svc.CreateSeries(TaxonomyInput{Name: contentStr("Go")}, actor)
	require.NoError(t, err)
	assert.Equal(t, "go", first.Slug)

	// 重名 → 1004（series.name 没有唯一索引，靠 ensureNameFree 前置检查兜住）。
	_, err = svc.CreateSeries(TaxonomyInput{Name: contentStr("Go"), Slug: contentStr("go-2")}, actor)
	assert.Equal(t, errs.ErrorConflict, contentAppError(t, err).Code)

	// 重 slug → 2001。
	_, err = svc.CreateSeries(TaxonomyInput{Name: contentStr("Golang"), Slug: contentStr("go")}, actor)
	assert.Equal(t, errs.ErrorSlugConflict, contentAppError(t, err).Code)

	// 中文名 + 空 slug → 1003。
	_, err = svc.CreateSeries(TaxonomyInput{Name: contentStr("中文系列")}, actor)
	appErr := contentAppError(t, err)
	assert.Equal(t, errs.ErrorValidation, appErr.Code)
	assert.Contains(t, appErr.Message, "slug 不能为空")

	// name 缺失 → 1003 字段数组。
	_, err = svc.CreateSeries(TaxonomyInput{}, actor)
	assert.Equal(t, "name", contentFieldErrors(t, err)[0].Field)

	// 不存在 → 1002。
	_, err = svc.UpdateSeries(999999, TaxonomyInput{Name: contentStr("x")}, actor)
	assert.Equal(t, errs.ErrorNotFound, contentAppError(t, err).Code)
	err = svc.DeleteSeries(999999, actor)
	assert.Equal(t, errs.ErrorNotFound, contentAppError(t, err).Code)

	// 更新 name / description / slug。
	updated, err := svc.UpdateSeries(first.ID, TaxonomyInput{
		Name:        contentStr("Golang"),
		Description: contentStr(" 新描述 "),
		Slug:        contentStr("golang"),
	}, actor)
	require.NoError(t, err)
	assert.Equal(t, "Golang", updated.Name)
	assert.Equal(t, "新描述", updated.Description)
	assert.Equal(t, "golang", updated.Slug)

	second, err := svc.CreateSeries(TaxonomyInput{Name: contentStr("Rust")}, actor)
	require.NoError(t, err)

	// 撞名 / 撞 slug / 名称非法 / 描述超长。
	_, err = svc.UpdateSeries(second.ID, TaxonomyInput{Name: contentStr("Golang")}, actor)
	assert.Equal(t, errs.ErrorConflict, contentAppError(t, err).Code)
	_, err = svc.UpdateSeries(second.ID, TaxonomyInput{Slug: contentStr("golang")}, actor)
	assert.Equal(t, errs.ErrorSlugConflict, contentAppError(t, err).Code)
	_, err = svc.UpdateSeries(second.ID, TaxonomyInput{Name: contentStr("")}, actor)
	assert.Equal(t, "name", contentFieldErrors(t, err)[0].Field)
	_, err = svc.UpdateSeries(second.ID, TaxonomyInput{Description: contentStr(strings.Repeat("x", taxonomyDescriptionMaxLen+1))}, actor)
	assert.Equal(t, "description", contentFieldErrors(t, err)[0].Field)

	// 空 slug 按当前 name 重新生成。
	regenerated, err := svc.UpdateSeries(second.ID, TaxonomyInput{Slug: contentStr("")}, actor)
	require.NoError(t, err)
	assert.Equal(t, "rust", regenerated.Slug)

	// 全 nil：不改也不写审计。
	before := countRows(t, "audit_logs", "action = ?", AuditActionSeriesUpdate)
	same, err := svc.UpdateSeries(second.ID, TaxonomyInput{}, actor)
	require.NoError(t, err)
	assert.Equal(t, "rust", same.Slug)
	assert.Equal(t, before, countRows(t, "audit_logs", "action = ?", AuditActionSeriesUpdate))

	assert.Equal(t, int64(2), countRows(t, "audit_logs", "action = ?", AuditActionSeriesCreate))
}

// TestContentService_SeriesListCountsAndDeleteDetaches 系列计数口径与"删系列不删文章"。
func TestContentService_SeriesListCountsAndDeleteDetaches(t *testing.T) {
	setupEnv(t)
	actor := testActor(t)
	svc := ContentService{}

	series, err := svc.CreateSeries(TaxonomyInput{Name: contentStr("Go")}, actor)
	require.NoError(t, err)
	empty, err := svc.CreateSeries(TaxonomyInput{Name: contentStr("Rust")}, actor)
	require.NoError(t, err)

	live := insertPost(t, "已发布", "series-live", model.PostStatusPublished)
	draft := insertPost(t, "草稿", "series-draft", model.PostStatusDraft)
	gone := insertPost(t, "已删除", "series-gone", model.PostStatusPublished)
	for _, post := range []*model.Post{live, draft, gone} {
		require.NoError(t, global.DB.Exec(
			"UPDATE posts SET series_id = ?, series_order = 7 WHERE id = ?", series.ID, post.ID).Error)
	}
	require.NoError(t, global.DB.Exec(
		"UPDATE posts SET deleted_at = ? WHERE id = ?", int64(dbtime.Now()), gone.ID).Error)

	items, err := svc.Series()
	require.NoError(t, err)
	require.Len(t, items, 2)

	byID := make(map[int64]model.AdminSeriesItem, len(items))
	for _, item := range items {
		byID[item.ID] = item
	}
	assert.Equal(t, int64(2), byID[series.ID].Count) // 草稿计入，软删不计
	assert.Equal(t, int64(0), byID[empty.ID].Count)
	assert.Equal(t, "Go", items[0].Name) // 按名称升序

	// 删系列：文章的 series_id 置空、series_order 归零，文章本身不动。
	require.NoError(t, svc.DeleteSeries(series.ID, actor))
	assert.Equal(t, int64(0), countRows(t, "series", "id = ?", series.ID))
	assert.Equal(t, int64(1), countRows(t, "posts", "id = ?", live.ID))
	assert.Equal(t, int64(1), countRows(t, "audit_logs", "action = ?", AuditActionSeriesDelete))

	var detached model.Post
	require.NoError(t, global.DB.Where("id = ?", live.ID).Take(&detached).Error)
	assert.Nil(t, detached.SeriesID)
	assert.Zero(t, detached.SeriesOrder)
}

// ---------------------------------------------------------------- 友链 ----

// TestContentService_LinkCRUD 友链的创建默认值、排序与更新 / 删除。
func TestContentService_LinkCRUD(t *testing.T) {
	setupEnv(t)
	actor := testActor(t)
	svc := ContentService{}

	link, err := svc.CreateLink(LinkInput{
		Name:        contentStr(" 友链 "),
		URL:         contentStr(" https://example.com "),
		Avatar:      contentStr("https://example.com/a.png"),
		Description: contentStr(" 描述 "),
		Sort:        contentInt(5),
	}, actor)
	require.NoError(t, err)
	assert.NotZero(t, link.ID)
	assert.Equal(t, "友链", link.Name)
	assert.Equal(t, "https://example.com", link.URL)
	assert.Equal(t, "描述", link.Description)
	assert.Equal(t, model.LinkStatusActive, link.Status) // 不传 status 默认 active
	assert.Equal(t, 5, link.Sort)
	assert.Equal(t, int64(1), countRows(t, "audit_logs", "action = ?", AuditActionLinkCreate))

	// status 显式空串也归一成 active；hidden 原样保存。
	blank, err := svc.CreateLink(LinkInput{
		Name: contentStr("空状态"), URL: contentStr("http://a.example"), Status: contentStr(""),
	}, actor)
	require.NoError(t, err)
	assert.Equal(t, model.LinkStatusActive, blank.Status)

	hidden, err := svc.CreateLink(LinkInput{
		Name: contentStr("隐藏"), URL: contentStr("http://b.example"),
		Status: contentStr(" hidden "), Sort: contentInt(5),
	}, actor)
	require.NoError(t, err)
	assert.Equal(t, model.LinkStatusHidden, hidden.Status)

	// 管理端列表含 hidden，按 sort、id 升序。
	links, err := svc.Links()
	require.NoError(t, err)
	require.Len(t, links, 3)
	for i := 1; i < len(links); i++ {
		assert.LessOrEqual(t, links[i-1].Sort, links[i].Sort)
	}
	assert.Equal(t, blank.ID, links[0].ID)
	assert.Equal(t, link.ID, links[1].ID)
	assert.Equal(t, hidden.ID, links[2].ID)

	// 更新 sort / status。
	updated, err := svc.UpdateLink(link.ID, LinkInput{
		Sort: contentInt(9), Status: contentStr(model.LinkStatusHidden),
	}, actor)
	require.NoError(t, err)
	assert.Equal(t, 9, updated.Sort)
	assert.Equal(t, model.LinkStatusHidden, updated.Status)
	assert.Equal(t, int64(1), countRows(t, "audit_logs", "action = ?", AuditActionLinkUpdate))

	// 全 nil：不改也不写审计。
	same, err := svc.UpdateLink(link.ID, LinkInput{}, actor)
	require.NoError(t, err)
	assert.Equal(t, 9, same.Sort)
	assert.Equal(t, int64(1), countRows(t, "audit_logs", "action = ?", AuditActionLinkUpdate))

	_, err = svc.UpdateLink(999999, LinkInput{Name: contentStr("x")}, actor)
	assert.Equal(t, errs.ErrorNotFound, contentAppError(t, err).Code)

	require.NoError(t, svc.DeleteLink(link.ID, actor))
	assert.Equal(t, int64(0), countRows(t, "links", "id = ?", link.ID))
	assert.Equal(t, int64(1), countRows(t, "audit_logs", "action = ?", AuditActionLinkDelete))
	err = svc.DeleteLink(link.ID, actor)
	assert.Equal(t, errs.ErrorNotFound, contentAppError(t, err).Code)
}

// TestContentService_LinkValidation 友链字段校验：name / url / avatar / description / sort / status。
func TestContentService_LinkValidation(t *testing.T) {
	setupEnv(t)
	actor := testActor(t)
	svc := ContentService{}

	// name 与 url 都缺。
	_, err := svc.CreateLink(LinkInput{}, actor)
	assert.ElementsMatch(t, []string{"name", "url"}, contentFieldNames(contentFieldErrors(t, err)))

	// name 超长。
	_, err = svc.CreateLink(LinkInput{
		Name: contentStr(strings.Repeat("字", linkNameMaxLen+1)), URL: contentStr("https://a.example"),
	}, actor)
	assert.Equal(t, "name", contentFieldErrors(t, err)[0].Field)

	// url 不是 http(s) / 超长。
	_, err = svc.CreateLink(LinkInput{Name: contentStr("x"), URL: contentStr("ftp://a.example")}, actor)
	assert.Equal(t, "url", contentFieldErrors(t, err)[0].Field)
	_, err = svc.CreateLink(LinkInput{
		Name: contentStr("x"), URL: contentStr("https://" + strings.Repeat("a", linkURLMaxLen)),
	}, actor)
	assert.Equal(t, "url", contentFieldErrors(t, err)[0].Field)

	// avatar 填了就必须是 http(s)。
	_, err = svc.CreateLink(LinkInput{
		Name: contentStr("x"), URL: contentStr("https://a.example"), Avatar: contentStr("/local.png"),
	}, actor)
	assert.Equal(t, "avatar", contentFieldErrors(t, err)[0].Field)
	_, err = svc.CreateLink(LinkInput{
		Name: contentStr("x"), URL: contentStr("https://a.example"),
		Avatar: contentStr("https://" + strings.Repeat("a", linkAvatarMaxLen)),
	}, actor)
	assert.Equal(t, "avatar", contentFieldErrors(t, err)[0].Field)

	// description 超长。
	_, err = svc.CreateLink(LinkInput{
		Name: contentStr("x"), URL: contentStr("https://a.example"),
		Description: contentStr(strings.Repeat("字", linkDescriptionMaxLen+1)),
	}, actor)
	assert.Equal(t, "description", contentFieldErrors(t, err)[0].Field)

	// sort 越界（下界与上界）。
	_, err = svc.CreateLink(LinkInput{Name: contentStr("x"), URL: contentStr("https://a.example"), Sort: contentInt(-1)}, actor)
	assert.Equal(t, "sort", contentFieldErrors(t, err)[0].Field)
	_, err = svc.CreateLink(LinkInput{Name: contentStr("x"), URL: contentStr("https://a.example"), Sort: contentInt(linkSortMax + 1)}, actor)
	assert.Equal(t, "sort", contentFieldErrors(t, err)[0].Field)

	// status 枚举只接受 active / hidden。
	_, err = svc.CreateLink(LinkInput{
		Name: contentStr("x"), URL: contentStr("https://a.example"), Status: contentStr("draft"),
	}, actor)
	assert.Equal(t, "status", contentFieldErrors(t, err)[0].Field)

	// 边界值 0 与 99999 合法。
	boundary, err := svc.CreateLink(LinkInput{
		Name: contentStr("边界"), URL: contentStr("https://a.example"), Sort: contentInt(linkSortMax),
	}, actor)
	require.NoError(t, err)
	assert.Equal(t, linkSortMax, boundary.Sort)

	// UpdateLink 会把入参与现值合成后整体校验：非法 status 一样回 1003。
	_, err = svc.UpdateLink(boundary.ID, LinkInput{Status: contentStr("paused")}, actor)
	assert.Equal(t, "status", contentFieldErrors(t, err)[0].Field)

	// 只有边界值那条落库了。
	assert.Equal(t, int64(1), countRows(t, "links", ""))
}

// ---------------------------------------------------------------- 跳转 ----

// TestContentService_RedirectCRUD 跳转的默认状态码、唯一 fromPath 与更新 / 删除。
func TestContentService_RedirectCRUD(t *testing.T) {
	setupEnv(t)
	actor := testActor(t)
	svc := ContentService{}

	// status 留空 → 301。
	item, err := svc.CreateRedirect(RedirectInput{FromPath: contentStr(" /old "), ToPath: contentStr(" /new ")}, actor)
	require.NoError(t, err)
	assert.Equal(t, "/old", item.FromPath)
	assert.Equal(t, "/new", item.ToPath)
	assert.Equal(t, redirectStatusMoved, item.Status)
	assert.Equal(t, int64(1), countRows(t, "audit_logs", "action = ?", AuditActionRedirectCreate))

	// 显式 302 + 外链目标。
	second, err := svc.CreateRedirect(RedirectInput{
		FromPath: contentStr("/old2"), ToPath: contentStr("https://example.com/x"), Status: contentInt(302),
	}, actor)
	require.NoError(t, err)
	assert.Equal(t, redirectStatusFound, second.Status)

	// fromPath 重复 → 1004。
	_, err = svc.CreateRedirect(RedirectInput{FromPath: contentStr("/old"), ToPath: contentStr("/other")}, actor)
	appErr := contentAppError(t, err)
	assert.Equal(t, errs.ErrorConflict, appErr.Code)
	assert.Contains(t, appErr.Message, "fromPath 已存在")

	// 列表按 id 倒序。
	items, err := svc.Redirects()
	require.NoError(t, err)
	require.Len(t, items, 2)
	assert.Equal(t, second.ID, items[0].ID)

	// 只改 toPath：fromPath 与自身相同，不该被当成冲突。
	updated, err := svc.UpdateRedirect(item.ID, RedirectInput{ToPath: contentStr("/newer"), Status: contentInt(302)}, actor)
	require.NoError(t, err)
	assert.Equal(t, "/newer", updated.ToPath)
	assert.Equal(t, "/old", updated.FromPath)
	assert.Equal(t, redirectStatusFound, updated.Status)
	assert.Equal(t, int64(1), countRows(t, "audit_logs", "action = ?", AuditActionRedirectUpdate))

	// 改 fromPath 撞别人的 → 1004。
	_, err = svc.UpdateRedirect(item.ID, RedirectInput{FromPath: contentStr("/old2")}, actor)
	assert.Equal(t, errs.ErrorConflict, contentAppError(t, err).Code)

	// 全 nil：不改也不写审计。
	same, err := svc.UpdateRedirect(item.ID, RedirectInput{}, actor)
	require.NoError(t, err)
	assert.Equal(t, "/newer", same.ToPath)
	assert.Equal(t, int64(1), countRows(t, "audit_logs", "action = ?", AuditActionRedirectUpdate))

	require.NoError(t, svc.DeleteRedirect(item.ID, actor))
	assert.Equal(t, int64(0), countRows(t, "redirects", "id = ?", item.ID))
	assert.Equal(t, int64(1), countRows(t, "audit_logs", "action = ?", AuditActionRedirectDelete))
	err = svc.DeleteRedirect(item.ID, actor)
	assert.Equal(t, errs.ErrorNotFound, contentAppError(t, err).Code)
}

// TestContentService_RedirectValidation 跳转字段校验：fromPath 前缀、toPath 前缀、status 枚举。
func TestContentService_RedirectValidation(t *testing.T) {
	setupEnv(t)
	actor := testActor(t)
	svc := ContentService{}

	// fromPath 必须以 / 开头。
	_, err := svc.CreateRedirect(RedirectInput{FromPath: contentStr("old"), ToPath: contentStr("/new")}, actor)
	assert.Equal(t, "fromPath", contentFieldErrors(t, err)[0].Field)

	// toPath 必须以 / 或 http(s):// 开头。
	_, err = svc.CreateRedirect(RedirectInput{FromPath: contentStr("/a"), ToPath: contentStr("new")}, actor)
	assert.Equal(t, "toPath", contentFieldErrors(t, err)[0].Field)

	// status 只允许 301 / 302。
	_, err = svc.CreateRedirect(RedirectInput{
		FromPath: contentStr("/b"), ToPath: contentStr("/c"), Status: contentInt(303),
	}, actor)
	assert.Equal(t, "status", contentFieldErrors(t, err)[0].Field)

	// 两个都缺。
	_, err = svc.CreateRedirect(RedirectInput{}, actor)
	assert.ElementsMatch(t, []string{"fromPath", "toPath"}, contentFieldNames(contentFieldErrors(t, err)))

	// 超长（按字符数）。
	_, err = svc.CreateRedirect(RedirectInput{
		FromPath: contentStr("/" + strings.Repeat("x", redirectFromPathMaxLen)),
		ToPath:   contentStr("/" + strings.Repeat("y", redirectToPathMaxLen)),
	}, actor)
	assert.ElementsMatch(t, []string{"fromPath", "toPath"}, contentFieldNames(contentFieldErrors(t, err)))

	assert.Equal(t, int64(0), countRows(t, "redirects", ""))

	// http:// 目标也合法。
	item, err := svc.CreateRedirect(RedirectInput{FromPath: contentStr("/ok"), ToPath: contentStr("http://example.com/ok")}, actor)
	require.NoError(t, err)

	// 更新：非法 status / 不存在。
	_, err = svc.UpdateRedirect(item.ID, RedirectInput{Status: contentInt(307)}, actor)
	assert.Equal(t, "status", contentFieldErrors(t, err)[0].Field)
	_, err = svc.UpdateRedirect(999999, RedirectInput{ToPath: contentStr("/x")}, actor)
	assert.Equal(t, errs.ErrorNotFound, contentAppError(t, err).Code)
}

// ---------------------------------------------------------------- 附件 ----

// TestContentService_AttachmentCreateAndList 外链附件的创建、分页与按文章过滤。
func TestContentService_AttachmentCreateAndList(t *testing.T) {
	setupEnv(t)
	actor := testActor(t)
	svc := ContentService{}

	post := insertPost(t, "带附件", "attachment-post", model.PostStatusPublished)
	other := insertPost(t, "别的文章", "attachment-other", model.PostStatusDraft)

	item, err := svc.CreateAttachment(AttachmentInput{
		PostID:   contentInt64(post.ID),
		Title:    contentStr(" 手册 "),
		ShareURL: contentStr(" https://example.com/manual.pdf "),
		FileName: contentStr("manual.pdf"),
		Bytes:    contentInt64(1024),
		SHA256:   contentStr("abc123"),
		Version:  contentStr("v1"),
		Sort:     contentInt(3),
	}, actor)
	require.NoError(t, err)
	assert.NotZero(t, item.ID)
	assert.Equal(t, "手册", item.Title)
	assert.Equal(t, "https://example.com/manual.pdf", item.ShareURL)
	assert.Empty(t, item.StorageKey) // 外链附件没有 storage_key
	assert.Equal(t, int64(1), countRows(t, "audit_logs", "action = ?", AuditActionAttachmentCreate))

	// 本地上传的附件（走 /attachments/upload）有 storage_key、没有 share_url；这里直接落库模拟。
	local := &model.PostAttachment{
		PostID: other.ID, Title: "本地包", FileName: "pkg.zip",
		StorageKey: "ab/" + strings.Repeat("a", 64), Bytes: 10, Sort: 1, CreatedAt: dbtime.Now(),
	}
	require.NoError(t, global.DB.Create(local).Error)

	all, total, err := svc.Attachments(0, 1, 20) // postID=0 表示不过滤
	require.NoError(t, err)
	assert.Equal(t, int64(2), total)
	assert.Len(t, all, 2)

	// 按文章过滤 + 分页。
	page, total, err := svc.Attachments(post.ID, 1, 1)
	require.NoError(t, err)
	assert.Equal(t, int64(1), total)
	require.Len(t, page, 1)
	assert.Equal(t, item.ID, page[0].ID)

	page, total, err = svc.Attachments(post.ID, 2, 1)
	require.NoError(t, err)
	assert.Equal(t, int64(1), total)
	assert.Empty(t, page)

	// AttachmentByID：存在与不存在。
	got, err := svc.AttachmentByID(item.ID)
	require.NoError(t, err)
	assert.Equal(t, item.Title, got.Title)
	_, err = svc.AttachmentByID(999999)
	assert.Equal(t, errs.ErrorNotFound, contentAppError(t, err).Code)
}

// TestContentService_AttachmentValidation 附件字段校验与外链附件必须填 shareUrl 的约定。
func TestContentService_AttachmentValidation(t *testing.T) {
	setupEnv(t)
	actor := testActor(t)
	svc := ContentService{}

	post := insertPost(t, "文章", "attachment-validate", model.PostStatusPublished)
	base := func() AttachmentInput {
		return AttachmentInput{
			PostID: contentInt64(post.ID), Title: contentStr("标题"), ShareURL: contentStr("https://a.example/f"),
		}
	}

	// postId 缺失 / 非正数。
	_, err := svc.CreateAttachment(AttachmentInput{Title: contentStr("标题"), ShareURL: contentStr("https://a.example/f")}, actor)
	assert.Equal(t, "postId", contentFieldErrors(t, err)[0].Field)

	// shareUrl 缺失：外链附件必须填，本地附件请走 /attachments/upload。
	in := base()
	in.ShareURL = nil
	_, err = svc.CreateAttachment(in, actor)
	fields := contentFieldErrors(t, err)
	require.Len(t, fields, 1)
	assert.Equal(t, "shareUrl", fields[0].Field)
	assert.Contains(t, fields[0].Message, "/attachments/upload")

	// shareUrl 不是 http(s) / 超长。
	in = base()
	in.ShareURL = contentStr("/local/path")
	_, err = svc.CreateAttachment(in, actor)
	assert.Equal(t, "shareUrl", contentFieldErrors(t, err)[0].Field)

	in = base()
	in.ShareURL = contentStr("https://" + strings.Repeat("a", attachmentShareURLMaxLen))
	_, err = svc.CreateAttachment(in, actor)
	assert.Equal(t, "shareUrl", contentFieldErrors(t, err)[0].Field)

	// title 缺失 / 超长。
	in = base()
	in.Title = contentStr("")
	_, err = svc.CreateAttachment(in, actor)
	assert.Equal(t, "title", contentFieldErrors(t, err)[0].Field)

	in = base()
	in.Title = contentStr(strings.Repeat("字", attachmentTitleMaxLen+1))
	_, err = svc.CreateAttachment(in, actor)
	assert.Equal(t, "title", contentFieldErrors(t, err)[0].Field)

	// fileName 超长、bytes 为负、version 超长、sort 越界。
	in = base()
	in.FileName = contentStr(strings.Repeat("f", attachmentFileNameMaxLen+1))
	_, err = svc.CreateAttachment(in, actor)
	assert.Equal(t, "fileName", contentFieldErrors(t, err)[0].Field)

	in = base()
	in.Bytes = contentInt64(-1)
	_, err = svc.CreateAttachment(in, actor)
	assert.Equal(t, "bytes", contentFieldErrors(t, err)[0].Field)

	in = base()
	in.Version = contentStr(strings.Repeat("v", attachmentVersionMaxLen+1))
	_, err = svc.CreateAttachment(in, actor)
	assert.Equal(t, "version", contentFieldErrors(t, err)[0].Field)

	in = base()
	in.Sort = contentInt(attachmentSortMax + 1)
	_, err = svc.CreateAttachment(in, actor)
	assert.Equal(t, "sort", contentFieldErrors(t, err)[0].Field)

	// 文章不存在 → 1002。
	in = base()
	in.PostID = contentInt64(999999)
	_, err = svc.CreateAttachment(in, actor)
	appErr := contentAppError(t, err)
	assert.Equal(t, errs.ErrorNotFound, appErr.Code)
	assert.Equal(t, "文章不存在", appErr.Message)

	// 软删的文章同样不能挂附件。
	gone := insertPost(t, "已删除", "attachment-gone", model.PostStatusPublished)
	require.NoError(t, global.DB.Exec(
		"UPDATE posts SET deleted_at = ? WHERE id = ?", int64(dbtime.Now()), gone.ID).Error)
	in = base()
	in.PostID = contentInt64(gone.ID)
	_, err = svc.CreateAttachment(in, actor)
	assert.Equal(t, errs.ErrorNotFound, contentAppError(t, err).Code)

	assert.Equal(t, int64(0), countRows(t, "post_attachments", ""))
}

// TestContentService_UpdateAttachment 局部更新、换挂文章与"外链被清空"的现状。
func TestContentService_UpdateAttachment(t *testing.T) {
	setupEnv(t)
	actor := testActor(t)
	svc := ContentService{}

	post := insertPost(t, "文章", "attachment-update", model.PostStatusPublished)
	other := insertPost(t, "新文章", "attachment-update-2", model.PostStatusDraft)

	item, err := svc.CreateAttachment(AttachmentInput{
		PostID: contentInt64(post.ID), Title: contentStr("旧"), ShareURL: contentStr("https://a.example/f"), Sort: contentInt(1),
	}, actor)
	require.NoError(t, err)

	updated, err := svc.UpdateAttachment(item.ID, AttachmentInput{
		Title: contentStr(" 新 "), Sort: contentInt(9), PostID: contentInt64(other.ID),
	}, actor)
	require.NoError(t, err)
	assert.Equal(t, "新", updated.Title)
	assert.Equal(t, 9, updated.Sort)
	assert.Equal(t, other.ID, updated.PostID)
	assert.Equal(t, int64(1), countRows(t, "audit_logs", "action = ?", AuditActionAttachmentUpdate))

	// 全 nil：不改也不写审计。
	same, err := svc.UpdateAttachment(item.ID, AttachmentInput{}, actor)
	require.NoError(t, err)
	assert.Equal(t, "新", same.Title)
	assert.Equal(t, int64(1), countRows(t, "audit_logs", "action = ?", AuditActionAttachmentUpdate))

	// 换到不存在的文章 → 1002。
	_, err = svc.UpdateAttachment(item.ID, AttachmentInput{PostID: contentInt64(999999)}, actor)
	assert.Equal(t, errs.ErrorNotFound, contentAppError(t, err).Code)

	// 字段非法 → 1003。
	_, err = svc.UpdateAttachment(item.ID, AttachmentInput{Sort: contentInt(-1)}, actor)
	assert.Equal(t, "sort", contentFieldErrors(t, err)[0].Field)

	// 附件不存在 → 1002。
	_, err = svc.UpdateAttachment(999999, AttachmentInput{Title: contentStr("x")}, actor)
	assert.Equal(t, errs.ErrorNotFound, contentAppError(t, err).Code)

	// 外链附件的 shareUrl 不允许被清空：CreateAttachment 就拒绝"既没有 shareUrl
	// 也没有 storage_key"的空壳，Update 必须同一条口径（D18 修复）。
	_, err = svc.UpdateAttachment(item.ID, AttachmentInput{ShareURL: contentStr("")}, actor)
	assert.Equal(t, "shareUrl", contentFieldErrors(t, err)[0].Field)

	// 本地附件（storage_key 非空）本来就没有 shareUrl，清空不算把记录变空壳。
	local := &model.PostAttachment{PostID: post.ID, Title: "本地", StorageKey: "ab/" + strings.Repeat("c", 64), CreatedAt: dbtime.Now()}
	require.NoError(t, global.DB.Create(local).Error)
	kept, err := svc.UpdateAttachment(local.ID, AttachmentInput{ShareURL: contentStr("")}, actor)
	require.NoError(t, err)
	assert.Empty(t, kept.ShareURL)
	assert.NotEmpty(t, kept.StorageKey)
}

// TestContentService_DeleteAttachmentPurgesUnreferencedFile 删除语义：
// 同一份文件被多条记录共用时，只有最后一条记录被删才动磁盘（§10）。
func TestContentService_DeleteAttachmentPurgesUnreferencedFile(t *testing.T) {
	setupEnv(t)
	actor := testActor(t)
	svc := ContentService{}

	post := insertPost(t, "文章", "attachment-purge", model.PostStatusPublished)

	key := "ab/" + strings.Repeat("b", 64)
	path := filepath.Join(global.Config.File.Dir, filepath.FromSlash(key))
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
	require.NoError(t, os.WriteFile(path, []byte("payload"), 0o600))

	first := &model.PostAttachment{PostID: post.ID, Title: "共享1", StorageKey: key, CreatedAt: dbtime.Now()}
	second := &model.PostAttachment{PostID: post.ID, Title: "共享2", StorageKey: key, CreatedAt: dbtime.Now()}
	require.NoError(t, global.DB.Create(first).Error)
	require.NoError(t, global.DB.Create(second).Error)

	// 还有别的记录引用同一份文件：文件必须留着。
	require.NoError(t, svc.DeleteAttachment(first.ID, actor))
	_, statErr := os.Stat(path)
	assert.NoError(t, statErr)
	assert.Equal(t, int64(0), countRows(t, "post_attachments", "id = ?", first.ID))
	assert.Equal(t, int64(1), countRows(t, "audit_logs", "action = ?", AuditActionAttachmentDelete))

	// 最后一条引用删掉后，文件才被清理。
	require.NoError(t, svc.DeleteAttachment(second.ID, actor))
	_, statErr = os.Stat(path)
	assert.ErrorIs(t, statErr, os.ErrNotExist)

	// 文件本就不存在：不报错（PurgeUnreferencedAttachmentFiles 的 ErrNotExist 分支）。
	ghost := &model.PostAttachment{PostID: post.ID, Title: "幽灵", StorageKey: "cd/" + strings.Repeat("c", 64), CreatedAt: dbtime.Now()}
	require.NoError(t, global.DB.Create(ghost).Error)
	require.NoError(t, svc.DeleteAttachment(ghost.ID, actor))

	// storage_key 越界（../）：只记日志，不影响删除本身。
	evil := &model.PostAttachment{PostID: post.ID, Title: "越界", StorageKey: "../../escape", CreatedAt: dbtime.Now()}
	require.NoError(t, global.DB.Create(evil).Error)
	require.NoError(t, svc.DeleteAttachment(evil.ID, actor))

	// 外链附件（storage_key 为空）不碰文件系统。
	external, err := svc.CreateAttachment(AttachmentInput{
		PostID: contentInt64(post.ID), Title: contentStr("外链"), ShareURL: contentStr("https://a.example/f"),
	}, actor)
	require.NoError(t, err)
	require.NoError(t, svc.DeleteAttachment(external.ID, actor))

	// 不存在 → 1002。
	err = svc.DeleteAttachment(999999, actor)
	assert.Equal(t, errs.ErrorNotFound, contentAppError(t, err).Code)
}

// ---------------------------------------------------------------- 设置 ----

// TestContentService_SettingsDefaults 回显可写项与只读项（迁移 000002 的种子值）。
func TestContentService_SettingsDefaults(t *testing.T) {
	setupEnv(t)

	view, err := ContentService{}.Settings()
	require.NoError(t, err)
	assert.Empty(t, view.ImgBaseURL)
	assert.Equal(t, "Asia/Shanghai", view.SiteTimezone)
	assert.Equal(t, int64(1), view.ContentVersion)
	assert.Equal(t, global.Config.Upload.Dir, view.Upload.Dir)
	assert.Equal(t, global.Config.Upload.MaxBytes, view.Upload.MaxBytes)
	assert.Equal(t, global.Config.File.Dir, view.File.Dir)
	assert.Equal(t, global.Config.File.MaxBytes, view.File.MaxBytes)
	assert.Equal(t, global.Config.Task.Enabled, view.Task.Enabled)
	assert.Equal(t, global.Config.Task.IntervalSeconds, view.Task.IntervalSeconds)
	assert.Equal(t, global.Config.DB.Path, view.DB.Path)
	assert.False(t, view.Notify.Enabled)
}

// TestContentService_SettingsTimezoneFallback 时区留空回落到默认值；imgBaseUrl 回显时裁空白。
func TestContentService_SettingsTimezoneFallback(t *testing.T) {
	setupEnv(t)
	contentPutRawSetting(t, model.SettingSiteTimezone, `""`)
	contentPutRawSetting(t, model.SettingImgBaseURL, `"  https://img.example.com  "`)

	view, err := ContentService{}.Settings()
	require.NoError(t, err)
	assert.Equal(t, publicDefaultTimezone, view.SiteTimezone)
	assert.Equal(t, "https://img.example.com", view.ImgBaseURL)
}

// TestContentService_SettingsSurfacesDirtySettingErrors 脏 settings 值必须报错，不能静默吞掉。
func TestContentService_SettingsSurfacesDirtySettingErrors(t *testing.T) {
	cases := []struct{ name, key, raw string }{
		{"imgBaseUrl 不是 JSON 字符串", model.SettingImgBaseURL, "1"},
		{"siteTimezone 不是 JSON 字符串", model.SettingSiteTimezone, "1"},
		{"contentVersion 不是 JSON 整数", model.SettingContentVersion, `"abc"`},
		{"notify 不是合法 JSON", model.SettingNotify, "{"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			setupEnv(t)
			contentPutRawSetting(t, tc.key, tc.raw)

			_, err := ContentService{}.Settings()
			require.Error(t, err)
		})
	}
}

// TestContentService_UpdateSettingsBumpsContentVersion 内容字段改动必须 content_version+1（公开 ETag 依赖它）。
func TestContentService_UpdateSettingsBumpsContentVersion(t *testing.T) {
	setupEnv(t)
	actor := testActor(t)
	svc := ContentService{}

	before, err := svc.Settings()
	require.NoError(t, err)

	view, err := svc.UpdateSettings(SettingsUpdate{
		ImgBaseURL:   contentStr("  https://img.example.com  "),
		SiteTimezone: contentStr(" UTC "),
	}, actor)
	require.NoError(t, err)
	assert.Equal(t, "https://img.example.com", view.ImgBaseURL)
	assert.Equal(t, "UTC", view.SiteTimezone)
	assert.Equal(t, before.ContentVersion+1, view.ContentVersion)
	assert.Equal(t, int64(1), countRows(t, "audit_logs",
		"action = ? AND target_id = ?", AuditActionSettingsUpdate, settingsTargetID))

	// 值确实落库。
	again, err := svc.Settings()
	require.NoError(t, err)
	assert.Equal(t, "UTC", again.SiteTimezone)
	assert.Equal(t, before.ContentVersion+1, again.ContentVersion)
}

// TestContentService_UpdateSettingsNotifyOnlyKeepsContentVersion 只改 notify 不动 content_version，
// 且审计摘要只记"配没配"，不落密钥原文（§9）。
func TestContentService_UpdateSettingsNotifyOnlyKeepsContentVersion(t *testing.T) {
	setupEnv(t)
	actor := testActor(t)
	svc := ContentService{}

	before, err := svc.Settings()
	require.NoError(t, err)

	events := NotifyEvents{JobFailed: true, DiskHigh: false, CertExpiring: true}
	view, err := svc.UpdateSettings(SettingsUpdate{Notify: &NotifyUpdate{
		Enabled:              contentBool(false),
		Channel:              contentStr(" webhook "),
		WebhookURL:           contentStr(""),
		TelegramBotToken:     contentStr(" tok-secret "),
		TelegramChatID:       contentStr(" chat-42 "),
		Events:               &events,
		DiskThresholdPercent: contentInt(50),
		CertExpireDays:       contentInt(30),
	}}, actor)
	require.NoError(t, err)
	assert.Equal(t, before.ContentVersion, view.ContentVersion)
	assert.Equal(t, NotifyChannelWebhook, view.Notify.Channel)
	assert.True(t, view.Notify.TelegramBotTokenSet)
	assert.Equal(t, "chat-42", view.Notify.TelegramChatID)
	assert.Equal(t, events, view.Notify.Events)

	meta := contentAuditMeta(t, AuditActionSettingsUpdate)
	assert.Contains(t, meta, `"channel":"webhook"`)
	assert.Contains(t, meta, `"webhookUrlSet":false`)
	assert.Contains(t, meta, `"telegramBotTokenSet":true`)
	assert.Contains(t, meta, `"telegramChatIdSet":true`)
	assert.Contains(t, meta, `"diskThresholdPercent":50`)
	assert.Contains(t, meta, `"certExpireDays":30`)
	assert.NotContains(t, meta, "tok-secret")
	assert.NotContains(t, meta, "chat-42")
}

// TestContentService_UpdateSettingsEmptyBodyIsNoop 空请求体不改任何东西，也不动 content_version。
func TestContentService_UpdateSettingsEmptyBodyIsNoop(t *testing.T) {
	setupEnv(t)
	actor := testActor(t)
	svc := ContentService{}

	before, err := svc.Settings()
	require.NoError(t, err)

	view, err := svc.UpdateSettings(SettingsUpdate{}, actor)
	require.NoError(t, err)
	assert.Equal(t, before.ContentVersion, view.ContentVersion)
	assert.Equal(t, int64(0), countRows(t, "audit_logs", "action = ?", AuditActionSettingsUpdate))
}

// TestContentService_UpdateSettingsValidation imgBaseUrl / siteTimezone 的校验与归一化。
func TestContentService_UpdateSettingsValidation(t *testing.T) {
	setupEnv(t)
	actor := testActor(t)
	svc := ContentService{}

	before, err := svc.Settings()
	require.NoError(t, err)

	badURLs := []string{
		"img.example.com",          // 没有协议
		"https://img.example.com/", // 结尾斜杠
		"ftp://img.example.com",    // 非 http(s)
		"https://" + strings.Repeat("a", settingsImgBaseURLMaxLen), // 超长
	}
	for _, bad := range badURLs {
		_, err := svc.UpdateSettings(SettingsUpdate{ImgBaseURL: contentStr(bad)}, actor)
		appErr := contentAppError(t, err)
		assert.Equal(t, errs.ErrorValidation, appErr.Code, "imgBaseUrl=%q", bad)
	}

	// 非法 IANA 时区名。
	_, err = svc.UpdateSettings(SettingsUpdate{SiteTimezone: contentStr("Mars/Olympus")}, actor)
	appErr := contentAppError(t, err)
	assert.Equal(t, errs.ErrorValidation, appErr.Code)
	assert.Contains(t, appErr.Message, "siteTimezone")

	// 失败不留半截写入，也不动 content_version。
	after, err := svc.Settings()
	require.NoError(t, err)
	assert.Equal(t, before.ContentVersion, after.ContentVersion)
	assert.Empty(t, after.ImgBaseURL)
	assert.Equal(t, "Asia/Shanghai", after.SiteTimezone)
	assert.Equal(t, int64(0), countRows(t, "audit_logs", "action = ?", AuditActionSettingsUpdate))

	// 空 imgBaseUrl 合法（表示同源相对路径），且仍然 +1。
	view, err := svc.UpdateSettings(SettingsUpdate{ImgBaseURL: contentStr("")}, actor)
	require.NoError(t, err)
	assert.Empty(t, view.ImgBaseURL)
	assert.Equal(t, before.ContentVersion+1, view.ContentVersion)

	// http:// 也合法。
	view, err = svc.UpdateSettings(SettingsUpdate{ImgBaseURL: contentStr("http://img.example.com")}, actor)
	require.NoError(t, err)
	assert.Equal(t, "http://img.example.com", view.ImgBaseURL)
	assert.Equal(t, before.ContentVersion+2, view.ContentVersion)
}

// TestContentService_UpdateSettingsNotifyValidation 告警配置先写先校验：失败时内容字段不该被改。
func TestContentService_UpdateSettingsNotifyValidation(t *testing.T) {
	setupEnv(t)
	actor := testActor(t)
	svc := ContentService{}

	before, err := svc.Settings()
	require.NoError(t, err)

	// enabled=true 但 webhook 地址为空 → 1003。
	_, err = svc.UpdateSettings(SettingsUpdate{
		ImgBaseURL: contentStr("https://img.example.com"),
		Notify:     &NotifyUpdate{Enabled: contentBool(true)},
	}, actor)
	fields := contentFieldErrors(t, err)
	assert.Contains(t, contentFieldNames(fields), "webhookUrl")

	after, err := svc.Settings()
	require.NoError(t, err)
	assert.Equal(t, before.ContentVersion, after.ContentVersion)
	assert.Empty(t, after.ImgBaseURL)
	assert.Equal(t, int64(0), countRows(t, "audit_logs", "action = ?", AuditActionSettingsUpdate))

	// 内容字段与 notify 同时改：都要落库，且只 +1。
	view, err := svc.UpdateSettings(SettingsUpdate{
		SiteTimezone: contentStr("UTC"),
		Notify:       &NotifyUpdate{Enabled: contentBool(false), Channel: contentStr("webhook")},
	}, actor)
	require.NoError(t, err)
	assert.Equal(t, "UTC", view.SiteTimezone)
	assert.Equal(t, before.ContentVersion+1, view.ContentVersion)
}

// TestContentService_UpdateSettingsBumpsEvenWhenValueUnchanged 记录一个边界现状。
func TestContentService_UpdateSettingsBumpsEvenWhenValueUnchanged(t *testing.T) {
	setupEnv(t)
	actor := testActor(t)
	svc := ContentService{}

	first, err := svc.UpdateSettings(SettingsUpdate{SiteTimezone: contentStr("UTC")}, actor)
	require.NoError(t, err)

	// 疑似缺陷：再写一次完全相同的时区，content_version 依然 +1。
	// 代码只判断"入参给了内容字段"，不比较新旧值；契约措辞是"写内容类设置必须 +1"，
	// 前端也只提交真正改动的字段，所以这里按现状断言并记录。
	second, err := svc.UpdateSettings(SettingsUpdate{SiteTimezone: contentStr("UTC")}, actor)
	require.NoError(t, err)
	assert.Equal(t, first.ContentVersion+1, second.ContentVersion)
}

// TestContentService_UpdateSettingsLeavesPartialWriteWhenVersionBumpFails
// content_version 是脏数据时，写内容字段会在落库之后失败，留下"值已改、版本没涨、审计没写"的半截状态。
func TestContentService_UpdateSettingsLeavesPartialWriteWhenVersionBumpFails(t *testing.T) {
	setupEnv(t)
	actor := testActor(t)
	svc := ContentService{}

	contentPutRawSetting(t, model.SettingContentVersion, `"abc"`)

	_, err := svc.UpdateSettings(SettingsUpdate{ImgBaseURL: contentStr("https://img.example.com")}, actor)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "不是 JSON 数字")

	// 疑似缺陷：img_base_url 已经写进去了，但 content_version 没涨、审计也没写 —— 整个写入没有事务。
	got, readErr := ServiceGroupApp.Setting.String(model.SettingImgBaseURL)
	require.NoError(t, readErr)
	assert.Equal(t, "https://img.example.com", got)
	assert.Equal(t, int64(0), countRows(t, "audit_logs", "action = ?", AuditActionSettingsUpdate))
}

// ------------------------------------------------- 写冲突 / 查询失败的翻码 ----

// TestContentService_TaxonomyWriteErrorMapsUniqueViolation 用触发器制造"预检查通过、落库时唯一索引冲突"
// 的并发场景，覆盖 taxonomyWriteError 的翻码分支。
func TestContentService_TaxonomyWriteErrorMapsUniqueViolation(t *testing.T) {
	setupEnv(t)
	actor := testActor(t)

	// AFTER INSERT 再插一条同 slug 的标签 → UNIQUE constraint failed: tags.slug。
	require.NoError(t, global.DB.Exec(`CREATE TRIGGER content_test_tags_slug_dup
		AFTER INSERT ON tags
		BEGIN
			INSERT INTO tags (name, slug, created_at) VALUES (NEW.name || '-dup', NEW.slug, 0);
		END`).Error)

	_, err := ContentService{}.CreateTag(TaxonomyInput{Name: contentStr("Race"), Slug: contentStr("race")}, actor)
	appErr := contentAppError(t, err)
	// name 没撞车 → 归为 slug 冲突（2001）。
	assert.Equal(t, errs.ErrorSlugConflict, appErr.Code)

	// 事务整体回滚，没有留下半条记录。
	assert.Equal(t, int64(0), countRows(t, "tags", ""))
}

// TestContentService_TaxonomyWriteErrorWrapsOtherFailures 非唯一冲突的写失败保持普通错误（由 api 层翻 1006）。
func TestContentService_TaxonomyWriteErrorWrapsOtherFailures(t *testing.T) {
	setupEnv(t)
	actor := testActor(t)

	require.NoError(t, global.DB.Exec(`CREATE TRIGGER content_test_tags_block
		BEFORE INSERT ON tags
		BEGIN
			SELECT RAISE(ABORT, 'boom');
		END`).Error)

	_, err := ContentService{}.CreateTag(TaxonomyInput{Name: contentStr("Blocked")}, actor)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "保存分类失败")
	assert.Contains(t, err.Error(), "boom")

	_, ok := errs.As(err)
	assert.False(t, ok, "非唯一冲突不该被翻成业务错误")
}

// TestContentService_QueryFailuresAreWrapped 读路径的查询失败要包上可读前缀，不能吞错。
func TestContentService_QueryFailuresAreWrapped(t *testing.T) {
	setupEnv(t)
	actor := testActor(t)
	svc := ContentService{}

	require.NoError(t, global.DB.Exec("DROP TABLE tags").Error)
	_, err := svc.Tags()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "查询标签失败")

	// 按主键查标签失败不是"不存在"，要走 taxonomyNotFoundOr 的另一条分支。
	_, err = svc.UpdateTag(1, TaxonomyInput{Name: contentStr("x")}, actor)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "查询分类失败")
	require.Error(t, svc.DeleteTag(1, actor))

	require.NoError(t, global.DB.Exec("DROP TABLE series").Error)
	_, err = svc.Series()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "查询系列失败")
	_, err = svc.UpdateSeries(1, TaxonomyInput{Name: contentStr("x")}, actor)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "查询分类失败")
	require.Error(t, svc.DeleteSeries(1, actor))

	require.NoError(t, global.DB.Exec("DROP TABLE links").Error)
	_, err = svc.Links()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "查询友链失败")
	_, err = svc.UpdateLink(1, LinkInput{Name: contentStr("x")}, actor)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "查询友链失败")
	require.Error(t, svc.DeleteLink(1, actor))

	require.NoError(t, global.DB.Exec("DROP TABLE redirects").Error)
	_, err = svc.Redirects()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "查询跳转失败")
	_, err = svc.UpdateRedirect(1, RedirectInput{ToPath: contentStr("/x")}, actor)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "查询跳转失败")
	require.Error(t, svc.DeleteRedirect(1, actor))

	// fromPath 查重失败：新建时连"路径是否被占用"都问不出来。
	_, err = svc.CreateRedirect(RedirectInput{FromPath: contentStr("/x"), ToPath: contentStr("/y")}, actor)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "检查 fromPath 失败")

	require.NoError(t, global.DB.Exec("DROP TABLE post_attachments").Error)
	_, _, err = svc.Attachments(0, 1, 20)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "查询附件失败")
	_, err = svc.AttachmentByID(1)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "查询附件失败")
	_, err = svc.UpdateAttachment(1, AttachmentInput{Title: contentStr("x")}, actor)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "查询附件失败")
	require.Error(t, svc.DeleteAttachment(1, actor))

	// 挂载文章的查询失败（不是"文章不存在"）。
	require.NoError(t, global.DB.Exec("DROP TABLE posts").Error)
	_, err = svc.CreateAttachment(AttachmentInput{
		PostID: contentInt64(1), Title: contentStr("x"), ShareURL: contentStr("https://a.example/f"),
	}, actor)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "查询文章失败")

	// 设置写入失败：PutString 落库出错要往上抛，不能假装成功。
	require.NoError(t, global.DB.Exec("DROP TABLE settings").Error)
	_, err = svc.UpdateSettings(SettingsUpdate{ImgBaseURL: contentStr("https://img.example.com")}, actor)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "写入设置 img_base_url 失败")
}

// TestContentService_TaxonomyLookupFailuresAreWrapped 分类查重（name / slug）自身的查询失败。
func TestContentService_TaxonomyLookupFailuresAreWrapped(t *testing.T) {
	setupEnv(t)
	actor := testActor(t)
	svc := ContentService{}

	// name 查重失败：表都没了。
	require.NoError(t, global.DB.Exec("DROP TABLE tags").Error)
	_, err := svc.CreateTag(TaxonomyInput{Name: contentStr("X")}, actor)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "检查名称失败")

	// slug 查重失败：表在，但没有 slug 列（name 查得通，slug 查不通）。
	require.NoError(t, global.DB.Exec("CREATE TABLE tags (id INTEGER PRIMARY KEY, name TEXT NOT NULL)").Error)
	_, err = svc.CreateTag(TaxonomyInput{Name: contentStr("X")}, actor)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "检查 slug 失败")
}

// TestContentService_WriteFailuresAreWrapped 写入路径的 DB 失败要包上可读前缀。
//
// 用 BEFORE INSERT/UPDATE/DELETE 触发器人为制造失败，覆盖各 CRUD 的防御分支。
func TestContentService_WriteFailuresAreWrapped(t *testing.T) {
	setupEnv(t)
	actor := testActor(t)
	svc := ContentService{}
	post := insertPost(t, "文章", "write-failure-post", model.PostStatusPublished)

	// 标签：先建好再挡写。
	tag, err := svc.CreateTag(TaxonomyInput{Name: contentStr("Tag")}, actor)
	require.NoError(t, err)
	contentBlockWrites(t, "tags", "UPDATE")
	_, err = svc.UpdateTag(tag.ID, TaxonomyInput{Name: contentStr("Tag2")}, actor)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "保存分类失败")
	contentDropTrigger(t, "tags", "UPDATE")
	contentBlockWrites(t, "tags", "DELETE")
	require.Error(t, svc.DeleteTag(tag.ID, actor))
	contentDropTrigger(t, "tags", "DELETE")

	// 系列。
	series, err := svc.CreateSeries(TaxonomyInput{Name: contentStr("Series")}, actor)
	require.NoError(t, err)
	contentBlockWrites(t, "series", "UPDATE")
	_, err = svc.UpdateSeries(series.ID, TaxonomyInput{Name: contentStr("Series2")}, actor)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "保存分类失败")
	contentDropTrigger(t, "series", "UPDATE")
	contentBlockWrites(t, "series", "DELETE")
	require.Error(t, svc.DeleteSeries(series.ID, actor))
	contentDropTrigger(t, "series", "DELETE")
	contentBlockWrites(t, "series", "INSERT")
	_, err = svc.CreateSeries(TaxonomyInput{Name: contentStr("Blocked")}, actor)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "保存分类失败")
	contentDropTrigger(t, "series", "INSERT")

	// 友链。
	link, err := svc.CreateLink(LinkInput{Name: contentStr("Link"), URL: contentStr("https://a.example")}, actor)
	require.NoError(t, err)
	contentBlockWrites(t, "links", "UPDATE")
	_, err = svc.UpdateLink(link.ID, LinkInput{Sort: contentInt(1)}, actor)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "保存友链失败")
	contentDropTrigger(t, "links", "UPDATE")
	contentBlockWrites(t, "links", "DELETE")
	require.Error(t, svc.DeleteLink(link.ID, actor))
	contentDropTrigger(t, "links", "DELETE")
	contentBlockWrites(t, "links", "INSERT")
	_, err = svc.CreateLink(LinkInput{Name: contentStr("Blocked"), URL: contentStr("https://b.example")}, actor)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "创建友链失败")
	contentDropTrigger(t, "links", "INSERT")

	// 跳转。
	redirect, err := svc.CreateRedirect(RedirectInput{FromPath: contentStr("/w"), ToPath: contentStr("/w2")}, actor)
	require.NoError(t, err)
	contentBlockWrites(t, "redirects", "UPDATE")
	_, err = svc.UpdateRedirect(redirect.ID, RedirectInput{ToPath: contentStr("/w3")}, actor)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "保存跳转失败")
	contentDropTrigger(t, "redirects", "UPDATE")
	contentBlockWrites(t, "redirects", "DELETE")
	require.Error(t, svc.DeleteRedirect(redirect.ID, actor))
	contentDropTrigger(t, "redirects", "DELETE")
	contentBlockWrites(t, "redirects", "INSERT")
	_, err = svc.CreateRedirect(RedirectInput{FromPath: contentStr("/blocked"), ToPath: contentStr("/x")}, actor)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "创建跳转失败")
	contentDropTrigger(t, "redirects", "INSERT")

	// 附件。
	attachment, err := svc.CreateAttachment(AttachmentInput{
		PostID: contentInt64(post.ID), Title: contentStr("附件"), ShareURL: contentStr("https://a.example/f"),
	}, actor)
	require.NoError(t, err)
	contentBlockWrites(t, "post_attachments", "UPDATE")
	_, err = svc.UpdateAttachment(attachment.ID, AttachmentInput{Sort: contentInt(1)}, actor)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "保存附件失败")
	contentDropTrigger(t, "post_attachments", "UPDATE")
	contentBlockWrites(t, "post_attachments", "DELETE")
	require.Error(t, svc.DeleteAttachment(attachment.ID, actor))
	contentDropTrigger(t, "post_attachments", "DELETE")
	contentBlockWrites(t, "post_attachments", "INSERT")
	_, err = svc.CreateAttachment(AttachmentInput{
		PostID: contentInt64(post.ID), Title: contentStr("Blocked"), ShareURL: contentStr("https://b.example/f"),
	}, actor)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "创建附件失败")
	contentDropTrigger(t, "post_attachments", "INSERT")
}

// TestContentService_DatabaseLevelPathConflicts 预检查通过、落库时才发生的 fromPath 冲突
// （并发窗口）也要被翻成 1004，而不是 1006。
func TestContentService_DatabaseLevelPathConflicts(t *testing.T) {
	setupEnv(t)
	actor := testActor(t)
	svc := ContentService{}

	// AFTER INSERT 再插一条同 from_path 的记录 → UNIQUE constraint failed: redirects.from_path。
	require.NoError(t, global.DB.Exec(`CREATE TRIGGER content_test_redirect_dup
		AFTER INSERT ON redirects
		BEGIN
			INSERT INTO redirects (from_path, to_path, status, hits, created_at)
			VALUES (NEW.from_path, '/dup', 301, 0, 0);
		END`).Error)

	_, err := svc.CreateRedirect(RedirectInput{FromPath: contentStr("/race"), ToPath: contentStr("/race2")}, actor)
	appErr := contentAppError(t, err)
	assert.Equal(t, errs.ErrorConflict, appErr.Code)
	assert.Equal(t, "fromPath 已存在", appErr.Message)
	require.NoError(t, global.DB.Exec("DROP TRIGGER content_test_redirect_dup").Error)
	assert.Equal(t, int64(0), countRows(t, "redirects", ""))

	// AFTER UPDATE 制造同样的冲突 → 更新路径也翻 1004。
	redirect, err := svc.CreateRedirect(RedirectInput{FromPath: contentStr("/upd"), ToPath: contentStr("/upd2")}, actor)
	require.NoError(t, err)
	require.NoError(t, global.DB.Exec(`CREATE TRIGGER content_test_redirect_upd_dup
		AFTER UPDATE ON redirects
		BEGIN
			INSERT INTO redirects (from_path, to_path, status, hits, created_at)
			VALUES (NEW.from_path, '/dup2', 301, 0, 0);
		END`).Error)

	_, err = svc.UpdateRedirect(redirect.ID, RedirectInput{ToPath: contentStr("/upd3")}, actor)
	appErr = contentAppError(t, err)
	assert.Equal(t, errs.ErrorConflict, appErr.Code)
	require.NoError(t, global.DB.Exec("DROP TRIGGER content_test_redirect_upd_dup").Error)
}
