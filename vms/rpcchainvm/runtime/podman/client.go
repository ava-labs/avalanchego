package podman

import (
	"context"
	"fmt"
	"os"

	"github.com/containers/podman/v4/libpod/define"
	"github.com/containers/podman/v4/pkg/bindings/containers"
	"github.com/containers/podman/v4/pkg/bindings/images"
)

type Client struct {
}

func NewClient() *Client {
	return &Client{}
}

// Start attempts to Start a container.
func (c *Client) Start(ctx context.Context, id string) {
	// TODO
}

// Pull attempts to pull a container image.
func (c *Client) Pull(ctx context.Context, image string) error {
	_, err := images.Pull(ctx, image, &images.PullOptions{})
	return err
}

// Exists returns true if a container id exists.
func (c *Client) Exists(ctx context.Context, id string) (bool, error) {
	return exists(ctx, id)
}

// WaitForStatus will block until container meets the expected container status.
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

// TODO add support macOS
func getSocketPath() (string, error) {
	sockDir := os.Getenv("XDG_RUNTIME_DIR")
	if sockDir == "" {
		return "", fmt.Errorf("failed to find rootless socket")
	}
	return fmt.Sprintf("unix:%s/podman/podman.sock", sockDir), nil
}
