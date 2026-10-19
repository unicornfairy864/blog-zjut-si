// Package public 是公开端（8640）接口层。
package public

// ApiGroup 聚合公开接口。
type ApiGroup struct {
	Archive     ArchiveApi
	Attachment  AttachmentApi
	Comment     CommentApi
	Interaction InteractionApi
	Link        LinkApi
	Page        PageApi
	Post        PostApi
	Preview     PreviewApi
	Series      SeriesApi
	Site        SiteApi
	Tag         TagApi
	SystemApi
}

// Group 是公开接口聚合实例。
var Group = new(ApiGroup)
