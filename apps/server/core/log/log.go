// Package log 统一构造进程日志。
//
// 契约（agent.md §7）：slog JSON 输出到 stdout，字段
// ts / level / msg / request_id / method / path / status / dur_ms / ip_hash；
// 禁止打印密码、邮箱、cookie。
package log

import (
	"io"
	"log/slog"
	"strings"
)

// New 构造 JSON 格式的 slog.Logger，时间字段名为 ts。
func New(level string, w io.Writer) *slog.Logger {
	opts := &slog.HandlerOptions{
		Level: ParseLevel(level),
		ReplaceAttr: func(_ []string, a slog.Attr) slog.Attr {
			switch a.Key {
			case slog.TimeKey:
				a.Key = "ts"
			case slog.MessageKey:
				a.Key = "msg"
			}
			return a
		},
	}
	return slog.New(slog.NewJSONHandler(w, opts))
}

// ParseLevel 把配置里的字符串映射为 slog 级别，未知值回落到 info。
func ParseLevel(s string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
