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
	opCtx := context.WithoutCancel(ctx)

	runOnce(opCtx, cli, auths, cfg)
	if ctx.Err() != nil {
		logf(slog.LevelInfo, "shutdown")
		return
	}

	t := time.NewTicker(cfg.Interval)
	defer t.Stop()

	for {
		select {
		case <-ctx.Done():
			logf(slog.LevelInfo, "shutdown")
			return
		case <-t.C:
			runOnce(opCtx, cli, auths, cfg)
			if ctx.Err() != nil {
				logf(slog.LevelInfo, "shutdown")
				return
			}
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
	updatedRefs := make([]notifyRef, 0)
	failedRefs := make([]notifyRef, 0)

	logf(slog.LevelDebug, "scan: %d container(s) eligible", scanned)

	for _, c := range containers {
		ref := containerRefFromSummary(c)
		wasUpdated, newImageID, err := updateContainerIfNeeded(ctx, cli, auths, cfg, c)
		if err != nil {
			logContainerf(slog.LevelError, ref, "update error: %v", err)
			failed++
			if !isTransientError(err) {
				failedRefs = append(failedRefs, notifyRef{Name: ref.Name, Info: err.Error()})
			}
			continue
		}
		if wasUpdated {
			updated++
			info := shortID(newImageID)
			if info == "" {
				info = "unknown"
			}
			updatedRefs = append(updatedRefs, notifyRef{Name: ref.Name, Info: info})
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

	if cfg.Notify != nil && (len(updatedRefs) > 0 || len(failedRefs) > 0) {
		msg := buildNotificationMessage(updatedRefs, failedRefs)
		if err := cfg.Notify(ctx, msg); err != nil {
			logf(slog.LevelWarn, "telegram notify error: %v", err)
		}
	}
}

type notifyRef struct {
	Name string
	Info string
}

func buildNotificationMessage(updatedRefs, failedRefs []notifyRef) string {
	var b strings.Builder
	b.WriteString("<b>Up-to-date</b>")
	if len(updatedRefs) > 0 {
		b.WriteString("\n\n✅ Updated:\n")
		writeRefList(&b, updatedRefs, false, false)
	}
	if len(failedRefs) > 0 {
		if b.Len() > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString("❌ Failed:\n")
		writeRefList(&b, failedRefs, true, true)
	}
	return b.String()
}

func writeRefList(b *strings.Builder, refs []notifyRef, infoAsBlock, blankLineBetween bool) {
	for i, ref := range refs {
		if i > 0 && blankLineBetween {
			b.WriteString("\n")
		}
		name := ref.Name
		if name == "" {
			name = "<noname>"
		}
		fmt.Fprintf(b, "\n• <code>%s</code>", html.EscapeString(name))
		if ref.Info == "" {
			continue
		}
		info := html.EscapeString(strings.TrimSpace(ref.Info))
		if infoAsBlock {
			fmt.Fprintf(b, "\n<pre>%s</pre>", info)
		} else {
			fmt.Fprintf(b, " – <code>%s</code>", info)
		}
	}
}
