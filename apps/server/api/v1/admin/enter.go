// Package admin 是管理端（8641）接口层。
package admin

// ApiGroup 聚合管理端接口。
type ApiGroup struct {
	Attachment AttachmentApi
	Audit      AuditApi
	AuthApi
	Comment CommentApi
	Content ContentApi
	Job     JobApi
	Media   MediaApi
	Post    PostApi
	Setting SettingApi
	Stats   StatsApi
}

// Group 是管理端接口聚合实例。
var Group = new(ApiGroup)
