package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/moby/moby/client"

	"github.com/devem-tech/up-to-date/internal/app"
	"github.com/devem-tech/up-to-date/internal/dockerauth"
)

const appVersion = "0.5.0"

func main() {
	var cfg app.Config
	var logLevelStr string
	fs := flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	printUsage := func() {
		fmt.Fprintf(os.Stderr, "Usage of %s:\n", os.Args[0])
		fs.SetOutput(os.Stderr)
		fs.PrintDefaults()
		fs.SetOutput(io.Discard)
	}
	fs.Usage = func() {}
	usageError := func(format string, args ...any) {
		fmt.Fprintf(os.Stderr, "❌ error: "+format+"\n", args...)
		fmt.Fprintln(os.Stderr)
		printUsage()
		os.Exit(2)
	}

	fs.DurationVar(&cfg.Interval, "interval", 30*time.Second, "Check interval (e.g. 30s)")
	fs.BoolVar(&cfg.Cleanup, "cleanup", false, "Remove old images for updated containers")
	fs.BoolVar(&cfg.LabelEnable, "label-enable", false, "Update only containers that have label")
	fs.StringVar(&cfg.Label, "label", "devem.tech/up-to-date.enabled=true", "Label selector for --label-enable (key or key=value)")

	fs.StringVar(&cfg.DockerConfigPath, "docker-config", "", "Path to docker config.json for registry auth (optional)")

	fs.StringVar(&cfg.RollingLabel, "rolling-label", "devem.tech/up-to-date.rolling=true", "Label selector to enable rolling updates (key or key=value)")
	fs.StringVar(&logLevelStr, "log-level", "info", "Log level: debug, info, warn, error")
	if err := fs.Parse(os.Args[1:]); err != nil {
		if err == flag.ErrHelp {
			printUsage()
			os.Exit(0)
		}
		usageError("%v", err)
	}

	if cfg.Interval <= 0 {
		usageError("interval must be positive")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	parsedLogLevel, err := app.ParseLogLevel(logLevelStr)
	if err != nil {
		usageError("log level: %v", err)
	}
	cfg.LogLevel = parsedLogLevel
	app.SetupLogging(cfg.LogLevel)

	cli, err := client.New(client.FromEnv)
	if err != nil {
		slog.Error("docker client", "error", err)
		os.Exit(1)
	}
	defer cli.Close()

	var auths dockerauth.Index
	if cfg.DockerConfigPath != "" {
		auths, err = dockerauth.Load(cfg.DockerConfigPath)
		if err != nil {
			usageError("docker-config: %v", err)
		}
	}

	slog.Info("up-to-date " + appVersion)
	slog.Info("--log-level=" + strings.ToLower(cfg.LogLevel.String()))
	slog.Info("--interval=" + cfg.Interval.String())
	slog.Info("--cleanup=" + fmt.Sprintf("%t", cfg.Cleanup))
	slog.Info("--label-enable=" + fmt.Sprintf("%t", cfg.LabelEnable))

	if notify, err := app.NewTelegramNotifierFromEnv(); err != nil {
		slog.Warn("telegram notifications disabled", "error", err)
	} else if notify != nil {
		cfg.Notify = notify
		slog.Info("telegram notifications enabled")
	}

	app.Run(ctx, cli, auths, cfg)
}
