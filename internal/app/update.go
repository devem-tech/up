package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/devem-tech/up-to-date/internal/dockerauth"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
)

func updateContainerIfNeeded(ctx context.Context, cli *client.Client, auths dockerauth.Index, cfg Config, summary container.Summary) (bool, string, error) {
	ref := containerRefFromSummary(summary)
	ins, err := cli.ContainerInspect(ctx, summary.ID, client.ContainerInspectOptions{})
	if err != nil {
		return false, "", fmt.Errorf("inspect container: %w", err)
	}

	cur := ins.Container
	ref = containerRefFromInspect(cur)

	imageRef := cur.Config.Image
	if imageRef == "" {
		return false, "", errors.New("container has empty Config.Image")
	}

	oldImageID := cur.Image
	if oldImageID == "" {
		if img, err := cli.ImageInspect(ctx, imageRef); err == nil {
			oldImageID = img.ID
		}
	}

	logContainerf(slog.LevelDebug, ref, "checking for updates (%s)", imageRef)

	regAuth, _ := auths.RegistryAuthForImageRef(imageRef)
	if err := pullImage(ctx, cli, imageRef, regAuth); err != nil {
		return false, "", fmt.Errorf("pull %q: %w", imageRef, err)
	}

	newImg, err := cli.ImageInspect(ctx, imageRef)
	if err != nil {
		return false, "", fmt.Errorf("inspect pulled image %q: %w", imageRef, err)
	}
	newImageID := newImg.ID

	if newImageID == "" || oldImageID == "" || newImageID == oldImageID {
		logContainerf(slog.LevelDebug, ref, "no update")
		return false, "", nil
	}

	logContainerf(slog.LevelInfo, ref, "update available %s (%s)", imageRef, shortID(newImageID))

	if supportsRollingUpdate(cur) && hasRollingLabel(cur, cfg.RollingLabel) {
		if err := rollingUpdateContainer(ctx, cli, cur, imageRef); err != nil {
			return false, "", fmt.Errorf("rolling update: %w", err)
		}
	} else {
		if err := recreateContainer(ctx, cli, cur, imageRef); err != nil {
			return false, "", err
		}
	}

	if cfg.Cleanup {
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

	return true, newImageID, nil
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

func hasRollingLabel(cur container.InspectResponse, label string) bool {
	if cur.Config == nil || cur.Config.Labels == nil {
		return false
	}
	if label == "" {
		return false
	}
	key, value, hasValue := strings.Cut(label, "=")
	if key == "" {
		return false
	}
	got, ok := cur.Config.Labels[key]
	if !ok {
		return false
	}
	if !hasValue || value == "" {
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
