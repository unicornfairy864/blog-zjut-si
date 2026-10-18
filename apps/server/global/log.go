package global

import "log/slog"

// Log 是全进程唯一的结构化日志句柄。
var Log *slog.Logger
