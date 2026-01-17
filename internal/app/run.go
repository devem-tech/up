package app

import (
	"context"
	"log/slog"
	"time"

	"github.com/devem-tech/up-to-date/internal/dockerauth"
	"github.com/moby/moby/client"
)

func Run(ctx context.Context, cli *client.Client, auths dockerauth.Index, cfg Config) {
	runOnce(ctx, cli, auths, cfg)

	t := time.NewTicker(cfg.Interval)
	defer t.Stop()

	for {
		select {
		case <-ctx.Done():
			logf(slog.LevelInfo, "shutdown")
			return
		case <-t.C:
			runOnce(ctx, cli, auths, cfg)
		}
	}
}

func runOnce(ctx context.Context, cli *client.Client, auths dockerauth.Index, cfg Config) {
	start := time.Now()
	containers, err := listTargetContainers(ctx, cli, cfg)
	if err != nil {
		logf(slog.LevelError, "list containers error: %v", err)
		return
	}
	scanned := len(containers)
	updated := 0
	failed := 0

	logf(slog.LevelDebug, "scan: %d container(s) eligible", scanned)

	for _, c := range containers {
		ref := containerRefFromSummary(c)
		wasUpdated, err := updateContainerIfNeeded(ctx, cli, auths, cfg, c)
		if err != nil {
			logContainerf(slog.LevelError, ref, "update error: %v", err)
			failed++
			continue
		}
		if wasUpdated {
			updated++
		}
	}

	slog.Default().LogAttrs(
		logCtx,
		slog.LevelInfo,
		"session done",
		slog.Int("scanned", scanned),
		slog.Int("updated", updated),
		slog.Int("failed", failed),
		slog.Duration("duration", time.Since(start)),
	)
}
