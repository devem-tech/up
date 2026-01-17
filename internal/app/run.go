package app

import (
	"context"
	"fmt"
	"html"
	"log/slog"
	"strings"
	"time"

	"github.com/moby/moby/client"

	"github.com/devem-tech/up-to-date/internal/dockerauth"
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
	updatedRefs := make([]containerRef, 0)
	failedRefs := make([]containerRef, 0)

	logf(slog.LevelDebug, "scan: %d container(s) eligible", scanned)

	for _, c := range containers {
		ref := containerRefFromSummary(c)
		wasUpdated, err := updateContainerIfNeeded(ctx, cli, auths, cfg, c)
		if err != nil {
			logContainerf(slog.LevelError, ref, "update error: %v", err)
			failed++
			failedRefs = append(failedRefs, ref)
			continue
		}
		if wasUpdated {
			updated++
			updatedRefs = append(updatedRefs, ref)
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

	if cfg.Notify != nil && (updated > 0 || failed > 0) {
		msg := buildNotificationMessage(updatedRefs, failedRefs)
		if err := cfg.Notify(ctx, msg); err != nil {
			logf(slog.LevelWarn, "telegram notify error: %v", err)
		}
	}
}

func buildNotificationMessage(updatedRefs, failedRefs []containerRef) string {
	var b strings.Builder
	b.WriteString("<b>Up-to-date</b>")
	if len(updatedRefs) > 0 {
		b.WriteString("\n\n✅ Updated:\n")
		writeRefList(&b, updatedRefs)
	}
	if len(failedRefs) > 0 {
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString("❌ Failed:\n")
		writeRefList(&b, failedRefs)
	}
	return b.String()
}

func writeRefList(b *strings.Builder, refs []containerRef) {
	for _, ref := range refs {
		name := ref.Name
		if name == "" {
			name = "<noname>"
		}
		fmt.Fprintf(b, "\n• <code>%s</code>", html.EscapeString(name))
	}
}
