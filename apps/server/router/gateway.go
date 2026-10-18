package router

import (
	"net/http"
	"strings"
)

// NewAdminGateway 把 8641 的同一端口分给内部边缘与管理边缘。
//
// §3.2.7 要求三个 engine 各自成文件，§2 又要求 /api/internal/* 与健康检查
// 挂在 8641；这里做一次纯路径分发，两个 engine 互不感知。
func NewAdminGateway(admin, internal http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isInternalPath(r.URL.Path) {
			internal.ServeHTTP(w, r)
			return
		}
		admin.ServeHTTP(w, r)
	})
}

// isInternalPath 报告该路径属于内部边缘。
func isInternalPath(path string) bool {
	switch path {
	case "/healthz", "/readyz":
		return true
	}
	return strings.HasPrefix(path, "/api/internal/")
}
