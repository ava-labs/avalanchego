// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package p2ptest

import (
	"context"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils/set"
)

var _ p2p.ClientInterface = (*Client)(nil)

// Client is a testing implementation of p2p.ClientInterface against a specified
// server handler. The zero value of Client times out AppRequest and drops
// outbound AppGossip messages.
type Client struct {
	NodeID       ids.NodeID
	ServerNodeID ids.NodeID
	Handler      p2p.Handler
}

func (c Client) AppRequestAny(ctx context.Context, appRequestBytes []byte, onResponse p2p.AppResponseCallback) error {
	return c.AppRequest(ctx, nil, appRequestBytes, onResponse)
}

func (c Client) AppRequest(ctx context.Context, _ set.Set[ids.NodeID], appRequestBytes []byte, onResponse p2p.AppResponseCallback) error {
	if c.Handler == nil {
		onResponse(ctx, c.ServerNodeID, nil, common.ErrTimeout)
		return nil
	}

	responseBytes, err := c.Handler.AppRequest(ctx, c.NodeID, time.Time{}, appRequestBytes)
	if err != nil {
		onResponse(ctx, c.ServerNodeID, nil, err)
		return nil
	}

	onResponse(ctx, c.ServerNodeID, responseBytes, nil)
	return nil
}

func (c Client) AppGossip(ctx context.Context, _ common.SendConfig, appGossipBytes []byte) error {
	if c.Handler == nil {
		return nil
	}

	c.Handler.AppGossip(ctx, c.NodeID, appGossipBytes)
	return nil
}
