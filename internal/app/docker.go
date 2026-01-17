package app

import (
	"context"
	"fmt"
	"strings"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"
)

func listTargetContainers(ctx context.Context, cli *client.Client, cfg Config) ([]container.Summary, error) {
	var f client.Filters
	if cfg.LabelEnable {
		f = make(client.Filters).Add("label", cfg.Label)
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
