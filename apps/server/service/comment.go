package service

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"gorm.io/gorm"

	"github.com/unicornfairy864/blog-zjut-si/apps/server/core/render"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/global"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/model"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/model/common/dbtime"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/model/common/errs"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/model/common/request"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/utils"
)

// CommentService 处理评论的公开读写与管理端审核（agent.md §5 / §9 / §16 G5）。
//
// 分层：SQL 只在 model；本层只写业务规则，不接收 *gin.Context——
// 来源 IP 与 UA 由 api 层作为参数传入，事务在这里发起。
type CommentService struct{}

const (
	// commentAdminDefaultStatus 是管理端列表缺省筛选的状态（§5）。
	commentAdminDefaultStatus = model.CommentStatusPending
	// commentUAStoreLimit 是落库 UA 的字符上限，避免超长请求头撑大表。
	commentUAStoreLimit = 500
	// commentRootWalkLimit 是公开列表回溯顶层祖先的最大层数，防御脏数据成环。
	commentRootWalkLimit = 16
)

// CommentActor 是评论管理写操作的审计上下文；service 不接触 *gin.Context。
type CommentActor struct {
	AdminID int64
	IP      string
}

// CommentCreateInput 是公开提交评论的入参；格式校验在 api 层完成，这里只做业务校验。
type CommentCreateInput struct {
	AuthorName  string
	AuthorEmail string
	AuthorURL   string
	ContentMD   string
	ParentID    *int64
}

// CommentCreateResult 是公开提交评论的输出。
type CommentCreateResult struct {
	ID        int64
	Status    string
	CreatedAt dbtime.Unix
}

// PublicList 返回某文章的公开评论：只出 approved，嵌套固定两层（§5 / §16 G5）。
//
// 文章不存在 / 草稿 → 404/1002，归档 / 已删除 → 410/1007，与详情同口径。
func (s CommentService) PublicList(slug string) (model.PublicCommentList, error) {
	post, err := ServiceGroupApp.Post.PublicBySlug(slug)
	if err != nil {
		return model.PublicCommentList{}, err
	}

	comments, err := model.ListApprovedComments(global.DB, post.ID)
	if err != nil {
		return model.PublicCommentList{}, fmt.Errorf("查询评论失败: %w", err)
	}
	return buildPublicComments(comments), nil
}

// Create 提交一条评论：一律落 pending 等审核，content_html 与正文同一套渲染管线（§5 / §11）。
func (s CommentService) Create(slug string, in CommentCreateInput, ip, ua string) (CommentCreateResult, error) {
	post, err := ServiceGroupApp.Post.PublicBySlug(slug)
	if err != nil {
		return CommentCreateResult{}, err
	}
	if !post.AllowComments {
		return CommentCreateResult{}, errs.New(errs.ErrorForbidden, "该文章已关闭评论")
	}

	var parent *model.Comment
	if in.ParentID != nil {
		parent, err = s.parentComment(*in.ParentID, post.ID)
		if err != nil {
			return CommentCreateResult{}, err
		}
	}

	name := strings.TrimSpace(in.AuthorName)
	email := strings.TrimSpace(in.AuthorEmail)
	content := strings.TrimSpace(in.ContentMD)
	now := dbtime.Now()

	comment := &model.Comment{
		PostID:      post.ID,
		ParentID:    in.ParentID,
		AuthorName:  name,
		AuthorEmail: email,
		EmailHash:   utils.HashEmail(email),
		AuthorURL:   strings.TrimSpace(in.AuthorURL),
		ContentMD:   content,
		ContentHTML: render.Markdown(content).HTML,
		Status:      model.CommentStatusPending,
		IPHash:      utils.HashIP(ip),
		UA:          truncateRunes(ua, commentUAStoreLimit),
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if parent != nil {
		rootID := parent.ID
		if parent.RootID != nil {
			rootID = *parent.RootID
		}
		comment.RootID = &rootID
	}

	if err := global.DB.Transaction(func(tx *gorm.DB) error {
		if err := model.CreateComment(tx, comment); err != nil {
			return err
		}
		if comment.ParentID != nil {
			return nil
		}
		// 顶层评论的 root_id 指向自身：插入拿到自增 id 后回填。
		comment.RootID = &comment.ID
		return model.SetCommentRootID(tx, comment.ID, comment.ID)
	}); err != nil {
		return CommentCreateResult{}, fmt.Errorf("保存评论失败: %w", err)
	}

	return CommentCreateResult{ID: comment.ID, Status: comment.Status, CreatedAt: comment.CreatedAt}, nil
}

// AdminList 返回管理端评论分页；status 缺省 pending，all 表示全部（§5）。
func (s CommentService) AdminList(status string, page, pageSize int) ([]model.AdminCommentItem, int64, error) {
	filter, err := normalizeCommentStatusFilter(status)
	if err != nil {
		return nil, 0, err
	}

	rows, total, err := model.ListCommentsForAdmin(global.DB, filter, pageSize, (page-1)*pageSize)
	if err != nil {
		return nil, 0, fmt.Errorf("查询评论失败: %w", err)
	}

	items := make([]model.AdminCommentItem, 0, len(rows))
	for i := range rows {
		items = append(items, rows[i].Item())
	}
	return items, total, nil
}

// Patch 审核单条评论：只接受 approved|spam|deleted，并同步 posts.comment_count（§5）。
func (s CommentService) Patch(id int64, status string, actor CommentActor) (*model.AdminCommentItem, error) {
	status = strings.TrimSpace(status)
	action, err := commentStatusAction(status)
	if err != nil {
		return nil, err
	}

	comment, err := model.FindCommentByID(global.DB, id)
	if err != nil {
		if errors.Is(err, model.ErrNotFound) {
			return nil, errs.New(errs.ErrorNotFound, "评论不存在")
		}
		return nil, fmt.Errorf("查询评论失败: %w", err)
	}

	now := dbtime.Now()
	if err := global.DB.Transaction(func(tx *gorm.DB) error {
		if _, err := model.UpdateCommentStatus(tx, id, status, now); err != nil {
			return err
		}
		if err := model.RecountPostCommentCount(tx, comment.PostID); err != nil {
			return err
		}
		return ServiceGroupApp.Audit.WriteTx(tx, AuditInput{
			AdminID:    actor.AdminID,
			Action:     action,
			TargetType: "comment",
			TargetID:   strconv.FormatInt(id, 10),
			Meta:       map[string]any{"from": comment.Status, "to": status},
			IP:         actor.IP,
		})
	}); err != nil {
		return nil, fmt.Errorf("更新评论状态失败: %w", err)
	}

	row, err := model.FindAdminCommentRow(global.DB, id)
	if err != nil {
		return nil, fmt.Errorf("查询评论失败: %w", err)
	}
	item := row.Item()
	return &item, nil
}

// Bulk 批量审核评论，返回实际更新的条数；ids 已由 api 层校验并去重（§5）。
func (s CommentService) Bulk(ids []int64, status string, actor CommentActor) (int64, error) {
	status = strings.TrimSpace(status)
	if _, err := commentStatusAction(status); err != nil {
		return 0, err
	}

	now := dbtime.Now()
	var updated int64
	if err := global.DB.Transaction(func(tx *gorm.DB) error {
		affected, err := model.BulkUpdateCommentStatus(tx, ids, status, now)
		if err != nil {
			return err
		}
		updated = affected

		postIDs, err := model.ListCommentPostIDs(tx, ids)
		if err != nil {
			return err
		}
		if err := model.RecountPostCommentCounts(tx, postIDs); err != nil {
			return err
		}

		return ServiceGroupApp.Audit.WriteTx(tx, AuditInput{
			AdminID:    actor.AdminID,
			Action:     AuditActionCommentBulk,
			TargetType: "comment",
			TargetID:   commentIDList(ids),
			Meta:       map[string]any{"status": status, "count": len(ids)},
			IP:         actor.IP,
		})
	}); err != nil {
		return 0, fmt.Errorf("批量更新评论失败: %w", err)
	}
	return updated, nil
}

// parentComment 校验父评论存在且属于同一篇文章；不满足时按 §5 返回字段级 1003。
func (s CommentService) parentComment(parentID, postID int64) (*model.Comment, error) {
	parent, err := model.FindCommentByID(global.DB, parentID)
	if err != nil {
		if errors.Is(err, model.ErrNotFound) {
			return nil, commentFieldError("parentId", "父评论不存在或不属于该文章")
		}
		return nil, fmt.Errorf("查询父评论失败: %w", err)
	}
	if parent.PostID != postID {
		return nil, commentFieldError("parentId", "父评论不存在或不属于该文章")
	}
	return parent, nil
}

// buildPublicComments 把按 created_at 升序的评论聚合成固定两层（§5）。
//
// 顶层 = parent_id 为 NULL；其余挂到解析出的顶层祖先下，祖先不在 approved 集合里时
// 退化为顶层输出，避免已过审的评论因为祖先未过审而凭空消失。
func buildPublicComments(comments []model.Comment) model.PublicCommentList {
	list := model.PublicCommentList{
		Total: int64(len(comments)),
		List:  make([]model.PublicComment, 0, len(comments)),
	}
	byID := make(map[int64]*model.Comment, len(comments))
	for i := range comments {
		byID[comments[i].ID] = &comments[i]
	}

	index := make(map[int64]int, len(comments))
	for i := range comments {
		if comments[i].ParentID != nil {
			continue
		}
		list.List = append(list.List, comments[i].PublicTop())
		index[comments[i].ID] = len(list.List) - 1
	}

	// 评论已按 created_at 升序，父评论必然先于子评论出现。
	for i := range comments {
		comment := &comments[i]
		if comment.ParentID == nil {
			continue
		}
		if pos, ok := index[publicCommentRoot(comment, byID)]; ok {
			list.List[pos].Replies = append(list.List[pos].Replies, comment.PublicReply())
			continue
		}
		list.List = append(list.List, comment.PublicTop())
		index[comment.ID] = len(list.List) - 1
	}
	return list
}

// publicCommentRoot 解析一条回复的顶层祖先 id。
//
// 优先用 root_id（提交时维护）；root 不在 approved 集合里（祖先未过审或数据脏）时沿
// parent_id 回溯到 approved 集合里能到达的最上层，链断了就退到该层，避免评论丢失。
func publicCommentRoot(comment *model.Comment, byID map[int64]*model.Comment) int64 {
	if comment.RootID != nil {
		if root, ok := byID[*comment.RootID]; ok && root.ParentID == nil {
			return *comment.RootID
		}
	}

	current := comment
	for i := 0; i < commentRootWalkLimit && current.ParentID != nil; i++ {
		parent, ok := byID[*current.ParentID]
		if !ok {
			break
		}
		current = parent
	}
	return current.ID
}

// normalizeCommentStatusFilter 校验管理端列表的状态筛选；空值取 pending，all 表示不过滤。
func normalizeCommentStatusFilter(status string) (string, error) {
	trimmed := strings.TrimSpace(status)
	switch trimmed {
	case "", commentAdminDefaultStatus:
		return model.CommentStatusPending, nil
	case "all":
		return "", nil
	case model.CommentStatusApproved, model.CommentStatusSpam, model.CommentStatusDeleted:
		return trimmed, nil
	default:
		return "", commentFieldError("status", "状态只接受 pending、approved、spam、deleted 或 all")
	}
}

// commentStatusAction 返回目标状态对应的审计动作；pending 等非法目标状态返回 1003。
func commentStatusAction(status string) (string, error) {
	switch strings.TrimSpace(status) {
	case model.CommentStatusApproved:
		return AuditActionCommentApprove, nil
	case model.CommentStatusSpam:
		return AuditActionCommentSpam, nil
	case model.CommentStatusDeleted:
		return AuditActionCommentDelete, nil
	default:
		return "", commentFieldError("status", "状态只接受 approved、spam 或 deleted")
	}
}

// commentFieldError 构造带字段错误的 1003；data 形状与 request.FieldError 一致（§5）。
func commentFieldError(field, message string) error {
	return errs.WithDetails(errs.ErrorValidation, "参数校验失败",
		[]request.FieldError{{Field: field, Message: message}})
}

// commentIDList 把 id 列表拼成逗号分隔串，写进 audit_logs.target_id（§9）。
func commentIDList(ids []int64) string {
	parts := make([]string, 0, len(ids))
	for _, id := range ids {
		parts = append(parts, strconv.FormatInt(id, 10))
	}
	return strings.Join(parts, ",")
}
