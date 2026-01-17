package app

import (
	"log/slog"
	"time"
)

type Config struct {
	Interval    time.Duration
	Cleanup     bool
	LabelEnable bool
	Label       string

	DockerConfigPath string // путь до config.json для registry auth (опционально)

	RollingLabel string

	LogLevel slog.Level

	Notify NotifyFunc
}
