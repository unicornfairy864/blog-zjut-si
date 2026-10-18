// Package v1 聚合三个边缘的接口层；api 只做绑定 → 校验 → 调 service → 输出。
package v1

import (
	"github.com/unicornfairy864/blog-zjut-si/apps/server/api/v1/admin"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/api/v1/internal"
	"github.com/unicornfairy864/blog-zjut-si/apps/server/api/v1/public"
)

// ApiGroup 聚合 public / admin / internal 三个边缘。
type ApiGroup struct {
	Public   *public.ApiGroup
	Admin    *admin.ApiGroup
	Internal *internal.ApiGroup
}

// ApiGroupApp 是全局接口聚合实例。
var ApiGroupApp = &ApiGroup{
	Public:   public.Group,
	Admin:    admin.Group,
	Internal: internal.Group,
}
