package podman

import (
	"context"
	"fmt"
	"os"

	"github.com/containers/podman/v4/libpod/define"
	"github.com/containers/podman/v4/pkg/bindings/containers"
	"github.com/containers/podman/v4/pkg/bindings/images"
)

var _ Container = (*Client)(nil)

type Container interface {
	// Start attempts to Start a container.
	Start(ctx context.Context, id string) error

	// Stop attempts to Stop a container.
	Stop(ctx context.Context, id string) error

	// Pull attempts to pull a container image.
	Pull(ctx context.Context, image string) error

	// Exists returns true if a container id exists.
	Exists(ctx context.Context, id string) (bool, error)

	// WaitForStatus will block until container meets the expected container status.
	WaitForStatus(ctx context.Context, id string, status define.ContainerStatus) (int32, error)
}

type Client struct{}

func NewClient() *Client {
	return &Client{}
}

func (c *Client) Start(ctx context.Context, id string) error {
	// TODO
	return nil
}

func (c *Client) Stop(ctx context.Context, id string) error {
	// TODO
	return nil
}

func (c *Client) Pull(ctx context.Context, image string) error {
	_, err := images.Pull(ctx, image, &images.PullOptions{})
	return err
}

func (c *Client) Exists(ctx context.Context, id string) (bool, error) {
	return exists(ctx, id)
}

func (c *Client) WaitForStatus(ctx context.Context, id string, status define.ContainerStatus) (int32, error) {
	return containers.Wait(ctx, id, &containers.WaitOptions{
		Condition: []define.ContainerStatus{status},
	})
}

func exists(ctx context.Context, id string) (bool, error) {
	// WithExternal means that it will check for the container outside of podman.
	opts := new(containers.ExistsOptions).WithExternal(true)
	return containers.Exists(ctx, id, opts)
}

func getSocketPath() (string, error) {
	provider, err := GetSystemProvider()
	if err != nil {
		return "", err
	}

	// macOS we hope :)
	if provider != nil {
		vm, err := provider.LoadVMByName("podman-machine-default")
		if err != nil {
			return "", err
		}

		machine, err := vm.Inspect()
		if err != nil {
			return "", fmt.Errorf("failed to inspect vm: %w", err)
		}

		if machine.ConnectionInfo.PodmanSocket != nil {
			return fmt.Sprintf("unix:%s/podman/podman.sock", machine.ConnectionInfo.PodmanSocket.Path), nil
		} else {
			return "", fmt.Errorf("failed to get socket path from machine: %w", err)
		}
	}

	sockDir := os.Getenv("XDG_RUNTIME_DIR")
	if sockDir == "" {
		return "", fmt.Errorf("failed to find rootless socket")
	}
	return fmt.Sprintf("unix:%s/podman/podman.sock", sockDir), nil
}
