package middleware

import (
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/unicornfairy864/blog-zjut-si/apps/server/utils"
)

// VisitorHeader 是前端携带访客标识的请求头（agent.md §5）。
//
// 公开接口没有用户系统，也不能在公开 HTML / 公开读接口上 Set-Cookie（§17.1），
// 所以访客标识由前端在 localStorage 生成一次，随写请求带上。
const VisitorHeader = "X-Visitor-Id"

const (
	// visitorIDMinLen / visitorIDMaxLen 限定请求头长度，避免任意长串进限流桶。
	visitorIDMinLen = 8
	visitorIDMaxLen = 64
)

// VisitorSeed 返回访客标识种子：优先 `X-Visitor-Id` 头，缺失或非法时退回 `ip|ua`。
func VisitorSeed(c *gin.Context) string {
	if id := strings.TrimSpace(c.GetHeader(VisitorHeader)); validVisitorID(id) {
		return "id|" + id
	}
	return "ip|" + c.ClientIP() + "|" + c.Request.UserAgent()
}

// VisitorKey 返回加盐后的 visitor_hash；限流与落库都用它，进程内不留原始标识（§9）。
func VisitorKey(c *gin.Context) string { return utils.HashVisitor(VisitorSeed(c)) }

// validVisitorID 只接受 `[A-Za-z0-9_-]{8,64}`，其余一律当没带。
func validVisitorID(id string) bool {
	if len(id) < visitorIDMinLen || len(id) > visitorIDMaxLen {
		return false
	}
	for i := 0; i < len(id); i++ {
		switch ch := id[i]; {
		case ch >= 'a' && ch <= 'z',
			ch >= 'A' && ch <= 'Z',
			ch >= '0' && ch <= '9',
			ch == '-', ch == '_':
		default:
			return false
		}
	}
	return true
}
