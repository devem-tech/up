package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/devem-tech/up-to-date/internal/app"
	"github.com/devem-tech/up-to-date/internal/dockerauth"
	"github.com/moby/moby/client"
)

const appVersion = "0.1.0"

func main() {
	var cfg app.Config
	var logLevelStr string

	flag.DurationVar(&cfg.Interval, "interval", 30*time.Second, "Check interval (e.g. 30s)")
	flag.BoolVar(&cfg.Cleanup, "cleanup", false, "Remove old images for updated containers")
	flag.BoolVar(&cfg.LabelEnable, "label-enable", false, "Update only containers that have label key=value")

	flag.StringVar(&cfg.LabelKey, "label-key", "devem.tech/up-to-date.enabled", "Label key to match (used with --label-enable)")
	flag.StringVar(&cfg.LabelValue, "label-value", "true", "Label value to match (used with --label-enable)")

	flag.StringVar(&cfg.DockerConfigPath, "docker-config", "/config.json", "Path to docker config.json for registry auth (optional)")

	flag.StringVar(&cfg.RollingLabelKey, "rolling-label-key", "devem.tech/up-to-date.rolling", "Label key to enable rolling updates for a container")
	flag.StringVar(&cfg.RollingLabelValue, "rolling-label-value", "true", "Label value to enable rolling updates (used with --rolling-label-key)")
	flag.StringVar(&logLevelStr, "log-level", "info", "Log level: debug, info, warn, error")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	parsedLogLevel, err := app.ParseLogLevel(logLevelStr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "log level: %v\n", err)
		os.Exit(2)
	}
	cfg.LogLevel = parsedLogLevel
	app.SetupLogging(cfg.LogLevel)

	cli, err := client.New(client.FromEnv)
	if err != nil {
		slog.Error("docker client", "error", err)
		os.Exit(1)
	}
	defer cli.Close()

	auths, err := dockerauth.Load(cfg.DockerConfigPath)
	if err != nil {
		slog.Warn("config", "error", err, "note", "continuing without registry auth")
	}

	slog.Info("up-to-date " + appVersion)
	slog.Info("--log-level=" + strings.ToLower(cfg.LogLevel.String()))
	slog.Info("--interval=" + cfg.Interval.String())
	slog.Info("--cleanup=" + fmt.Sprintf("%t", cfg.Cleanup))
	slog.Info("--label-enable=" + fmt.Sprintf("%t", cfg.LabelEnable))

	app.Run(ctx, cli, auths, cfg)
}
