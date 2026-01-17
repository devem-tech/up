package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"
)

type config struct {
	interval    time.Duration
	cleanup     bool
	labelEnable bool
	labelKey    string
	labelValue  string

	dockerConfigPath string // путь до config.json для registry auth (опционально)

	rollingLabelKey   string
	rollingLabelValue string

	logLevel slog.Level
}

const appVersion = "0.1.0"

var logCtx = context.Background()

func parseLogLevel(s string) (slog.Level, error) {
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

type containerRef struct {
	Name string
	ID   string
}

func containerRefFromSummary(c container.Summary) containerRef {
	name := "<unknown>"
	if len(c.Names) > 0 {
		name = shortName(c.Names[0])
	}
	return containerRef{Name: name, ID: shortID(c.ID)}
}

func containerRefFromInspect(cur container.InspectResponse) containerRef {
	return containerRef{Name: shortName(cur.Name), ID: shortID(cur.ID)}
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

func main() {
	var cfg config
	var logLevelStr string

	flag.DurationVar(&cfg.interval, "interval", 30*time.Second, "Check interval (e.g. 30s)")
	flag.BoolVar(&cfg.cleanup, "cleanup", false, "Remove old images for updated containers")
	flag.BoolVar(&cfg.labelEnable, "label-enable", false, "Update only containers that have label key=value")

	flag.StringVar(&cfg.labelKey, "label-key", "devem.tech/up-to-date.enabled", "Label key to match (used with --label-enable)")
	flag.StringVar(&cfg.labelValue, "label-value", "true", "Label value to match (used with --label-enable)")

	flag.StringVar(&cfg.dockerConfigPath, "docker-config", "/config.json", "Path to docker config.json for registry auth (optional)")

	flag.StringVar(&cfg.rollingLabelKey, "rolling-label-key", "devem.tech/up-to-date.rolling", "Label key to enable rolling updates for a container")
	flag.StringVar(&cfg.rollingLabelValue, "rolling-label-value", "true", "Label value to enable rolling updates (used with --rolling-label-key)")
	flag.StringVar(&logLevelStr, "log-level", "info", "Log level: debug, info, warn, error")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	parsedLogLevel, err := parseLogLevel(logLevelStr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "log level: %v\n", err)
		os.Exit(2)
	}
	cfg.logLevel = parsedLogLevel
	handler := slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: cfg.logLevel,
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
	})
	slog.SetDefault(slog.New(handler))

	cli, err := client.New(client.FromEnv)
	if err != nil {
		logf(slog.LevelError, "docker client: %v", err)
		os.Exit(1)
	}
	defer cli.Close()

	auths, err := loadDockerConfigAuths(cfg.dockerConfigPath)
	if err != nil {
		logf(slog.LevelWarn, "config: %v (continuing without registry auth)", err)
	}

	logf(slog.LevelInfo, "up-to-date %s", appVersion)
	logf(slog.LevelInfo, "--log-level=%s", strings.ToLower(cfg.logLevel.String()))
	logf(slog.LevelInfo, "--interval=%s", cfg.interval)
	logf(slog.LevelInfo, "--cleanup=%t", cfg.cleanup)
	logf(slog.LevelInfo, "--label-enable=%t", cfg.labelEnable)

	runOnce(ctx, cli, auths, cfg)

	t := time.NewTicker(cfg.interval)
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

func runOnce(ctx context.Context, cli *client.Client, auths dockerAuthIndex, cfg config) {
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

func listTargetContainers(ctx context.Context, cli *client.Client, cfg config) ([]container.Summary, error) {
	var f client.Filters
	if cfg.labelEnable {
		f = make(client.Filters).Add("label", fmt.Sprintf("%s=%s", cfg.labelKey, cfg.labelValue))
	}

	res, err := cli.ContainerList(ctx, client.ContainerListOptions{
		All:     false,
		Filters: f,
	})
	if err != nil {
		return nil, err
	}
	return res.Items, nil
}

func updateContainerIfNeeded(ctx context.Context, cli *client.Client, auths dockerAuthIndex, cfg config, summary container.Summary) (bool, error) {
	ref := containerRefFromSummary(summary)
	ins, err := cli.ContainerInspect(ctx, summary.ID, client.ContainerInspectOptions{})
	if err != nil {
		return false, fmt.Errorf("inspect container: %w", err)
	}

	cur := ins.Container
	ref = containerRefFromInspect(cur)

	imageRef := cur.Config.Image
	if imageRef == "" {
		return false, errors.New("container has empty Config.Image")
	}

	oldImageID := cur.Image
	if oldImageID == "" {
		if img, err := cli.ImageInspect(ctx, imageRef); err == nil {
			oldImageID = img.ID
		}
	}

	logContainerf(slog.LevelDebug, ref, "checking for updates (%s)", imageRef)

	regAuth, _ := auths.registryAuthForImageRef(imageRef)
	if err := pullImage(ctx, cli, imageRef, regAuth); err != nil {
		return false, fmt.Errorf("pull %q: %w", imageRef, err)
	}

	newImg, err := cli.ImageInspect(ctx, imageRef)
	if err != nil {
		return false, fmt.Errorf("inspect pulled image %q: %w", imageRef, err)
	}
	newImageID := newImg.ID

	if newImageID == "" || oldImageID == "" || newImageID == oldImageID {
		logContainerf(slog.LevelDebug, ref, "no update")
		return false, nil
	}

	logContainerf(slog.LevelInfo, ref, "update available %s (%s)", imageRef, shortID(newImageID))

	if supportsRollingUpdate(cur) && hasRollingLabel(cur, cfg.rollingLabelKey, cfg.rollingLabelValue) {
		if err := rollingUpdateContainer(ctx, cli, cur, imageRef); err != nil {
			return false, fmt.Errorf("rolling update: %w", err)
		}
	} else {
		if err := recreateContainer(ctx, cli, cur, imageRef); err != nil {
			return false, err
		}
	}

	if cfg.cleanup {
		removed, reason, err := cleanupOldImageIfUnused(ctx, cli, oldImageID)
		if err != nil {
			logContainerf(slog.LevelWarn, ref, "cleanup error for %s: %v", shortID(oldImageID), err)
		} else if removed {
			logContainerf(slog.LevelInfo, ref, "removed old image %s (%s)", shortID(oldImageID), reason)
		} else {
			logContainerf(slog.LevelInfo, ref, "skipped old image %s (%s)", shortID(oldImageID), reason)
		}
	} else {
		logContainerf(slog.LevelDebug, ref, "cleanup disabled: keeping old image %s", shortID(oldImageID))
	}

	return true, nil
}

func supportsRollingUpdate(cur container.InspectResponse) bool {
	if cur.HostConfig == nil {
		return true
	}
	if cur.HostConfig.NetworkMode == "host" {
		return false
	}
	if cur.HostConfig.PublishAllPorts {
		return false
	}
	if len(cur.HostConfig.PortBindings) > 0 {
		return false
	}
	return true
}

func hasRollingLabel(cur container.InspectResponse, key, value string) bool {
	if cur.Config == nil || cur.Config.Labels == nil {
		return false
	}
	if key == "" {
		return false
	}
	got, ok := cur.Config.Labels[key]
	if !ok {
		return false
	}
	if value == "" {
		return true
	}
	return got == value
}

func recreateContainer(ctx context.Context, cli *client.Client, cur container.InspectResponse, imageRef string) error {
	fullName := strings.TrimPrefix(cur.Name, "/")
	netCfg := buildNetworkingConfig(cur)

	newConfig := cur.Config
	newConfig.Image = imageRef

	refOld := containerRefFromInspect(cur)
	logContainerf(slog.LevelInfo, refOld, "stopping container")
	if _, err := cli.ContainerStop(ctx, cur.ID, client.ContainerStopOptions{}); err != nil {
		return fmt.Errorf("stop: %w", err)
	}

	logContainerf(slog.LevelInfo, refOld, "removing container")
	if _, err := cli.ContainerRemove(ctx, cur.ID, client.ContainerRemoveOptions{
		Force:         false,
		RemoveVolumes: false,
	}); err != nil {
		return fmt.Errorf("remove: %w", err)
	}

	refNew := containerRef{Name: fullName, ID: ""}
	logContainerf(slog.LevelInfo, refNew, "creating container")
	created, err := cli.ContainerCreate(ctx, client.ContainerCreateOptions{
		Config:           newConfig,
		HostConfig:       cur.HostConfig,
		NetworkingConfig: netCfg,
		Name:             fullName,
	})
	if err != nil {
		return fmt.Errorf("create: %w", err)
	}
	refNew.ID = shortID(created.ID)
	if len(created.Warnings) > 0 {
		logContainerf(slog.LevelWarn, refNew, "create warnings: %v", created.Warnings)
	}

	logContainerf(slog.LevelInfo, refNew, "starting container")
	if _, err := cli.ContainerStart(ctx, created.ID, client.ContainerStartOptions{}); err != nil {
		return fmt.Errorf("start: %w", err)
	}

	logContainerf(slog.LevelInfo, refNew, "updated successfully")
	return nil
}

func rollingUpdateContainer(ctx context.Context, cli *client.Client, cur container.InspectResponse, imageRef string) error {
	refOld := containerRefFromInspect(cur)
	fullName := strings.TrimPrefix(cur.Name, "/")
	netCfg := buildNetworkingConfig(cur)

	newConfig := cur.Config
	newConfig.Image = imageRef

	tempName := fmt.Sprintf("%s.next", fullName)
	refNew := containerRef{Name: tempName, ID: ""}
	logContainerf(slog.LevelInfo, refNew, "creating new container")
	created, err := cli.ContainerCreate(ctx, client.ContainerCreateOptions{
		Config:           newConfig,
		HostConfig:       cur.HostConfig,
		NetworkingConfig: netCfg,
		Name:             tempName,
	})
	if err != nil {
		return fmt.Errorf("create: %w", err)
	}
	refNew.ID = shortID(created.ID)
	if len(created.Warnings) > 0 {
		logContainerf(slog.LevelWarn, refNew, "create warnings: %v", created.Warnings)
	}

	logContainerf(slog.LevelInfo, refNew, "starting new container")
	if _, err := cli.ContainerStart(ctx, created.ID, client.ContainerStartOptions{}); err != nil {
		_, _ = cli.ContainerRemove(ctx, created.ID, client.ContainerRemoveOptions{Force: true})
		return fmt.Errorf("start: %w", err)
	}

	if err := waitForHealthyIfConfigured(ctx, cli, created.ID, 30*time.Second); err != nil {
		_, _ = cli.ContainerRemove(ctx, created.ID, client.ContainerRemoveOptions{Force: true})
		return fmt.Errorf("health check: %w", err)
	}

	logContainerf(slog.LevelInfo, refOld, "stopping old container")
	if _, err := cli.ContainerStop(ctx, cur.ID, client.ContainerStopOptions{}); err != nil {
		return fmt.Errorf("stop old: %w", err)
	}

	logContainerf(slog.LevelInfo, refOld, "removing old container")
	if _, err := cli.ContainerRemove(ctx, cur.ID, client.ContainerRemoveOptions{
		Force:         false,
		RemoveVolumes: false,
	}); err != nil {
		return fmt.Errorf("remove old: %w", err)
	}

	logContainerf(slog.LevelInfo, refNew, "renaming new container to %s", fullName)
	if _, err := cli.ContainerRename(ctx, created.ID, client.ContainerRenameOptions{NewName: fullName}); err != nil {
		return fmt.Errorf("rename: %w", err)
	}

	refNew.Name = fullName
	logContainerf(slog.LevelInfo, refNew, "updated successfully")
	return nil
}

func waitForHealthyIfConfigured(ctx context.Context, cli *client.Client, containerID string, timeout time.Duration) error {
	ins, err := cli.ContainerInspect(ctx, containerID, client.ContainerInspectOptions{})
	if err != nil {
		return err
	}
	cur := ins.Container
	if cur.Config == nil || cur.Config.Healthcheck == nil {
		return nil
	}

	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return fmt.Errorf("timeout waiting for healthy")
		case <-ticker.C:
			ins, err := cli.ContainerInspect(ctx, containerID, client.ContainerInspectOptions{})
			if err != nil {
				return err
			}
			cur := ins.Container
			if cur.State == nil || cur.State.Health == nil {
				return nil
			}
			switch cur.State.Health.Status {
			case container.Healthy:
				return nil
			case container.Unhealthy:
				return fmt.Errorf("container reported unhealthy")
			}
		}
	}
}

func buildNetworkingConfig(cur container.InspectResponse) *network.NetworkingConfig {
	if cur.NetworkSettings == nil || cur.NetworkSettings.Networks == nil {
		return nil
	}

	endpoints := map[string]*network.EndpointSettings{}
	for netName, ep := range cur.NetworkSettings.Networks {
		endpoints[netName] = ep
	}

	return &network.NetworkingConfig{
		EndpointsConfig: endpoints,
	}
}

func pullImage(ctx context.Context, cli *client.Client, ref, registryAuth string) error {
	resp, err := cli.ImagePull(ctx, ref, client.ImagePullOptions{
		RegistryAuth: registryAuth,
	})
	if err != nil {
		return err
	}
	defer resp.Close()

	return resp.Wait(ctx)
}

func cleanupOldImageIfUnused(ctx context.Context, cli *client.Client, oldImageID string) (bool, string, error) {
	oldImageID = strings.TrimSpace(oldImageID)
	if oldImageID == "" {
		return false, "empty image id", nil
	}

	res, err := cli.ContainerList(ctx, client.ContainerListOptions{All: true})
	if err != nil {
		return false, "failed to list containers", fmt.Errorf("list containers for cleanup: %w", err)
	}

	for _, c := range res.Items {
		if c.ImageID == oldImageID {
			return false, "old image still in use", nil
		}
	}

	_, err = cli.ImageRemove(ctx, oldImageID, client.ImageRemoveOptions{
		Force:         false,
		PruneChildren: true,
	})
	if err != nil {
		return false, "failed to remove old image", err
	}

	return true, "old image unused", nil
}

func shortID(id string) string {
	id = strings.TrimPrefix(id, "sha256:")
	if len(id) > 12 {
		return id[:12]
	}
	return id
}

func shortName(name string) string {
	if name == "" {
		return "<noname>"
	}
	return strings.TrimPrefix(name, "/")
}
