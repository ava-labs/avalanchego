// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package health

import (
	"context"
	"time"

	"github.com/ava-labs/avalanchego/utils/rpc"
)

type Client struct {
	Requester rpc.EndpointRequester
}

func NewClient(uri string) *Client {
	return &Client{Requester: rpc.NewEndpointRequester(
		uri + "/ext/health",
	)}
}

// Readiness returns if the node has finished initialization
func (c *Client) Readiness(ctx context.Context, tags []string, options ...rpc.Option) (*APIReply, error) {
	res := &APIReply{}
	err := c.Requester.SendRequest(ctx, "health.readiness", &APIArgs{Tags: tags}, res, options...)
	return res, err
}

// Health returns a summation of the health of the node
func (c *Client) Health(ctx context.Context, tags []string, options ...rpc.Option) (*APIReply, error) {
	res := &APIReply{}
	err := c.Requester.SendRequest(ctx, "health.health", &APIArgs{Tags: tags}, res, options...)
	return res, err
}

// Liveness returns if the node is in need of a restart
func (c *Client) Liveness(ctx context.Context, tags []string, options ...rpc.Option) (*APIReply, error) {
	res := &APIReply{}
	err := c.Requester.SendRequest(ctx, "health.liveness", &APIArgs{Tags: tags}, res, options...)
	return res, err
}

// AwaitReady polls the node every [freq] until the node reports ready.
// Only returns an error if [ctx] returns an error.
func AwaitReady(ctx context.Context, c *Client, freq time.Duration, tags []string, options ...rpc.Option) (bool, error) {
	return await(ctx, freq, c.Readiness, tags, options...)
}

// AwaitHealthy polls the node every [freq] until the node reports healthy.
// Only returns an error if [ctx] returns an error.
func AwaitHealthy(ctx context.Context, c *Client, freq time.Duration, tags []string, options ...rpc.Option) (bool, error) {
	return await(ctx, freq, c.Health, tags, options...)
}

// AwaitAlive polls the node every [freq] until the node reports liveness.
// Only returns an error if [ctx] returns an error.
func AwaitAlive(ctx context.Context, c *Client, freq time.Duration, tags []string, options ...rpc.Option) (bool, error) {
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
