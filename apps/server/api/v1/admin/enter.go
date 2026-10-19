// Package admin 是管理端（8641）接口层。
package admin

// ApiGroup 聚合管理端接口。
type ApiGroup struct {
	AuthApi
	Comment CommentApi
	Media   MediaApi
	Post    PostApi
	Stats   StatsApi
}

// Group 是管理端接口聚合实例。
var Group = new(ApiGroup)
