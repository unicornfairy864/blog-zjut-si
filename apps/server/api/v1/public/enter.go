// Package public 是公开端（8640）接口层。
package public

// ApiGroup 聚合公开接口。
type ApiGroup struct {
	Post PostApi
	SystemApi
}

// Group 是公开接口聚合实例。
var Group = new(ApiGroup)
