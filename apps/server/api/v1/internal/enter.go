// Package internal 是内部接口层（仅 8641，容器内或 SSH 隧道可达）。
package internal

// ApiGroup 聚合内部接口。
type ApiGroup struct {
	HealthApi
}

// Group 是内部接口聚合实例。
var Group = new(ApiGroup)
