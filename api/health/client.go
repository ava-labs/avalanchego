// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package health

import (
	"context"
	"time"

	"github.com/ava-labs/avalanchego/utils/rpc"
)

var _ Client = (*client)(nil)

// Client interface for Avalanche Health API Endpoint
// For helpers to wait for Readiness, Health, or Liveness, see AwaitReady,
// AwaitHealthy, and AwaitAlive.
type Client interface {
	// Readiness returns if the node has finished initialization
	Readiness(ctx context.Context, tags []string, options ...rpc.Option) (*APIReply, error)
	// Health returns a summation of the health of the node
	Health(ctx context.Context, tags []string, options ...rpc.Option) (*APIReply, error)
	// Liveness returns if the node is in need of a restart
	Liveness(ctx context.Context, tags []string, options ...rpc.Option) (*APIReply, error)
}

// Client implementation for Avalanche Health API Endpoint
type client struct {
	requester rpc.EndpointRequester
}

// NewClient returns a client to interact with Health API endpoint
func NewClient(uri string) Client {
	return &client{requester: rpc.NewEndpointRequester(
		uri + "/ext/health",
	)}
}

func (c *client) Readiness(ctx context.Context, tags []string, options ...rpc.Option) (*APIReply, error) {
	res := &APIReply{}
	err := c.requester.SendRequest(ctx, "health.readiness", &APIArgs{Tags: tags}, res, options...)
	return res, err
}

func (c *client) Health(ctx context.Context, tags []string, options ...rpc.Option) (*APIReply, error) {
	res := &APIReply{}
	err := c.requester.SendRequest(ctx, "health.health", &APIArgs{Tags: tags}, res, options...)
	return res, err
}

func (c *client) Liveness(ctx context.Context, tags []string, options ...rpc.Option) (*APIReply, error) {
	res := &APIReply{}
	err := c.requester.SendRequest(ctx, "health.liveness", &APIArgs{Tags: tags}, res, options...)
	return res, err
}

// AwaitReady polls the node every [freq] until the node reports ready.
// Only returns an error if [ctx] returns an error.
func AwaitReady(ctx context.Context, c Client, freq time.Duration, tags []string, options ...rpc.Option) (bool, error) {
	return await(ctx, freq, c.Readiness, tags, options...)
}

// AwaitHealthy polls the node every [freq] until the node reports healthy.
// Only returns an error if [ctx] returns an error.
func AwaitHealthy(ctx context.Context, c Client, freq time.Duration, tags []string, options ...rpc.Option) (bool, error) {
	return await(ctx, freq, c.Health, tags, options...)
}

// AwaitAlive polls the node every [freq] until the node reports liveness.
// Only returns an error if [ctx] returns an error.
func AwaitAlive(ctx context.Context, c Client, freq time.Duration, tags []string, options ...rpc.Option) (bool, error) {
	return await(ctx, freq, c.Liveness, tags, options...)
}

func await(
	ctx context.Context,
	freq time.Duration,
	check func(ctx context.Context, tags []string, options ...rpc.Option) (*APIReply, error),
	tags []string,
	options ...rpc.Option,
) (bool, error) {
	ticker := time.NewTicker(freq)
	defer ticker.Stop()

	for {
		res, err := check(ctx, tags, options...)
		if err == nil && res.Healthy {
			return true, nil
		}

		select {
		case <-ticker.C:
		case <-ctx.Done():
			return false, ctx.Err()
		}
	}
}
