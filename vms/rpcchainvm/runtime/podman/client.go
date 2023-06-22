package podman

import (
	"context"
	"github.com/containers/podman/v4/libpod/define"
	"github.com/containers/podman/v4/pkg/bindings/containers"
	"github.com/containers/podman/v4/pkg/bindings/images"
	"github.com/containers/podman/v4/pkg/specgen"
)

var _ Container = (*Client)(nil)

type Container interface {
	// Start attempts to Start a container.
	Start(ctx context.Context, image string) (string, error)

	// Stop attempts to Stop a container.
	Stop(ctx context.Context, id string) error

	// Pull attempts to pull a container image.
	Pull(ctx context.Context, image string) ([]string, error)

	// Exists returns true if a container id exists.
	Exists(ctx context.Context, id string) (bool, error)

	// WaitForStatus will block until container meets the expected container status.
	WaitForStatus(ctx context.Context, id string, status define.ContainerStatus) (int32, error)
}

type Client struct{}

func NewClient() *Client {
	return &Client{}
}

func (c *Client) Start(ctx context.Context, image string) (string, error) {
	s := specgen.NewSpecGenerator(image, false)
	s.Terminal = true
	r, err := containers.CreateWithSpec(ctx, s, &containers.CreateOptions{})
	if err != nil {
		return "", err
	}
	return r.ID, containers.Start(ctx, r.ID, &containers.StartOptions{})
}

func (c *Client) Stop(ctx context.Context, id string) error {
	return containers.Stop(ctx, id, &containers.StopOptions{})
}

func (c *Client) Pull(ctx context.Context, image string) ([]string, error) {
	return images.Pull(ctx, image, &images.PullOptions{})
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
	// TODO: make this configurable, hardcode for macos for now
	socket := "unix:" + "/Users/hao.hao/.local/share/containers/podman/machine/qemu/podman.sock"
	return socket, nil
}
