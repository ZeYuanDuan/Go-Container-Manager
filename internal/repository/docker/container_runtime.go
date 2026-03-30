package docker

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/alsonduan/go-container-manager/internal/domain"
)

// ContainerRuntime implements domain.ContainerRuntime using the Docker SDK.
type ContainerRuntime struct {
	cli *client.Client
}

func NewContainerRuntime() (*ContainerRuntime, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, fmt.Errorf("docker client: %w", err)
	}
	return &ContainerRuntime{cli: cli}, nil
}

func (r *ContainerRuntime) CreateAndStart(ctx context.Context, c *domain.Container, image string, hostMountPath string) (string, error) {
	// Docker requires absolute paths for bind mounts.
	absMount, err := filepath.Abs(hostMountPath)
	if err != nil {
		return "", err
	}
	resp, err := r.cli.ContainerCreate(ctx,
		&container.Config{
			Image: image,
			Cmd:   c.Command,
		},
		&container.HostConfig{
			Binds: []string{absMount + ":" + c.MountPath},
		},
		nil, nil, "aidms-"+c.ID,
	)
	if err != nil {
		return "", err
	}
	if err := r.cli.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
		// Clean up the created container on start failure.
		_ = r.cli.ContainerRemove(ctx, resp.ID, container.RemoveOptions{Force: true})
		return "", err
	}
	return resp.ID, nil
}

func (r *ContainerRuntime) Start(ctx context.Context, dockerContainerID string) error {
	return r.cli.ContainerStart(ctx, dockerContainerID, container.StartOptions{})
}

func (r *ContainerRuntime) Stop(ctx context.Context, dockerContainerID string) error {
	timeout := 1
	return r.cli.ContainerStop(ctx, dockerContainerID, container.StopOptions{Timeout: &timeout})
}

func (r *ContainerRuntime) Remove(ctx context.Context, dockerContainerID string) error {
	return r.cli.ContainerRemove(ctx, dockerContainerID, container.RemoveOptions{Force: true})
}

func (r *ContainerRuntime) InspectState(ctx context.Context, dockerContainerID string) (string, error) {
	info, err := r.cli.ContainerInspect(ctx, dockerContainerID)
	if err != nil {
		return "", err
	}
	if info.State == nil {
		return "unknown", nil
	}
	return string(info.State.Status), nil
}

func (r *ContainerRuntime) WaitExit(ctx context.Context, dockerContainerID string, timeout time.Duration) bool {
	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	statusCh, errCh := r.cli.ContainerWait(waitCtx, dockerContainerID, container.WaitConditionNotRunning)
	select {
	case <-statusCh:
		return true
	case <-errCh:
		return false
	case <-waitCtx.Done():
		return false
	}
}
