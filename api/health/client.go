// Copyright (C) 2022, Chain4Travel AG. All rights reserved.
//
// This file is a derived work, based on ava-labs code whose
// original notices appear below.
//
// It is distributed under the same license conditions as the
// original code from which it is derived.
//
// Much love to the original authors for their work.
// **********************************************************

// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package health

import (
	"context"
	"time"

	"github.com/chain4travel/caminogo/utils/rpc"
)

// Interface compliance
var _ Client = &client{}

// Client interface for Avalanche Health API Endpoint
type Client interface {
	// Readiness returns if the node has finished initialization
	Readiness(context.Context) (*APIHealthReply, error)
	// Health returns a summation of the health of the node
	Health(context.Context) (*APIHealthReply, error)
	// Liveness returns if the node is in need of a restart
	Liveness(context.Context) (*APIHealthReply, error)
	// AwaitHealthy queries the Health endpoint with a pause of [interval]
	// in between checks and returns early if Health returns healthy
	AwaitHealthy(ctx context.Context, freq time.Duration) (bool, error)
}

// Client implementation for Avalanche Health API Endpoint
type client struct {
	requester rpc.EndpointRequester
}

// NewClient returns a client to interact with Health API endpoint
func NewClient(uri string) Client {
	return &client{
		requester: rpc.NewEndpointRequester(uri, "/ext/health", "health"),
	}
}

func (c *client) Readiness(ctx context.Context) (*APIHealthReply, error) {
	res := &APIHealthReply{}
	err := c.requester.SendRequest(ctx, "readiness", struct{}{}, res)
	return res, err
}

func (c *client) Health(ctx context.Context) (*APIHealthReply, error) {
	res := &APIHealthReply{}
	err := c.requester.SendRequest(ctx, "health", struct{}{}, res)
	return res, err
}

func (c *client) Liveness(ctx context.Context) (*APIHealthReply, error) {
	res := &APIHealthReply{}
	err := c.requester.SendRequest(ctx, "liveness", struct{}{}, res)
	return res, err
}

func (c *client) AwaitHealthy(ctx context.Context, freq time.Duration) (bool, error) {
	ticker := time.NewTicker(freq)
	defer ticker.Stop()

	for {
		res, err := c.Health(ctx)
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
