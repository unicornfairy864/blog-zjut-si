// Package service 承载全部业务规则；事务由 service 发起，tx 以参数透传。
//
// 硬规则（agent.md §3.2）：service 不接收 *gin.Context，不直接拼 HTTP 响应。
package service

// ServiceGroup 聚合所有业务服务，跨包只走这里。
type ServiceGroup struct {
	Auth    AuthService
	Audit   AuditService
	Comment CommentService
	Content ContentService
	Health  HealthService
	Media   MediaService
	Ops     OpsService
	Post    PostService
	Public  PublicService
	Setting SettingService
	Stats   StatsService
	Task    TaskService
}

// ServiceGroupApp 是全局服务聚合实例。
var ServiceGroupApp = new(ServiceGroup)
