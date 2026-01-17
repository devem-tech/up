package app

import (
	"log/slog"
	"time"
)

type Config struct {
	Interval    time.Duration
	Cleanup     bool
	LabelEnable bool
	LabelKey    string
	LabelValue  string

	DockerConfigPath string // путь до config.json для registry auth (опционально)

	RollingLabelKey   string
	RollingLabelValue string

	LogLevel slog.Level
}
