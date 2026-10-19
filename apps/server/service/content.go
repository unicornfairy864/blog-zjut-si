package service

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"gorm.io/gorm"

	"github.com/unicornfairy864/blog-zjut-si/apps/server/global"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/model"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/model/common/dbtime"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/model/common/errs"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/model/common/request"
)

// ContentService 处理分类（标签 / 系列）、友链、跳转、附件与站点设置（agent.md §5「内容管理口径」）。
//
// 分层：SQL 只在 model，本层只写业务规则，不接收 *gin.Context；审计上下文由 api 层传入。
// 字段级校验在这里做（api 层只做绑定与 :id 解析），失败统一回 1003 且 data 带字段数组。
type ContentService struct{}

const (
	// tableTags / tableSeries 是分类查重用的表名（model 的 name / slug 查重函数按表名入参）。
	tableTags   = "tags"
	tableSeries = "series"

	// taxonomyNameMaxLen / taxonomyDescriptionMaxLen 是分类字段上限（§5）。
	taxonomyNameMaxLen        = 200
	taxonomyDescriptionMaxLen = 500

	// 友链字段上限（§5）。
	linkNameMaxLen        = 100
	linkURLMaxLen         = 500
	linkAvatarMaxLen      = 500
	linkDescriptionMaxLen = 200
	linkSortMax           = 99999

	// 跳转字段上限与允许的状态码（§5：只允许 301 / 302）。
	redirectFromPathMaxLen = 200
	redirectToPathMaxLen   = 500
	redirectStatusMoved    = 301
	redirectStatusFound    = 302

	// 附件字段上限（§5）。
	attachmentTitleMaxLen    = 200
	attachmentShareURLMaxLen = 500
	attachmentFileNameMaxLen = 200
	attachmentVersionMaxLen  = 50
	attachmentSortMax        = 99999

	// settingsImgBaseURLMaxLen 是 img_base_url 的字符上限（§5）。
	settingsImgBaseURLMaxLen = 200

	// settingsTargetID 是设置类审计记录的 target_id：设置没有主键，固定用 "site"。
	settingsTargetID = "site"
)

// TaxonomyInput 是标签 / 系列的写入入参；nil 表示"不修改"（PUT 语义）。
type TaxonomyInput struct {
	Name        *string
	Slug        *string
	Description *string
}

// Tags 返回全部标签（不分页），count 为该标签下未删除文章数（含草稿）。
func (s ContentService) Tags() ([]model.AdminTagItem, error) {
	items, err := model.ListAdminTags(global.DB)
	if err != nil {
		return nil, fmt.Errorf("查询标签失败: %w", err)
	}
	return items, nil
}

// CreateTag 新建标签：name 必填且唯一，slug 留空时按 name 生成（§5）。
func (s ContentService) CreateTag(in TaxonomyInput, actor PostActor) (*model.Tag, error) {
	name := strings.TrimSpace(derefString(in.Name))
	description := strings.TrimSpace(derefString(in.Description))

	if fields := validateTaxonomyFields(name, description); len(fields) > 0 {
		return nil, contentFieldError(fields)
	}
	if err := ensureNameFree(tableTags, name, 0); err != nil {
		return nil, err
	}
	slug, err := resolveTaxonomySlug(in.Slug, name, tableTags, 0)
	if err != nil {
		return nil, err
	}

	tag := &model.Tag{
		Name:        name,
		Slug:        slug,
		Description: description,
		CreatedAt:   dbtime.Now(),
	}
	if err := global.DB.Transaction(func(tx *gorm.DB) error {
		if err := model.CreateTag(tx, tag); err != nil {
			return err
		}
		return ServiceGroupApp.Audit.WriteTx(tx, AuditInput{
			AdminID:    actor.AdminID,
			Action:     AuditActionTagCreate,
			TargetType: "tag",
			TargetID:   strconv.FormatInt(tag.ID, 10),
			Meta:       map[string]any{"name": tag.Name, "slug": tag.Slug},
			IP:         actor.IP,
		})
	}); err != nil {
		return nil, taxonomyWriteError(err, tableTags, name, 0)
	}
	return tag, nil
}

// UpdateTag 局部更新标签；nil 字段保持不变，空 slug 按当前 name 重新生成。
func (s ContentService) UpdateTag(id int64, in TaxonomyInput, actor PostActor) (*model.Tag, error) {
	tag, err := model.FindTagByID(global.DB, id)
	if err != nil {
		return nil, taxonomyNotFoundOr(err, "标签不存在")
	}

	fields := make(map[string]any, 3)
	changed := make([]string, 0, 3)
	name := tag.Name

	if in.Name != nil {
		name = strings.TrimSpace(*in.Name)
		if errFields := validateTaxonomyFields(name, tag.Description); len(errFields) > 0 {
			return nil, contentFieldError(errFields)
		}
		if name != tag.Name {
			if err := ensureNameFree(tableTags, name, id); err != nil {
				return nil, err
			}
			fields["name"] = name
			changed = append(changed, "name")
		}
	}
	if in.Description != nil {
		description := strings.TrimSpace(*in.Description)
		if errFields := validateTaxonomyFields(name, description); len(errFields) > 0 {
			return nil, contentFieldError(errFields)
		}
		if description != tag.Description {
			fields["description"] = description
			changed = append(changed, "description")
		}
	}
	if in.Slug != nil {
		slug, err := resolveTaxonomySlug(in.Slug, name, tableTags, id)
		if err != nil {
			return nil, err
		}
		if slug != tag.Slug {
			fields["slug"] = slug
			changed = append(changed, "slug")
		}
	}

	if len(changed) == 0 {
		return tag, nil
	}
	if err := global.DB.Transaction(func(tx *gorm.DB) error {
		if err := model.UpdateTagFields(tx, id, fields); err != nil {
			return err
		}
		return ServiceGroupApp.Audit.WriteTx(tx, AuditInput{
			AdminID:    actor.AdminID,
			Action:     AuditActionTagUpdate,
			TargetType: "tag",
			TargetID:   strconv.FormatInt(id, 10),
			Meta:       map[string]any{"fields": changed},
			IP:         actor.IP,
		})
	}); err != nil {
		return nil, taxonomyWriteError(err, tableTags, name, id)
	}
	return model.FindTagByID(global.DB, id)
}

// DeleteTag 删除标签并清掉文章关联，不删文章（§5）；审计在事务外尽力写入。
func (s ContentService) DeleteTag(id int64, actor PostActor) error {
	tag, err := model.FindTagByID(global.DB, id)
	if err != nil {
		return taxonomyNotFoundOr(err, "标签不存在")
	}
	if err := model.DeleteTag(global.DB, id); err != nil {
		return fmt.Errorf("删除标签失败: %w", err)
	}
	ServiceGroupApp.Audit.Write(AuditInput{
		AdminID:    actor.AdminID,
		Action:     AuditActionTagDelete,
		TargetType: "tag",
		TargetID:   strconv.FormatInt(id, 10),
		Meta:       map[string]any{"name": tag.Name, "slug": tag.Slug},
		IP:         actor.IP,
	})
	return nil
}

// Series 返回全部系列（不分页），count 为该系列下未删除文章数（含草稿）。
func (s ContentService) Series() ([]model.AdminSeriesItem, error) {
	items, err := model.ListAdminSeries(global.DB)
	if err != nil {
		return nil, fmt.Errorf("查询系列失败: %w", err)
	}
	return items, nil
}

// CreateSeries 新建系列：name 必填且唯一，slug 留空时按 name 生成（§5）。
func (s ContentService) CreateSeries(in TaxonomyInput, actor PostActor) (*model.Series, error) {
	name := strings.TrimSpace(derefString(in.Name))
	description := strings.TrimSpace(derefString(in.Description))

	if fields := validateTaxonomyFields(name, description); len(fields) > 0 {
		return nil, contentFieldError(fields)
	}
	if err := ensureNameFree(tableSeries, name, 0); err != nil {
		return nil, err
	}
	slug, err := resolveTaxonomySlug(in.Slug, name, tableSeries, 0)
	if err != nil {
		return nil, err
	}

	series := &model.Series{
		Name:        name,
		Slug:        slug,
		Description: description,
		CreatedAt:   dbtime.Now(),
	}
	if err := global.DB.Transaction(func(tx *gorm.DB) error {
		if err := model.CreateSeries(tx, series); err != nil {
			return err
		}
		return ServiceGroupApp.Audit.WriteTx(tx, AuditInput{
			AdminID:    actor.AdminID,
			Action:     AuditActionSeriesCreate,
			TargetType: "series",
			TargetID:   strconv.FormatInt(series.ID, 10),
			Meta:       map[string]any{"name": series.Name, "slug": series.Slug},
			IP:         actor.IP,
		})
	}); err != nil {
		return nil, taxonomyWriteError(err, tableSeries, name, 0)
	}
	return series, nil
}

// UpdateSeries 局部更新系列；nil 字段保持不变，空 slug 按当前 name 重新生成。
func (s ContentService) UpdateSeries(id int64, in TaxonomyInput, actor PostActor) (*model.Series, error) {
	series, err := model.FindSeriesByID(global.DB, id)
	if err != nil {
		return nil, taxonomyNotFoundOr(err, "系列不存在")
	}

	fields := make(map[string]any, 3)
	changed := make([]string, 0, 3)
	name := series.Name

	if in.Name != nil {
		name = strings.TrimSpace(*in.Name)
		if errFields := validateTaxonomyFields(name, series.Description); len(errFields) > 0 {
			return nil, contentFieldError(errFields)
		}
		if name != series.Name {
			if err := ensureNameFree(tableSeries, name, id); err != nil {
				return nil, err
			}
			fields["name"] = name
			changed = append(changed, "name")
		}
	}
	if in.Description != nil {
		description := strings.TrimSpace(*in.Description)
		if errFields := validateTaxonomyFields(name, description); len(errFields) > 0 {
			return nil, contentFieldError(errFields)
		}
		if description != series.Description {
			fields["description"] = description
			changed = append(changed, "description")
		}
	}
	if in.Slug != nil {
		slug, err := resolveTaxonomySlug(in.Slug, name, tableSeries, id)
		if err != nil {
			return nil, err
		}
		if slug != series.Slug {
			fields["slug"] = slug
			changed = append(changed, "slug")
		}
	}

	if len(changed) == 0 {
		return series, nil
	}
	if err := global.DB.Transaction(func(tx *gorm.DB) error {
		if err := model.UpdateSeriesFields(tx, id, fields); err != nil {
			return err
		}
		return ServiceGroupApp.Audit.WriteTx(tx, AuditInput{
			AdminID:    actor.AdminID,
			Action:     AuditActionSeriesUpdate,
			TargetType: "series",
			TargetID:   strconv.FormatInt(id, 10),
			Meta:       map[string]any{"fields": changed},
			IP:         actor.IP,
		})
	}); err != nil {
		return nil, taxonomyWriteError(err, tableSeries, name, id)
	}
	return model.FindSeriesByID(global.DB, id)
}

// DeleteSeries 删除系列并把文章的 series_id 置空，不删文章（§5）。
func (s ContentService) DeleteSeries(id int64, actor PostActor) error {
	series, err := model.FindSeriesByID(global.DB, id)
	if err != nil {
		return taxonomyNotFoundOr(err, "系列不存在")
	}
	if err := model.DeleteSeries(global.DB, id); err != nil {
		return fmt.Errorf("删除系列失败: %w", err)
	}
	ServiceGroupApp.Audit.Write(AuditInput{
		AdminID:    actor.AdminID,
		Action:     AuditActionSeriesDelete,
		TargetType: "series",
		TargetID:   strconv.FormatInt(id, 10),
		Meta:       map[string]any{"name": series.Name, "slug": series.Slug},
		IP:         actor.IP,
	})
	return nil
}

// LinkInput 是友链的写入入参；nil 表示"不修改"（PUT 语义）。
type LinkInput struct {
	Name        *string
	URL         *string
	Avatar      *string
	Description *string
	Sort        *int
	Status      *string
}

// linkValues 是一条友链的有效字段值（入参与现值合成后的结果）。
type linkValues struct {
	Name        string
	URL         string
	Avatar      string
	Description string
	Status      string
	Sort        int
}

// Links 返回全部友链（含 hidden），按 sort、id 升序。
func (s ContentService) Links() ([]model.Link, error) {
	links, err := model.ListLinks(global.DB)
	if err != nil {
		return nil, fmt.Errorf("查询友链失败: %w", err)
	}
	return links, nil
}

// CreateLink 新建友链：name / url 必填，status 留空视为 active（§5）。
func (s ContentService) CreateLink(in LinkInput, actor PostActor) (*model.Link, error) {
	values := resolveLinkValues(in, model.Link{})
	if fields := validateLinkValues(values); len(fields) > 0 {
		return nil, contentFieldError(fields)
	}

	link := &model.Link{
		Name:        values.Name,
		URL:         values.URL,
		Avatar:      values.Avatar,
		Description: values.Description,
		Sort:        values.Sort,
		Status:      values.Status,
		CreatedAt:   dbtime.Now(),
	}
	if err := global.DB.Transaction(func(tx *gorm.DB) error {
		if err := model.CreateLink(tx, link); err != nil {
			return err
		}
		return ServiceGroupApp.Audit.WriteTx(tx, AuditInput{
			AdminID:    actor.AdminID,
			Action:     AuditActionLinkCreate,
			TargetType: "link",
			TargetID:   strconv.FormatInt(link.ID, 10),
			Meta:       map[string]any{"name": link.Name, "url": link.URL, "status": link.Status},
			IP:         actor.IP,
		})
	}); err != nil {
		return nil, fmt.Errorf("创建友链失败: %w", err)
	}
	return link, nil
}

// UpdateLink 局部更新友链；nil 字段保持不变。
func (s ContentService) UpdateLink(id int64, in LinkInput, actor PostActor) (*model.Link, error) {
	link, err := model.FindLinkByID(global.DB, id)
	if err != nil {
		if errors.Is(err, model.ErrNotFound) {
			return nil, errs.New(errs.ErrorNotFound, "友链不存在")
		}
		return nil, fmt.Errorf("查询友链失败: %w", err)
	}

	values := resolveLinkValues(in, *link)
	if fields := validateLinkValues(values); len(fields) > 0 {
		return nil, contentFieldError(fields)
	}

	fields := make(map[string]any, 6)
	changed := make([]string, 0, 6)
	appendIfChanged(fields, &changed, "name", link.Name, values.Name)
	appendIfChanged(fields, &changed, "url", link.URL, values.URL)
	appendIfChanged(fields, &changed, "avatar", link.Avatar, values.Avatar)
	appendIfChanged(fields, &changed, "description", link.Description, values.Description)
	appendIfChanged(fields, &changed, "sort", link.Sort, values.Sort)
	appendIfChanged(fields, &changed, "status", link.Status, values.Status)

	if len(changed) == 0 {
		return link, nil
	}
	if err := global.DB.Transaction(func(tx *gorm.DB) error {
		if err := model.UpdateLinkFields(tx, id, fields); err != nil {
			return err
		}
		return ServiceGroupApp.Audit.WriteTx(tx, AuditInput{
			AdminID:    actor.AdminID,
			Action:     AuditActionLinkUpdate,
			TargetType: "link",
			TargetID:   strconv.FormatInt(id, 10),
			Meta:       map[string]any{"fields": changed},
			IP:         actor.IP,
		})
	}); err != nil {
		return nil, fmt.Errorf("保存友链失败: %w", err)
	}
	return model.FindLinkByID(global.DB, id)
}

// DeleteLink 删除友链。
func (s ContentService) DeleteLink(id int64, actor PostActor) error {
	link, err := model.FindLinkByID(global.DB, id)
	if err != nil {
		if errors.Is(err, model.ErrNotFound) {
			return errs.New(errs.ErrorNotFound, "友链不存在")
		}
		return fmt.Errorf("查询友链失败: %w", err)
	}
	if err := model.DeleteLink(global.DB, id); err != nil {
		return fmt.Errorf("删除友链失败: %w", err)
	}
	ServiceGroupApp.Audit.Write(AuditInput{
		AdminID:    actor.AdminID,
		Action:     AuditActionLinkDelete,
		TargetType: "link",
		TargetID:   strconv.FormatInt(id, 10),
		Meta:       map[string]any{"name": link.Name, "url": link.URL},
		IP:         actor.IP,
	})
	return nil
}

// RedirectInput 是跳转的写入入参；nil 表示"不修改"（PUT 语义）。
type RedirectInput struct {
	FromPath *string
	ToPath   *string
	Status   *int
}

// redirectValues 是一条跳转的有效字段值。
type redirectValues struct {
	FromPath string
	ToPath   string
	Status   int
}

// Redirects 返回全部跳转，按 id 倒序。
func (s ContentService) Redirects() ([]model.Redirect, error) {
	items, err := model.ListRedirects(global.DB)
	if err != nil {
		return nil, fmt.Errorf("查询跳转失败: %w", err)
	}
	return items, nil
}

// CreateRedirect 新建跳转：fromPath 唯一且必须以 / 开头，status 留空视为 301（§5）。
func (s ContentService) CreateRedirect(in RedirectInput, actor PostActor) (*model.Redirect, error) {
	values := resolveRedirectValues(in, model.Redirect{})
	if fields := validateRedirectValues(values); len(fields) > 0 {
		return nil, contentFieldError(fields)
	}
	if err := ensureRedirectPathFree(values.FromPath, 0); err != nil {
		return nil, err
	}

	item := &model.Redirect{
		FromPath:  values.FromPath,
		ToPath:    values.ToPath,
		Status:    values.Status,
		CreatedAt: dbtime.Now(),
	}
	if err := global.DB.Transaction(func(tx *gorm.DB) error {
		if err := model.CreateRedirect(tx, item); err != nil {
			return err
		}
		return ServiceGroupApp.Audit.WriteTx(tx, AuditInput{
			AdminID:    actor.AdminID,
			Action:     AuditActionRedirectCreate,
			TargetType: "redirect",
			TargetID:   strconv.FormatInt(item.ID, 10),
			Meta:       map[string]any{"fromPath": item.FromPath, "toPath": item.ToPath, "status": item.Status},
			IP:         actor.IP,
		})
	}); err != nil {
		if model.IsUniqueViolation(err) {
			return nil, errs.New(errs.ErrorConflict, "fromPath 已存在")
		}
		return nil, fmt.Errorf("创建跳转失败: %w", err)
	}
	return item, nil
}

// UpdateRedirect 局部更新跳转；nil 字段保持不变。
func (s ContentService) UpdateRedirect(id int64, in RedirectInput, actor PostActor) (*model.Redirect, error) {
	item, err := model.FindRedirectByID(global.DB, id)
	if err != nil {
		if errors.Is(err, model.ErrNotFound) {
			return nil, errs.New(errs.ErrorNotFound, "跳转不存在")
		}
		return nil, fmt.Errorf("查询跳转失败: %w", err)
	}

	values := resolveRedirectValues(in, *item)
	if fields := validateRedirectValues(values); len(fields) > 0 {
		return nil, contentFieldError(fields)
	}
	if values.FromPath != item.FromPath {
		if err := ensureRedirectPathFree(values.FromPath, id); err != nil {
			return nil, err
		}
	}

	fields := make(map[string]any, 3)
	changed := make([]string, 0, 3)
	appendIfChanged(fields, &changed, "from_path", item.FromPath, values.FromPath)
	appendIfChanged(fields, &changed, "to_path", item.ToPath, values.ToPath)
	appendIfChanged(fields, &changed, "status", item.Status, values.Status)

	if len(changed) == 0 {
		return item, nil
	}
	if err := global.DB.Transaction(func(tx *gorm.DB) error {
		if err := model.UpdateRedirectFields(tx, id, fields); err != nil {
			return err
		}
		return ServiceGroupApp.Audit.WriteTx(tx, AuditInput{
			AdminID:    actor.AdminID,
			Action:     AuditActionRedirectUpdate,
			TargetType: "redirect",
			TargetID:   strconv.FormatInt(id, 10),
			Meta:       map[string]any{"fields": changed},
			IP:         actor.IP,
		})
	}); err != nil {
		if model.IsUniqueViolation(err) {
			return nil, errs.New(errs.ErrorConflict, "fromPath 已存在")
		}
		return nil, fmt.Errorf("保存跳转失败: %w", err)
	}
	return model.FindRedirectByID(global.DB, id)
}

// DeleteRedirect 删除跳转。
func (s ContentService) DeleteRedirect(id int64, actor PostActor) error {
	item, err := model.FindRedirectByID(global.DB, id)
	if err != nil {
		if errors.Is(err, model.ErrNotFound) {
			return errs.New(errs.ErrorNotFound, "跳转不存在")
		}
		return fmt.Errorf("查询跳转失败: %w", err)
	}
	if err := model.DeleteRedirect(global.DB, id); err != nil {
		return fmt.Errorf("删除跳转失败: %w", err)
	}
	ServiceGroupApp.Audit.Write(AuditInput{
		AdminID:    actor.AdminID,
		Action:     AuditActionRedirectDelete,
		TargetType: "redirect",
		TargetID:   strconv.FormatInt(id, 10),
		Meta:       map[string]any{"fromPath": item.FromPath, "toPath": item.ToPath},
		IP:         actor.IP,
	})
	return nil
}

// AttachmentInput 是附件的写入入参；nil 表示"不修改"（PUT 语义）。
type AttachmentInput struct {
	PostID   *int64
	Title    *string
	ShareURL *string
	FileName *string
	Bytes    *int64
	SHA256   *string
	Version  *string
	Sort     *int
}

// attachmentValues 是一条附件的有效字段值。
type attachmentValues struct {
	PostID   int64
	Title    string
	ShareURL string
	FileName string
	Bytes    int64
	SHA256   string
	Version  string
	Sort     int
}

// Attachments 返回附件分页；postID 为 0 表示不过滤，page / pageSize 由 api 层归一化后传入。
func (s ContentService) Attachments(postID int64, page, pageSize int) ([]model.PostAttachment, int64, error) {
	items, total, err := model.ListAttachments(global.DB, postID, pageSize, (page-1)*pageSize)
	if err != nil {
		return nil, 0, fmt.Errorf("查询附件失败: %w", err)
	}
	return items, total, nil
}

// CreateAttachment 新建附件：postId 必须指向未删除的文章，shareUrl 必须是 http(s)（§5）。
func (s ContentService) CreateAttachment(in AttachmentInput, actor PostActor) (*model.PostAttachment, error) {
	values := resolveAttachmentValues(in, model.PostAttachment{})
	if fields := validateAttachmentValues(values); len(fields) > 0 {
		return nil, contentFieldError(fields)
	}
	if err := ensurePostExists(values.PostID); err != nil {
		return nil, err
	}

	item := &model.PostAttachment{
		PostID:    values.PostID,
		Title:     values.Title,
		ShareURL:  values.ShareURL,
		FileName:  values.FileName,
		Bytes:     values.Bytes,
		SHA256:    values.SHA256,
		Version:   values.Version,
		Sort:      values.Sort,
		CreatedAt: dbtime.Now(),
	}
	if err := global.DB.Transaction(func(tx *gorm.DB) error {
		if err := model.CreateAttachment(tx, item); err != nil {
			return err
		}
		return ServiceGroupApp.Audit.WriteTx(tx, AuditInput{
			AdminID:    actor.AdminID,
			Action:     AuditActionAttachmentCreate,
			TargetType: "attachment",
			TargetID:   strconv.FormatInt(item.ID, 10),
			Meta:       map[string]any{"postId": item.PostID, "title": item.Title},
			IP:         actor.IP,
		})
	}); err != nil {
		return nil, fmt.Errorf("创建附件失败: %w", err)
	}
	return item, nil
}

// UpdateAttachment 局部更新附件；nil 字段保持不变。
func (s ContentService) UpdateAttachment(id int64, in AttachmentInput, actor PostActor) (*model.PostAttachment, error) {
	item, err := model.FindAttachmentByID(global.DB, id)
	if err != nil {
		return nil, attachmentNotFoundOr(err)
	}

	values := resolveAttachmentValues(in, *item)
	if fields := validateAttachmentValues(values); len(fields) > 0 {
		return nil, contentFieldError(fields)
	}
	if values.PostID != item.PostID {
		if err := ensurePostExists(values.PostID); err != nil {
			return nil, err
		}
	}

	fields := make(map[string]any, 8)
	changed := make([]string, 0, 8)
	appendIfChanged(fields, &changed, "post_id", item.PostID, values.PostID)
	appendIfChanged(fields, &changed, "title", item.Title, values.Title)
	appendIfChanged(fields, &changed, "share_url", item.ShareURL, values.ShareURL)
	appendIfChanged(fields, &changed, "file_name", item.FileName, values.FileName)
	appendIfChanged(fields, &changed, "bytes", item.Bytes, values.Bytes)
	appendIfChanged(fields, &changed, "sha256", item.SHA256, values.SHA256)
	appendIfChanged(fields, &changed, "version", item.Version, values.Version)
	appendIfChanged(fields, &changed, "sort", item.Sort, values.Sort)

	if len(changed) == 0 {
		return item, nil
	}
	if err := global.DB.Transaction(func(tx *gorm.DB) error {
		if err := model.UpdateAttachmentFields(tx, id, fields); err != nil {
			return err
		}
		return ServiceGroupApp.Audit.WriteTx(tx, AuditInput{
			AdminID:    actor.AdminID,
			Action:     AuditActionAttachmentUpdate,
			TargetType: "attachment",
			TargetID:   strconv.FormatInt(id, 10),
			Meta:       map[string]any{"fields": changed},
			IP:         actor.IP,
		})
	}); err != nil {
		return nil, fmt.Errorf("保存附件失败: %w", err)
	}
	return model.FindAttachmentByID(global.DB, id)
}

// DeleteAttachment 删除附件。
func (s ContentService) DeleteAttachment(id int64, actor PostActor) error {
	item, err := model.FindAttachmentByID(global.DB, id)
	if err != nil {
		return attachmentNotFoundOr(err)
	}
	if err := model.DeleteAttachment(global.DB, id); err != nil {
		return fmt.Errorf("删除附件失败: %w", err)
	}
	ServiceGroupApp.Audit.Write(AuditInput{
		AdminID:    actor.AdminID,
		Action:     AuditActionAttachmentDelete,
		TargetType: "attachment",
		TargetID:   strconv.FormatInt(id, 10),
		Meta:       map[string]any{"postId": item.PostID, "title": item.Title},
		IP:         actor.IP,
	})
	return nil
}

// AttachmentByID 返回单个附件（公开 /attachments/{id}/go 用）；不存在 → 404 / 1002。
func (s ContentService) AttachmentByID(id int64) (*model.PostAttachment, error) {
	item, err := model.FindAttachmentByID(global.DB, id)
	if err != nil {
		return nil, attachmentNotFoundOr(err)
	}
	return item, nil
}

// SettingsUpload 是上传目录与大小上限（只读回显，来自进程配置）。
type SettingsUpload struct {
	Dir      string `json:"dir"`
	MaxBytes int64  `json:"maxBytes"`
}

// SettingsTask 是调度器开关与周期（只读回显）。
type SettingsTask struct {
	Enabled         bool `json:"enabled"`
	IntervalSeconds int  `json:"intervalSeconds"`
}

// SettingsCloudreve 是图床归档配置（只读回显，口令永不返回，§9）。
type SettingsCloudreve struct {
	Enabled         bool   `json:"enabled"`
	WebDAVURL       string `json:"webdavUrl"`
	Username        string `json:"username"`
	BasePath        string `json:"basePath"`
	PublicBaseURL   string `json:"publicBaseUrl"`
	ArchiveVariants bool   `json:"archiveVariants"`
}

// SettingsDB 是数据库文件路径（只读回显）。
type SettingsDB struct {
	Path string `json:"path"`
}

// SettingsView 是 GET /api/admin/settings 的输出（§5）。
type SettingsView struct {
	ImgBaseURL     string            `json:"imgBaseUrl"`
	SiteTimezone   string            `json:"siteTimezone"`
	ContentVersion int64             `json:"contentVersion"`
	Upload         SettingsUpload    `json:"upload"`
	Task           SettingsTask      `json:"task"`
	Cloudreve      SettingsCloudreve `json:"cloudreve"`
	DB             SettingsDB        `json:"db"`
}

// SettingsUpdate 是设置更新入参；nil 表示不修改（§5：只允许改这两项）。
type SettingsUpdate struct {
	ImgBaseURL   *string
	SiteTimezone *string
}

// Settings 汇总当前可写项与只读回显；不含任何口令（§5 / §9）。
func (s ContentService) Settings() (SettingsView, error) {
	imgBaseURL, err := ServiceGroupApp.Setting.String(model.SettingImgBaseURL)
	if err != nil {
		return SettingsView{}, err
	}
	timezone, err := ServiceGroupApp.Setting.String(model.SettingSiteTimezone)
	if err != nil {
		return SettingsView{}, err
	}
	if timezone = strings.TrimSpace(timezone); timezone == "" {
		timezone = publicDefaultTimezone
	}
	version, err := ServiceGroupApp.Setting.Int64(model.SettingContentVersion, publicDefaultContentVersion)
	if err != nil {
		return SettingsView{}, err
	}

	return SettingsView{
		ImgBaseURL:     strings.TrimSpace(imgBaseURL),
		SiteTimezone:   timezone,
		ContentVersion: version,
		Upload: SettingsUpload{
			Dir:      global.Config.Upload.Dir,
			MaxBytes: global.Config.Upload.MaxBytes,
		},
		Task: SettingsTask{
			Enabled:         global.Config.Task.Enabled,
			IntervalSeconds: global.Config.Task.IntervalSeconds,
		},
		Cloudreve: SettingsCloudreve{
			Enabled:         global.Config.Cloudreve.Enabled,
			WebDAVURL:       global.Config.Cloudreve.WebDAVURL,
			Username:        global.Config.Cloudreve.Username,
			BasePath:        global.Config.Cloudreve.BasePath,
			PublicBaseURL:   global.Config.Cloudreve.PublicBaseURL,
			ArchiveVariants: global.Config.Cloudreve.ArchiveVariants,
		},
		DB: SettingsDB{Path: global.Config.DB.Path},
	}, nil
}

// settingWrite 是一次设置写入：settings 键、回显字段名与新值。
type settingWrite struct {
	key   string
	field string
	value string
}

// UpdateSettings 写入 imgBaseUrl / siteTimezone 并递增 content_version（§5）。
//
// 递增是硬要求：公开读接口的 ETag 依赖 content_version，不递增会让前端一直命中旧缓存。
func (s ContentService) UpdateSettings(in SettingsUpdate, actor PostActor) (SettingsView, error) {
	writes := make([]settingWrite, 0, 2)

	if in.ImgBaseURL != nil {
		value := strings.TrimSpace(*in.ImgBaseURL)
		if err := validateImgBaseURL(value); err != nil {
			return SettingsView{}, err
		}
		writes = append(writes, settingWrite{key: model.SettingImgBaseURL, field: "imgBaseUrl", value: value})
	}
	if in.SiteTimezone != nil {
		value := strings.TrimSpace(*in.SiteTimezone)
		if _, err := time.LoadLocation(value); err != nil {
			return SettingsView{}, errs.New(errs.ErrorValidation, "siteTimezone 不是合法的 IANA 时区名")
		}
		writes = append(writes, settingWrite{key: model.SettingSiteTimezone, field: "siteTimezone", value: value})
	}

	// 空请求体不改任何东西，也不动 content_version（没有内容变化就不该让前端缓存失效）。
	if len(writes) == 0 {
		return s.Settings()
	}

	meta := make(map[string]any, len(writes))
	for _, write := range writes {
		if err := ServiceGroupApp.Setting.PutString(write.key, write.value); err != nil {
			return SettingsView{}, err
		}
		meta[write.field] = write.value
	}
	if err := ServiceGroupApp.Setting.BumpContentVersion(); err != nil {
		return SettingsView{}, err
	}

	ServiceGroupApp.Audit.Write(AuditInput{
		AdminID:    actor.AdminID,
		Action:     AuditActionSettingsUpdate,
		TargetType: "settings",
		TargetID:   settingsTargetID,
		Meta:       meta,
		IP:         actor.IP,
	})
	return s.Settings()
}

// validateTaxonomyFields 校验分类的 name 与 description（§5：name 1–200、description ≤500）。
func validateTaxonomyFields(name, description string) []request.FieldError {
	fields := make([]request.FieldError, 0, 2)
	if n := utf8.RuneCountInString(name); n == 0 || n > taxonomyNameMaxLen {
		fields = append(fields, request.FieldError{Field: "name", Message: "名称长度必须在 1–200 个字符之间"})
	}
	if utf8.RuneCountInString(description) > taxonomyDescriptionMaxLen {
		fields = append(fields, request.FieldError{Field: "description", Message: "描述不能超过 500 个字符"})
	}
	return fields
}

// ensureNameFree 校验 name 在同表内未被占用（excludeID 用于更新时排除自身，§5）。
func ensureNameFree(table, name string, excludeID int64) error {
	exists, err := model.NameExistsIn(global.DB, table, name, excludeID)
	if err != nil {
		return fmt.Errorf("检查名称失败: %w", err)
	}
	if exists {
		return errs.New(errs.ErrorConflict, "名称已存在")
	}
	return nil
}

// resolveTaxonomySlug 决定写入用的 slug：手填优先，留空按 name 生成（§5）。
//
// 中文名经 slugify 会得到空串，此时要求手填，否则同一批中文分类会全部撞同一个 slug。
func resolveTaxonomySlug(in *string, name, table string, excludeID int64) (string, error) {
	slug := strings.TrimSpace(derefString(in))
	if slug == "" {
		slug = slugify(name)
		if slug == "" {
			return "", errs.New(errs.ErrorValidation, "slug 不能为空（中文名请手填）")
		}
	}
	if err := validatePostSlug(slug); err != nil {
		return "", err
	}

	exists, err := model.SlugExistsIn(global.DB, table, slug, excludeID)
	if err != nil {
		return "", fmt.Errorf("检查 slug 失败: %w", err)
	}
	if exists {
		return "", errs.New(errs.ErrorSlugConflict, "slug 已存在，请更换")
	}
	return slug, nil
}

// taxonomyWriteError 把分类写入失败翻成契约错误（§5）。
//
// 预检查挡不住并发写入，唯一索引冲突时回查一次 name，决定是 1004（name 撞车）还是 2001（slug 撞车）。
func taxonomyWriteError(err error, table, name string, excludeID int64) error {
	if !model.IsUniqueViolation(err) {
		return fmt.Errorf("保存分类失败: %w", err)
	}
	exists, checkErr := model.NameExistsIn(global.DB, table, name, excludeID)
	if checkErr == nil && exists {
		return errs.New(errs.ErrorConflict, "名称已存在")
	}
	return errs.New(errs.ErrorSlugConflict, "slug 已存在，请更换")
}

// taxonomyNotFoundOr 把 model.ErrNotFound 翻成 404 / 1002，其余错误原样包装。
func taxonomyNotFoundOr(err error, message string) error {
	if errors.Is(err, model.ErrNotFound) {
		return errs.New(errs.ErrorNotFound, message)
	}
	return fmt.Errorf("查询分类失败: %w", err)
}

// resolveLinkValues 把友链入参与现值合成有效值；status 留空视为 active（§5）。
func resolveLinkValues(in LinkInput, current model.Link) linkValues {
	values := linkValues{
		Name:        current.Name,
		URL:         current.URL,
		Avatar:      current.Avatar,
		Description: current.Description,
		Status:      current.Status,
		Sort:        current.Sort,
	}
	if in.Name != nil {
		values.Name = strings.TrimSpace(*in.Name)
	}
	if in.URL != nil {
		values.URL = strings.TrimSpace(*in.URL)
	}
	if in.Avatar != nil {
		values.Avatar = strings.TrimSpace(*in.Avatar)
	}
	if in.Description != nil {
		values.Description = strings.TrimSpace(*in.Description)
	}
	if in.Sort != nil {
		values.Sort = *in.Sort
	}
	if in.Status != nil {
		values.Status = strings.TrimSpace(*in.Status)
	}
	if values.Status == "" {
		values.Status = model.LinkStatusActive
	}
	return values
}

// validateLinkValues 校验友链的有效值（§5）。
func validateLinkValues(values linkValues) []request.FieldError {
	fields := make([]request.FieldError, 0, 5)

	if n := utf8.RuneCountInString(values.Name); n == 0 || n > linkNameMaxLen {
		fields = append(fields, request.FieldError{Field: "name", Message: "名称长度必须在 1–100 个字符之间"})
	}
	switch {
	case values.URL == "":
		fields = append(fields, request.FieldError{Field: "url", Message: "不能为空"})
	case utf8.RuneCountInString(values.URL) > linkURLMaxLen:
		fields = append(fields, request.FieldError{Field: "url", Message: "不能超过 500 个字符"})
	case !isHTTPURL(values.URL):
		fields = append(fields, request.FieldError{Field: "url", Message: "必须以 http:// 或 https:// 开头"})
	}
	switch {
	case values.Avatar == "":
	case utf8.RuneCountInString(values.Avatar) > linkAvatarMaxLen:
		fields = append(fields, request.FieldError{Field: "avatar", Message: "不能超过 500 个字符"})
	case !isHTTPURL(values.Avatar):
		fields = append(fields, request.FieldError{Field: "avatar", Message: "必须以 http:// 或 https:// 开头"})
	}
	if utf8.RuneCountInString(values.Description) > linkDescriptionMaxLen {
		fields = append(fields, request.FieldError{Field: "description", Message: "不能超过 200 个字符"})
	}
	if values.Sort < 0 || values.Sort > linkSortMax {
		fields = append(fields, request.FieldError{Field: "sort", Message: "必须在 0–99999 之间"})
	}
	switch values.Status {
	case model.LinkStatusActive, model.LinkStatusHidden:
	default:
		fields = append(fields, request.FieldError{Field: "status", Message: "只接受 active 或 hidden"})
	}
	return fields
}

// resolveRedirectValues 把跳转入参与现值合成有效值；status 留空视为 301（§5）。
func resolveRedirectValues(in RedirectInput, current model.Redirect) redirectValues {
	values := redirectValues{
		FromPath: current.FromPath,
		ToPath:   current.ToPath,
		Status:   current.Status,
	}
	if in.FromPath != nil {
		values.FromPath = strings.TrimSpace(*in.FromPath)
	}
	if in.ToPath != nil {
		values.ToPath = strings.TrimSpace(*in.ToPath)
	}
	if in.Status != nil {
		values.Status = *in.Status
	}
	if values.Status == 0 {
		values.Status = redirectStatusMoved
	}
	return values
}

// validateRedirectValues 校验跳转的有效值（§5）。
func validateRedirectValues(values redirectValues) []request.FieldError {
	fields := make([]request.FieldError, 0, 3)

	switch {
	case values.FromPath == "":
		fields = append(fields, request.FieldError{Field: "fromPath", Message: "不能为空"})
	case !strings.HasPrefix(values.FromPath, "/"):
		fields = append(fields, request.FieldError{Field: "fromPath", Message: "必须以 / 开头"})
	case utf8.RuneCountInString(values.FromPath) > redirectFromPathMaxLen:
		fields = append(fields, request.FieldError{Field: "fromPath", Message: "不能超过 200 个字符"})
	}
	switch {
	case values.ToPath == "":
		fields = append(fields, request.FieldError{Field: "toPath", Message: "不能为空"})
	case utf8.RuneCountInString(values.ToPath) > redirectToPathMaxLen:
		fields = append(fields, request.FieldError{Field: "toPath", Message: "不能超过 500 个字符"})
	case !strings.HasPrefix(values.ToPath, "/") && !isHTTPURL(values.ToPath):
		fields = append(fields, request.FieldError{Field: "toPath", Message: "必须以 / 或 http(s):// 开头"})
	}
	switch values.Status {
	case redirectStatusMoved, redirectStatusFound:
	default:
		fields = append(fields, request.FieldError{Field: "status", Message: "只接受 301 或 302"})
	}
	return fields
}

// ensureRedirectPathFree 校验 fromPath 未被别的记录占用（§5：from_path 唯一）。
func ensureRedirectPathFree(fromPath string, excludeID int64) error {
	existing, err := model.FindRedirectByPath(global.DB, fromPath)
	if err != nil {
		if errors.Is(err, model.ErrNotFound) {
			return nil
		}
		return fmt.Errorf("检查 fromPath 失败: %w", err)
	}
	if existing.ID != excludeID {
		return errs.New(errs.ErrorConflict, "fromPath 已存在")
	}
	return nil
}

// resolveAttachmentValues 把附件入参与现值合成有效值。
func resolveAttachmentValues(in AttachmentInput, current model.PostAttachment) attachmentValues {
	values := attachmentValues{
		PostID:   current.PostID,
		Title:    current.Title,
		ShareURL: current.ShareURL,
		FileName: current.FileName,
		Bytes:    current.Bytes,
		SHA256:   current.SHA256,
		Version:  current.Version,
		Sort:     current.Sort,
	}
	if in.PostID != nil {
		values.PostID = *in.PostID
	}
	if in.Title != nil {
		values.Title = strings.TrimSpace(*in.Title)
	}
	if in.ShareURL != nil {
		values.ShareURL = strings.TrimSpace(*in.ShareURL)
	}
	if in.FileName != nil {
		values.FileName = strings.TrimSpace(*in.FileName)
	}
	if in.Bytes != nil {
		values.Bytes = *in.Bytes
	}
	if in.SHA256 != nil {
		values.SHA256 = strings.TrimSpace(*in.SHA256)
	}
	if in.Version != nil {
		values.Version = strings.TrimSpace(*in.Version)
	}
	if in.Sort != nil {
		values.Sort = *in.Sort
	}
	return values
}

// validateAttachmentValues 校验附件的有效值（§5）。
func validateAttachmentValues(values attachmentValues) []request.FieldError {
	fields := make([]request.FieldError, 0, 6)

	if values.PostID <= 0 {
		fields = append(fields, request.FieldError{Field: "postId", Message: "必须是正整数"})
	}
	if n := utf8.RuneCountInString(values.Title); n == 0 || n > attachmentTitleMaxLen {
		fields = append(fields, request.FieldError{Field: "title", Message: "标题长度必须在 1–200 个字符之间"})
	}
	switch {
	case values.ShareURL == "":
		fields = append(fields, request.FieldError{Field: "shareUrl", Message: "不能为空"})
	case utf8.RuneCountInString(values.ShareURL) > attachmentShareURLMaxLen:
		fields = append(fields, request.FieldError{Field: "shareUrl", Message: "不能超过 500 个字符"})
	case !isHTTPURL(values.ShareURL):
		fields = append(fields, request.FieldError{Field: "shareUrl", Message: "必须以 http:// 或 https:// 开头"})
	}
	if utf8.RuneCountInString(values.FileName) > attachmentFileNameMaxLen {
		fields = append(fields, request.FieldError{Field: "fileName", Message: "不能超过 200 个字符"})
	}
	if values.Bytes < 0 {
		fields = append(fields, request.FieldError{Field: "bytes", Message: "不能为负数"})
	}
	if utf8.RuneCountInString(values.Version) > attachmentVersionMaxLen {
		fields = append(fields, request.FieldError{Field: "version", Message: "不能超过 50 个字符"})
	}
	if values.Sort < 0 || values.Sort > attachmentSortMax {
		fields = append(fields, request.FieldError{Field: "sort", Message: "必须在 0–99999 之间"})
	}
	return fields
}

// ensurePostExists 校验附件挂载的文章存在且未删除（§5）。
func ensurePostExists(postID int64) error {
	if _, err := model.FindPostByID(global.DB, postID, false); err != nil {
		if errors.Is(err, model.ErrNotFound) {
			return errs.New(errs.ErrorNotFound, "文章不存在")
		}
		return fmt.Errorf("查询文章失败: %w", err)
	}
	return nil
}

// attachmentNotFoundOr 把 model.ErrNotFound 翻成 404 / 1002，其余错误原样包装。
func attachmentNotFoundOr(err error) error {
	if errors.Is(err, model.ErrNotFound) {
		return errs.New(errs.ErrorNotFound, "附件不存在")
	}
	return fmt.Errorf("查询附件失败: %w", err)
}

// validateImgBaseURL 校验 img_base_url：留空表示用同源相对路径，填了必须是 http(s) 且不带结尾斜杠（§5）。
func validateImgBaseURL(value string) error {
	if value == "" {
		return nil
	}
	if !isHTTPURL(value) || strings.HasSuffix(value, "/") || utf8.RuneCountInString(value) > settingsImgBaseURLMaxLen {
		return errs.New(errs.ErrorValidation, "imgBaseUrl 必须以 http:// 或 https:// 开头、不以 / 结尾且不超过 200 个字符")
	}
	return nil
}

// isHTTPURL 判断字符串是否以 http:// 或 https:// 开头（§5 对 url / avatar / shareUrl 的约定）。
func isHTTPURL(raw string) bool {
	return strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "https://")
}

// derefString 取指针字段的值；nil（"没传"）返回空串。
func derefString(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

// appendIfChanged 把有变化的字段写进更新映射，并记下字段名（审计与日志用）。
func appendIfChanged[T comparable](fields map[string]any, changed *[]string, column string, current, next T) {
	if current == next {
		return
	}
	fields[column] = next
	*changed = append(*changed, column)
}

// contentFieldError 把字段错误数组包成 1003；data 形状与 request.FieldError 一致（§5）。
func contentFieldError(fields []request.FieldError) error {
	return errs.WithDetails(errs.ErrorValidation, "参数校验失败", fields)
}
