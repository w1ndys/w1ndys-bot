// Package logger 构建应用的结构化日志记录器。
package logger

import (
	"log/slog"
	"os"
)

// New 返回写入 stdout 的 slog.Logger。level 与 format 预期已由 config.Load
// 校验；传入未知值时分别回退到 info 与 json。
func New(level, format string) *slog.Logger {
	opts := &slog.HandlerOptions{Level: levelFrom(level)}

	var handler slog.Handler
	if format == "text" {
		handler = slog.NewTextHandler(os.Stdout, opts)
	} else {
		handler = slog.NewJSONHandler(os.Stdout, opts)
	}
	return slog.New(handler)
}

func levelFrom(level string) slog.Level {
	switch level {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
