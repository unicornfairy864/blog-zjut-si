package service

import (
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/unicornfairy864/blog-zjut-si/apps/server/core/render"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/global"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/model"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/model/common/dbtime"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/model/common/errs"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/model/common/request"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/utils"
)

// int64Ptr 造指针字面量：评论的 parent_id / root_id 都是可空列。
func int64Ptr(v int64) *int64 { return &v }

// commentTestPost 建一篇已发布文章并打开评论开关。
//
// insertPost 直插时 AllowComments 取 Go 零值 false（与列默认值 1 不同），
// 不显式打开的话所有评论提交都会命中"已关闭评论"分支。
func commentTestPost(t *testing.T, title, slug string) *model.Post {
	t.Helper()

	post := insertPost(t, title, slug, model.PostStatusPublished)
	require.NoError(t, global.DB.Model(post).Update("allow_comments", true).Error)
	post.AllowComments = true
	return post
}

// commentInsertAt 直插一条评论，用于给读路径造出任意状态 / root_id / 时间的数据。
func commentInsertAt(t *testing.T, postID int64, parentID, rootID *int64, status, name string, at dbtime.Unix) *model.Comment {
	t.Helper()

	comment := &model.Comment{
		PostID:      postID,
		ParentID:    parentID,
		RootID:      rootID,
		AuthorName:  name,
		ContentMD:   name,
		ContentHTML: "<p>" + name + "</p>",
		Status:      status,
		CreatedAt:   at,
		UpdatedAt:   at,
	}
	require.NoError(t, global.DB.Create(comment).Error)
	require.NotZero(t, comment.ID)
	return comment
}

// commentInsert 直插一条"刚刚"的评论。
func commentInsert(t *testing.T, postID int64, parentID, rootID *int64, status, name string) *model.Comment {
	t.Helper()

	return commentInsertAt(t, postID, parentID, rootID, status, name, dbtime.Now())
}

// commentActor 复用共享夹具 testActor 造审计上下文（PostActor 与 CommentActor 字段一一对应）。
func commentActor(t *testing.T) CommentActor {
	t.Helper()

	return CommentActor(testActor(t))
}

// commentAppErr 断言 err 是业务错误并取出它。
func commentAppErr(t *testing.T, err error) *errs.AppError {
	t.Helper()

	require.Error(t, err)
	appErr, ok := errs.As(err)
	require.True(t, ok, "应为业务错误，实际: %v", err)
	return appErr
}

// assertCommentErrCode 断言业务错误码。
func assertCommentErrCode(t *testing.T, err error, code int) {
	t.Helper()

	assert.Equal(t, code, commentAppErr(t, err).Code)
}

// assertCommentFieldError 断言 1003 且 Details 是约定形状的字段错误。
func assertCommentFieldError(t *testing.T, err error, field string) {
	t.Helper()

	appErr := commentAppErr(t, err)
	require.Equal(t, errs.ErrorValidation, appErr.Code)
	fields, ok := appErr.Details.([]request.FieldError)
	require.True(t, ok, "Details 应为 []request.FieldError，实际 %T", appErr.Details)
	require.NotEmpty(t, fields)
	assert.Equal(t, field, fields[0].Field)
}

// commentPostCommentCount 读 posts.comment_count（口径 = approved 评论数）。
func commentPostCommentCount(t *testing.T, postID int64) int {
	t.Helper()

	var count int
	require.NoError(t, global.DB.Raw(`SELECT comment_count FROM posts WHERE id = ?`, postID).Scan(&count).Error)
	return count
}

func TestCommentService_CreateStoresPendingAndCountsOnlyApproved(t *testing.T) {
	setupEnv(t)

	post := commentTestPost(t, "文章一", "post-one")
	svc := ServiceGroupApp.Comment

	// 600 个字符的 UA 超过 commentUAStoreLimit(500)，落库必须按字符截断。
	ua := strings.Repeat("ü", 600)
	result, err := svc.Create("post-one", CommentCreateInput{
		AuthorName:  "  Alice  ",
		AuthorEmail: " Alice@Example.com ",
		AuthorURL:   " https://example.com ",
		ContentMD:   "# 你好\n\n正文",
	}, "203.0.113.9", ua)
	require.NoError(t, err)
	assert.NotZero(t, result.ID)
	// 公开提交一律 pending，等人工审核（§5）。
	assert.Equal(t, model.CommentStatusPending, result.Status)

	stored, err := model.FindCommentByID(global.DB, result.ID)
	require.NoError(t, err)
	assert.Equal(t, model.CommentStatusPending, stored.Status)
	assert.Equal(t, post.ID, stored.PostID)
	assert.Nil(t, stored.ParentID)
	// 顶层评论的 root_id 指向自身（插入拿到自增 id 后回填）。
	require.NotNil(t, stored.RootID)
	assert.Equal(t, stored.ID, *stored.RootID)

	// 空白被裁掉，正文走与文章同一套 Markdown 渲染管线。
	assert.Equal(t, "Alice", stored.AuthorName)
	assert.Equal(t, "Alice@Example.com", stored.AuthorEmail)
	assert.Equal(t, "https://example.com", stored.AuthorURL)
	assert.Equal(t, render.Markdown("# 你好\n\n正文").HTML, stored.ContentHTML)
	// 邮箱 / IP 只落加盐哈希（§9），且哈希前先做小写归一。
	assert.Equal(t, utils.HashEmail("Alice@Example.com"), stored.EmailHash)
	assert.Equal(t, utils.HashIP("203.0.113.9"), stored.IPHash)
	assert.Equal(t, 500, utf8.RuneCountInString(stored.UA))

	// pending 不计入 posts.comment_count：口径只算 approved。
	assert.Zero(t, commentPostCommentCount(t, post.ID))

	actor := commentActor(t)
	item, err := svc.Patch(result.ID, model.CommentStatusApproved, actor)
	require.NoError(t, err)
	assert.Equal(t, model.CommentStatusApproved, item.Status)
	assert.Equal(t, 1, commentPostCommentCount(t, post.ID))

	// 再打成 spam：approved 数回落，计数跟着回落（整体重算而不是增减）。
	item, err = svc.Patch(result.ID, model.CommentStatusSpam, actor)
	require.NoError(t, err)
	assert.Equal(t, model.CommentStatusSpam, item.Status)
	assert.Zero(t, commentPostCommentCount(t, post.ID))
}

func TestCommentService_CreateNestedRepliesFlattenToSecondLevel(t *testing.T) {
	setupEnv(t)

	post := commentTestPost(t, "嵌套", "nested")
	svc := ServiceGroupApp.Comment
	actor := commentActor(t)

	top, err := svc.Create("nested", CommentCreateInput{AuthorName: "顶层", ContentMD: "top"}, "198.51.100.1", "ua")
	require.NoError(t, err)
	_, err = svc.Patch(top.ID, model.CommentStatusApproved, actor)
	require.NoError(t, err)

	reply, err := svc.Create("nested", CommentCreateInput{
		AuthorName: "回复", ContentMD: "reply", ParentID: int64Ptr(top.ID),
	}, "198.51.100.2", "ua")
	require.NoError(t, err)
	replyRow, err := model.FindCommentByID(global.DB, reply.ID)
	require.NoError(t, err)
	require.NotNil(t, replyRow.RootID)
	assert.Equal(t, top.ID, *replyRow.RootID)

	// 回复二级评论：root_id 取父评论的 root_id，于是公开列表里被压回第二层。
	nested, err := svc.Create("nested", CommentCreateInput{
		AuthorName: "回复的回复", ContentMD: "deep", ParentID: int64Ptr(reply.ID),
	}, "198.51.100.3", "ua")
	require.NoError(t, err)
	nestedRow, err := model.FindCommentByID(global.DB, nested.ID)
	require.NoError(t, err)
	require.NotNil(t, nestedRow.RootID)
	assert.Equal(t, top.ID, *nestedRow.RootID)
	// parent_id 保留原始父评论，只有 root_id 被压平（前端靠它画"回复某某"）。
	require.NotNil(t, nestedRow.ParentID)
	assert.Equal(t, reply.ID, *nestedRow.ParentID)

	_, err = svc.Patch(reply.ID, model.CommentStatusApproved, actor)
	require.NoError(t, err)
	_, err = svc.Patch(nested.ID, model.CommentStatusApproved, actor)
	require.NoError(t, err)

	list, err := svc.PublicList("nested")
	require.NoError(t, err)
	assert.Equal(t, int64(3), list.Total)
	require.Len(t, list.List, 1)
	assert.Equal(t, top.ID, list.List[0].ID)
	require.Len(t, list.List[0].Replies, 2)
	assert.Equal(t, reply.ID, list.List[0].Replies[0].ID)
	assert.Equal(t, nested.ID, list.List[0].Replies[1].ID)
	require.NotNil(t, list.List[0].Replies[1].ParentID)
	assert.Equal(t, reply.ID, *list.List[0].Replies[1].ParentID)
	// 三条 approved：comment_count 同步为 3。
	assert.Equal(t, 3, commentPostCommentCount(t, post.ID))
}

func TestCommentService_CreateRejectsBadParentAndPost(t *testing.T) {
	setupEnv(t)

	postA := commentTestPost(t, "A", "post-a")
	postB := commentTestPost(t, "B", "post-b")
	svc := ServiceGroupApp.Comment

	// 父评论属于另一篇文章：按 §5 返回字段级 1003，不能泄露"这个 id 存在"。
	foreignParent := commentInsert(t, postB.ID, nil, nil, model.CommentStatusApproved, "别处的评论")
	_, err := svc.Create("post-a", CommentCreateInput{
		AuthorName: "n", ContentMD: "c", ParentID: int64Ptr(foreignParent.ID),
	}, "203.0.113.11", "ua")
	assertCommentFieldError(t, err, "parentId")

	// 父评论不存在：与"属于别的文章"同一个错误形状（不区分，避免探测 id）。
	_, err = svc.Create("post-a", CommentCreateInput{
		AuthorName: "n", ContentMD: "c", ParentID: int64Ptr(999999),
	}, "203.0.113.12", "ua")
	assertCommentFieldError(t, err, "parentId")

	// 文章不存在 / 草稿 → 1002；归档 → 1007（与详情同口径，不泄露草稿存在性）。
	insertPost(t, "草稿", "draft-post", model.PostStatusDraft)
	insertPost(t, "归档", "archived-post", model.PostStatusArchived)
	_, err = svc.Create("no-such-post", CommentCreateInput{AuthorName: "n", ContentMD: "c"}, "203.0.113.13", "ua")
	assertCommentErrCode(t, err, errs.ErrorNotFound)
	_, err = svc.Create("draft-post", CommentCreateInput{AuthorName: "n", ContentMD: "c"}, "203.0.113.14", "ua")
	assertCommentErrCode(t, err, errs.ErrorNotFound)
	_, err = svc.Create("archived-post", CommentCreateInput{AuthorName: "n", ContentMD: "c"}, "203.0.113.15", "ua")
	assertCommentErrCode(t, err, errs.ErrorGone)

	// allow_comments=false：403 语义的 1001。
	closed := insertPost(t, "关闭评论", "closed-post", model.PostStatusPublished)
	require.False(t, closed.AllowComments, "insertPost 的零值应是 false")
	_, err = svc.Create("closed-post", CommentCreateInput{AuthorName: "n", ContentMD: "c"}, "203.0.113.16", "ua")
	assertCommentErrCode(t, err, errs.ErrorForbidden)

	// 以上失败都不该落库。
	assert.Equal(t, int64(1), countRows(t, "comments", "post_id = ?", postB.ID))
	assert.Equal(t, int64(0), countRows(t, "comments", "post_id = ?", postA.ID))
}

func TestCommentService_PublicListOnlyApprovedAndTimeOrdered(t *testing.T) {
	setupEnv(t)

	post := commentTestPost(t, "列表", "list-post")
	other := commentTestPost(t, "别的", "other-post")
	base := dbtime.Now().Add(-time.Hour)

	older := commentInsertAt(t, post.ID, nil, nil, model.CommentStatusApproved, "更早", base)
	newer := commentInsertAt(t, post.ID, nil, nil, model.CommentStatusApproved, "更晚", base.Add(time.Minute))
	commentInsert(t, post.ID, nil, nil, model.CommentStatusPending, "待审")
	commentInsert(t, post.ID, nil, nil, model.CommentStatusSpam, "垃圾")
	commentInsert(t, post.ID, nil, nil, model.CommentStatusDeleted, "已删")
	// 另一篇文章的 approved 评论不能串进本文章的列表。
	commentInsert(t, other.ID, nil, nil, model.CommentStatusApproved, "别处")

	list, err := ServiceGroupApp.Comment.PublicList("list-post")
	require.NoError(t, err)
	assert.Equal(t, int64(2), list.Total)
	require.Len(t, list.List, 2)
	// 按 created_at 升序。
	assert.Equal(t, older.ID, list.List[0].ID)
	assert.Equal(t, newer.ID, list.List[1].ID)
	// 顶层评论的 replies 固定为空数组（形状稳定，不是 null）。
	assert.NotNil(t, list.List[0].Replies)
	assert.Empty(t, list.List[0].Replies)
}

func TestCommentService_PublicListHidesUnpublishedPosts(t *testing.T) {
	setupEnv(t)

	draft := insertPost(t, "草稿", "draft-post", model.PostStatusDraft)
	commentInsert(t, draft.ID, nil, nil, model.CommentStatusApproved, "草稿下的评论")
	insertPost(t, "归档", "archived-post", model.PostStatusArchived)
	svc := ServiceGroupApp.Comment

	// 未发布文章的评论不可见：列表接口直接按详情口径报错。
	_, err := svc.PublicList("draft-post")
	assertCommentErrCode(t, err, errs.ErrorNotFound)
	_, err = svc.PublicList("archived-post")
	assertCommentErrCode(t, err, errs.ErrorGone)
	_, err = svc.PublicList("no-such-post")
	assertCommentErrCode(t, err, errs.ErrorNotFound)
}

func TestCommentService_AdminListStatusFilterAndPaging(t *testing.T) {
	setupEnv(t)

	post := commentTestPost(t, "管理", "admin-post")
	base := dbtime.Now().Add(-time.Hour)
	commentInsertAt(t, post.ID, nil, nil, model.CommentStatusPending, "p1", base)
	commentInsertAt(t, post.ID, nil, nil, model.CommentStatusPending, "p2", base.Add(time.Minute))
	p3 := commentInsertAt(t, post.ID, nil, nil, model.CommentStatusPending, "p3", base.Add(2*time.Minute))
	commentInsertAt(t, post.ID, nil, nil, model.CommentStatusApproved, "a1", base.Add(3*time.Minute))
	commentInsertAt(t, post.ID, nil, nil, model.CommentStatusApproved, "a2", base.Add(4*time.Minute))
	// 评论挂在已被物理删除的文章下：LEFT JOIN + COALESCE 兜住 NULL（slug/title 为空串）。
	orphan := commentInsert(t, 999999, nil, nil, model.CommentStatusPending, "孤儿")
	svc := ServiceGroupApp.Comment

	// status 缺省 = pending，且按 created_at 倒序。
	items, total, err := svc.AdminList("", 1, 2)
	require.NoError(t, err)
	assert.Equal(t, int64(4), total)
	require.Len(t, items, 2)
	assert.Equal(t, orphan.ID, items[0].ID)
	assert.Empty(t, items[0].PostSlug)
	assert.Empty(t, items[0].PostTitle)
	assert.Equal(t, p3.ID, items[1].ID)
	assert.Equal(t, "admin-post", items[1].PostSlug)
	assert.Equal(t, "管理", items[1].PostTitle)

	// all = 不过滤。
	all, total, err := svc.AdminList("all", 1, 100)
	require.NoError(t, err)
	assert.Equal(t, int64(6), total)
	assert.Len(t, all, 6)

	// 单状态过滤。
	approved, total, err := svc.AdminList("approved", 1, 100)
	require.NoError(t, err)
	assert.Equal(t, int64(2), total)
	assert.Len(t, approved, 2)

	// 分页：page=2 / pageSize=4 → 偏移 4，剩 2 条。
	page2, total, err := svc.AdminList("all", 2, 4)
	require.NoError(t, err)
	assert.Equal(t, int64(6), total)
	assert.Len(t, page2, 2)

	// 没有数据的状态：空数组而不是 nil。
	spam, total, err := svc.AdminList("spam", 1, 10)
	require.NoError(t, err)
	assert.Zero(t, total)
	assert.NotNil(t, spam)
	assert.Empty(t, spam)

	// 非法状态筛选 → 1003 字段错误。
	_, _, err = svc.AdminList("bogus", 1, 10)
	assertCommentFieldError(t, err, "status")
}

func TestCommentService_PatchStatusAuditAndMissingID(t *testing.T) {
	setupEnv(t)

	post := commentTestPost(t, "审核", "patch-post")
	comment := commentInsert(t, post.ID, nil, nil, model.CommentStatusPending, "待审")
	svc := ServiceGroupApp.Comment
	actor := commentActor(t)

	// 状态前后空白应被裁掉。
	item, err := svc.Patch(comment.ID, " approved ", actor)
	require.NoError(t, err)
	require.NotNil(t, item)
	assert.Equal(t, model.CommentStatusApproved, item.Status)
	assert.Equal(t, "patch-post", item.PostSlug)
	assert.Equal(t, "审核", item.PostTitle)
	assert.Equal(t, 1, commentPostCommentCount(t, post.ID))

	// 审计：动作 / 目标 / 元数据（from → to）。
	assert.Equal(t, int64(1), countRows(t, "audit_logs",
		"action = ? AND target_type = ? AND target_id = ?",
		AuditActionCommentApprove, "comment", strconv.FormatInt(comment.ID, 10)))
	var meta string
	require.NoError(t, global.DB.Raw(`SELECT meta_json FROM audit_logs WHERE action = ?`,
		AuditActionCommentApprove).Scan(&meta).Error)
	assert.JSONEq(t, `{"from":"pending","to":"approved"}`, meta)

	// 再改 spam：comment_count 回落。
	item, err = svc.Patch(comment.ID, model.CommentStatusSpam, actor)
	require.NoError(t, err)
	assert.Equal(t, model.CommentStatusSpam, item.Status)
	assert.Zero(t, commentPostCommentCount(t, post.ID))

	// 三种合法目标状态的审计动作各不相同（deleted 走 comment.delete）。
	item, err = svc.Patch(comment.ID, model.CommentStatusDeleted, actor)
	require.NoError(t, err)
	assert.Equal(t, model.CommentStatusDeleted, item.Status)
	assert.Zero(t, commentPostCommentCount(t, post.ID))
	assert.Equal(t, int64(1), countRows(t, "audit_logs", "action = ?", AuditActionCommentDelete))

	// 不存在的 id → 1002。
	_, err = svc.Patch(999999, model.CommentStatusApproved, actor)
	assertCommentErrCode(t, err, errs.ErrorNotFound)

	// pending 不是合法审核目标（不能"回到待审"），空串同理。
	for _, status := range []string{model.CommentStatusPending, "bogus", ""} {
		_, err = svc.Patch(comment.ID, status, actor)
		assertCommentFieldError(t, err, "status")
	}
}

func TestCommentService_BulkStatusAndRecount(t *testing.T) {
	setupEnv(t)

	postA := commentTestPost(t, "批量A", "bulk-a")
	postB := commentTestPost(t, "批量B", "bulk-b")
	c1 := commentInsert(t, postA.ID, nil, nil, model.CommentStatusPending, "b1")
	c2 := commentInsert(t, postA.ID, nil, nil, model.CommentStatusPending, "b2")
	c3 := commentInsert(t, postB.ID, nil, nil, model.CommentStatusPending, "b3")
	svc := ServiceGroupApp.Comment
	actor := commentActor(t)

	updated, err := svc.Bulk([]int64{c1.ID, c2.ID, c3.ID}, model.CommentStatusApproved, actor)
	require.NoError(t, err)
	assert.Equal(t, int64(3), updated)
	// 按文章分组重算 comment_count。
	assert.Equal(t, 2, commentPostCommentCount(t, postA.ID))
	assert.Equal(t, 1, commentPostCommentCount(t, postB.ID))

	// 审计：一条 bulk 记录，target_id 是逗号分隔的 id 串。
	assert.Equal(t, int64(1), countRows(t, "audit_logs", "action = ?", AuditActionCommentBulk))
	var targetID string
	require.NoError(t, global.DB.Raw(`SELECT target_id FROM audit_logs WHERE action = ?`,
		AuditActionCommentBulk).Scan(&targetID).Error)
	assert.Equal(t, strings.Join([]string{
		strconv.FormatInt(c1.ID, 10), strconv.FormatInt(c2.ID, 10), strconv.FormatInt(c3.ID, 10),
	}, ","), targetID)

	// 不存在的 id 不报错，返回的是实际受影响行数；postA 只剩 c2 一条 approved。
	updated, err = svc.Bulk([]int64{c1.ID, 999999}, model.CommentStatusSpam, actor)
	require.NoError(t, err)
	assert.Equal(t, int64(1), updated)
	assert.Equal(t, 1, commentPostCommentCount(t, postA.ID))
	// postB 不在这次批量里，计数保持不动。
	assert.Equal(t, 1, commentPostCommentCount(t, postB.ID))

	// 空 ids：0 条更新；审计仍会写一条（count=0），记录"管理员点了空批量"。
	audits := countRows(t, "audit_logs", "action = ?", AuditActionCommentBulk)
	updated, err = svc.Bulk(nil, model.CommentStatusApproved, actor)
	require.NoError(t, err)
	assert.Zero(t, updated)
	assert.Equal(t, audits+1, countRows(t, "audit_logs", "action = ?", AuditActionCommentBulk))

	// 非法目标状态 → 1003（在事务之前就拒绝）。
	_, err = svc.Bulk([]int64{c1.ID}, model.CommentStatusPending, actor)
	assertCommentFieldError(t, err, "status")
}

// TestBuildPublicComments_ResolvesRootAndPromotesOrphans 直接覆盖两层的聚合算法。
//
// 走数据库很难造出"root_id 指向不在 approved 集合里的评论"这种脏数据，
// 这里直接喂 model.Comment 切片，覆盖 publicCommentRoot 的两条回溯分支与
// buildPublicComments 的"祖先不可达时退化为顶层"分支。
func TestBuildPublicComments_ResolvesRootAndPromotesOrphans(t *testing.T) {
	base := dbtime.Now().Add(-time.Hour)
	makeComment := func(id int64, parentID, rootID *int64, offset time.Duration) model.Comment {
		return model.Comment{
			ID: id, PostID: 7, ParentID: parentID, RootID: rootID,
			AuthorName: "n", ContentHTML: "<p>n</p>", Status: model.CommentStatusApproved,
			CreatedAt: base.Add(offset),
		}
	}

	top := makeComment(1, nil, int64Ptr(1), 0)
	replyA := makeComment(2, int64Ptr(1), int64Ptr(1), time.Minute)
	deepB := makeComment(3, int64Ptr(2), int64Ptr(1), 2*time.Minute)
	// root_id 脏（指向不存在/未过审的 id）：沿 parent_id 回溯到顶层 1。
	dirtyRoot := makeComment(4, int64Ptr(1), int64Ptr(999), 3*time.Minute)
	// root_id 指向的是一条二级回复（不是顶层）：同样退回回溯。
	replyRoot := makeComment(5, int64Ptr(1), int64Ptr(2), 4*time.Minute)
	// 父评论不在集合里：祖先不可达，退化为顶层输出，避免评论凭空消失。
	orphan := makeComment(6, int64Ptr(999), int64Ptr(999), 5*time.Minute)

	list := buildPublicComments([]model.Comment{top, replyA, deepB, dirtyRoot, replyRoot, orphan})
	assert.Equal(t, int64(6), list.Total)
	require.Len(t, list.List, 2)
	assert.Equal(t, int64(1), list.List[0].ID)
	require.Len(t, list.List[0].Replies, 4)
	assert.Equal(t, []int64{2, 3, 4, 5}, []int64{
		list.List[0].Replies[0].ID,
		list.List[0].Replies[1].ID,
		list.List[0].Replies[2].ID,
		list.List[0].Replies[3].ID,
	})
	assert.Equal(t, int64(6), list.List[1].ID)
	assert.Empty(t, list.List[1].Replies)
}

// TestPublicCommentRoot_WalkLimitStopsCycle 覆盖 commentRootWalkLimit 的防御分支：
// 脏数据成环时不能死循环，回溯到层数上限就停下。
func TestPublicCommentRoot_WalkLimitStopsCycle(t *testing.T) {
	// A(10) ↔ B(11) 互为父子：无论从哪条开始，16 步后都停在自身。
	a := &model.Comment{ID: 10, ParentID: int64Ptr(11)}
	b := &model.Comment{ID: 11, ParentID: int64Ptr(10)}
	byID := map[int64]*model.Comment{10: a, 11: b}

	assert.Equal(t, int64(10), publicCommentRoot(a, byID))
	assert.Equal(t, int64(11), publicCommentRoot(b, byID))

	// 成环的两条在聚合里各自成为顶层，谁都不会丢。
	list := buildPublicComments([]model.Comment{*a, *b})
	assert.Equal(t, int64(2), list.Total)
	require.Len(t, list.List, 2)
	assert.Equal(t, int64(10), list.List[0].ID)
	assert.Equal(t, int64(11), list.List[1].ID)
}

// TestCommentLimiterContract_ThreePerTenMinutes 覆盖 §9 的评论限流契约（3 次 / 10 分钟 / IP）。
//
// 覆盖说明：评论限流器不在 CommentService 里，而在 router 层
// （router/public.go → middleware.NewCommentLimiter）。service 包的测试无法 import
// middleware（middleware/auth.go 反向依赖 service，会成环），所以这里用同一组参数
// 构造 utils.KeyedLimiter 验证契约，并顺带断言 CommentService.Create 自身不做限流——
// 服务层单测因此无法直接断言 ErrorRateLimited(1005)，限流由中间件返回。
func TestCommentLimiterContract_ThreePerTenMinutes(t *testing.T) {
	limiter := utils.NewKeyedLimiter(10*time.Minute/3, 3)
	const ip = "203.0.113.30"

	for i := 1; i <= 3; i++ {
		assert.True(t, limiter.Allow(ip), "第 %d 次应在配额内", i)
	}
	assert.False(t, limiter.Allow(ip), "第 4 次应被限流（1005）")
	// 桶按 key 隔离：换个 IP 不受影响。
	assert.True(t, limiter.Allow("203.0.113.31"))

	// Create 自身不限流：连续第 4 次提交仍然成功落库（限流在 router 中间件）。
	setupEnv(t)
	post := commentTestPost(t, "限流", "rate-post")
	svc := ServiceGroupApp.Comment
	for i := 1; i <= 4; i++ {
		_, err := svc.Create("rate-post", CommentCreateInput{AuthorName: "n", ContentMD: "c"}, ip, "ua")
		require.NoError(t, err, "第 %d 次提交", i)
	}
	assert.Equal(t, int64(4), countRows(t, "comments", "post_id = ?", post.ID))
}
