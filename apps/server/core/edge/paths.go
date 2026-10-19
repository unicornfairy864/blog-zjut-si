package edge

import "strings"

// IsPrivatePath 报告该路径是否必须从 8640 隐藏（C4/C5）。
//
// 覆盖：管理页 /admin、管理 API /api/admin、静态图片 /uploads（nginx 直出）、
// 内部 API /api/internal、健康检查。
func IsPrivatePath(path string) bool {
	switch path {
	case "/healthz", "/readyz":
		return true
	}
	return hasPathPrefix(path, "/admin") ||
		hasPathPrefix(path, "/api/admin") ||
		hasPathPrefix(path, "/api/internal") ||
		hasPathPrefix(path, "/uploads")
}

// IsAPIPath 报告该路径是否属于 API 命名空间（8640 上未匹配的 /api/* 必须 404，不能落到 Next）。
func IsAPIPath(path string) bool {
	return hasPathPrefix(path, "/api")
}

// IsAdminPagePath 报告 8641 上该路径是否应转发给 Next。
//
// 登录页在 /admin/login（app/(admin)/admin/login），因此 /admin* 一并放行；
// /preview/* 也放行：作者在隧道里（http://localhost:8641）点开预览链接就能直接看，
// 不必把域名换成公网站点（§5 预览链接）。
func IsAdminPagePath(path string) bool {
	switch path {
	case "/favicon.ico", "/robots.txt":
		return true
	}
	return hasPathPrefix(path, "/admin") ||
		hasPathPrefix(path, "/preview") ||
		strings.HasPrefix(path, "/_next/")
}

// hasPathPrefix 按路径段比较，避免 /administrator 误命中 /admin。
func hasPathPrefix(path, prefix string) bool {
	if path == prefix {
		return true
	}
	return strings.HasPrefix(path, prefix+"/")
}
