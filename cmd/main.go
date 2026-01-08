package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
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
}

func main() {
	var cfg config

	flag.DurationVar(&cfg.interval, "interval", 30*time.Second, "Check interval (e.g. 30s)")
	flag.BoolVar(&cfg.cleanup, "cleanup", false, "Remove old images for updated containers")
	flag.BoolVar(&cfg.labelEnable, "label-enable", false, "Update only containers that have label key=value")

	flag.StringVar(&cfg.labelKey, "label-key", "devem.tech/up-to-date.enabled", "Label key to match (used with --label-enable)")
	flag.StringVar(&cfg.labelValue, "label-value", "true", "Label value to match (used with --label-enable)")

	flag.StringVar(&cfg.dockerConfigPath, "docker-config", "/config.json", "Path to docker config.json for registry auth (optional)")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cli, err := client.New(client.FromEnv)
	if err != nil {
		log.Fatalf("docker client: %v", err)
	}
	defer cli.Close()

	auths, err := loadDockerConfigAuths(cfg.dockerConfigPath)
	if err != nil {
		log.Printf("config: %v (continuing without registry auth)", err)
	}

	log.Printf(
		"up-to-date started: interval=%s cleanup=%v label-enable=%v label=%s=%s",
		cfg.interval, cfg.cleanup, cfg.labelEnable, cfg.labelKey, cfg.labelValue,
	)

	runOnce(ctx, cli, auths, cfg)

	t := time.NewTicker(cfg.interval)
	defer t.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Printf("shutdown")
			return
		case <-t.C:
			runOnce(ctx, cli, auths, cfg)
		}
	}
}

func runOnce(ctx context.Context, cli *client.Client, auths dockerAuthIndex, cfg config) {
	containers, err := listTargetContainers(ctx, cli, cfg)
	if err != nil {
		log.Printf("list containers error: %v", err)
		return
	}

	log.Printf("scan: %d container(s) eligible", len(containers))

	for _, c := range containers {
		name := "<unknown>"
		if len(c.Names) > 0 {
			name = shortName(c.Names[0])
		}

		if err := updateContainerIfNeeded(ctx, cli, auths, cfg.cleanup, c.ID); err != nil {
			log.Printf("[%s] %v", name, err)
		}
	}
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

func updateContainerIfNeeded(ctx context.Context, cli *client.Client, auths dockerAuthIndex, cleanup bool, containerID string) error {
	ins, err := cli.ContainerInspect(ctx, containerID, client.ContainerInspectOptions{})
	if err != nil {
		return err
	}

	cur := ins.Container
	name := shortName(cur.Name)

	imageRef := cur.Config.Image
	if imageRef == "" {
		return errors.New("container has empty Config.Image")
	}

	oldImageID := cur.Image
	if oldImageID == "" {
		if img, err := cli.ImageInspect(ctx, imageRef); err == nil {
			oldImageID = img.ID
		}
	}

	log.Printf("[%s] checking for updates: pulling (%s)", name, imageRef)

	regAuth, _ := auths.registryAuthForImageRef(imageRef)
	if err := pullImage(ctx, cli, imageRef, regAuth); err != nil {
		return fmt.Errorf("pull %q: %w", imageRef, err)
	}

	newImg, err := cli.ImageInspect(ctx, imageRef)
	if err != nil {
		return fmt.Errorf("inspect pulled image %q: %w", imageRef, err)
	}
	newImageID := newImg.ID

	if newImageID == "" || oldImageID == "" || newImageID == oldImageID {
		log.Printf("[%s] no update (%s)", name, imageRef)
		return nil
	}

	log.Printf("[%s] update available: %s -> %s (%s)", name, shortID(oldImageID), shortID(newImageID), imageRef)

	fullName := strings.TrimPrefix(cur.Name, "/")
	netCfg := buildNetworkingConfig(cur)

	newConfig := cur.Config
	newConfig.Image = imageRef

	log.Printf("[%s] stopping container", name)
	if _, err := cli.ContainerStop(ctx, containerID, client.ContainerStopOptions{}); err != nil {
		return fmt.Errorf("stop: %w", err)
	}

	log.Printf("[%s] removing container", name)
	if _, err := cli.ContainerRemove(ctx, containerID, client.ContainerRemoveOptions{
		Force:         false,
		RemoveVolumes: false,
	}); err != nil {
		return fmt.Errorf("remove: %w", err)
	}

	log.Printf("[%s] creating container", name)
	created, err := cli.ContainerCreate(ctx, client.ContainerCreateOptions{
		Config:           newConfig,
		HostConfig:       cur.HostConfig,
		NetworkingConfig: netCfg,
		Name:             fullName,
	})
	if err != nil {
		return fmt.Errorf("create: %w", err)
	}
	if len(created.Warnings) > 0 {
		log.Printf("[%s] create warnings: %v", name, created.Warnings)
	}

	log.Printf("[%s] starting container", name)
	if _, err := cli.ContainerStart(ctx, created.ID, client.ContainerStartOptions{}); err != nil {
		return fmt.Errorf("start: %w", err)
	}

	log.Printf("[%s] updated successfully", name)

	if cleanup {
		log.Printf("[%s] cleanup enabled: checking old image %s", name, shortID(oldImageID))
		removed, reason, err := cleanupOldImageIfUnused(ctx, cli, oldImageID)
		if err != nil {
			log.Printf("[%s] cleanup error for %s: %v", name, shortID(oldImageID), err)
		} else if removed {
			log.Printf("[%s] cleanup: removed old image %s (%s)", name, shortID(oldImageID), reason)
		} else {
			log.Printf("[%s] cleanup: skipped old image %s (%s)", name, shortID(oldImageID), reason)
		}
	} else {
		log.Printf("[%s] cleanup disabled: keeping old image %s", name, shortID(oldImageID))
	}

	return nil
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
