package app

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"
)

var logCtx = context.Background()

func ParseLogLevel(s string) (slog.Level, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return slog.LevelDebug, nil
	case "info":
		return slog.LevelInfo, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return slog.LevelInfo, fmt.Errorf("unknown log level %q", s)
	}
}

func SetupLogging(level slog.Level) {
	handler := slog.NewTextHandler(
		os.Stdout,
		&slog.HandlerOptions{
			Level: level,
			ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
				if a.Key == slog.TimeKey {
					if t, ok := a.Value.Any().(time.Time); ok {
						return slog.String(slog.TimeKey, t.UTC().Format(time.RFC3339))
					}
				}
				if a.Key == slog.LevelKey {
					if lvl, ok := a.Value.Any().(slog.Level); ok {
						return slog.String(slog.LevelKey, strings.ToLower(lvl.String()))
					}
				}
				return a
			},
		},
	)
	slog.SetDefault(slog.New(handler))
}

func logf(lvl slog.Level, format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	slog.Default().Log(logCtx, lvl, msg)
}

func logContainerf(lvl slog.Level, ref containerRef, format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	attrs := []slog.Attr{slog.String("name", ref.Name)}
	if ref.ID == "" {
		slog.Default().LogAttrs(logCtx, lvl, msg, attrs...)
		return
	}
	attrs = append(attrs, slog.String("id", ref.ID))
	slog.Default().LogAttrs(logCtx, lvl, msg, attrs...)
}
