// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package p2p

import (
	"context"
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils/set"
)

var (
	ErrRequestPending = errors.New("request pending")
	ErrNoPeers        = errors.New("no peers")
)

// AppResponseCallback is called upon receiving an AppResponse for an AppRequest
// issued by Client.
// Callers should check [err] to see whether the AppRequest failed or not.
type AppResponseCallback func(
	ctx context.Context,
	nodeID ids.NodeID,
	responseBytes []byte,
	err error,
)

// CrossChainAppResponseCallback is called upon receiving an
// CrossChainAppResponse for a CrossChainAppRequest issued by Client.
// Callers should check [err] to see whether the AppRequest failed or not.
type CrossChainAppResponseCallback func(
	ctx context.Context,
	chainID ids.ID,
	responseBytes []byte,
	err error,
)

type Client struct {
	handlerID     uint64
	handlerPrefix []byte
	router        *Router
	sender        common.AppSender
	// nodeSampler is used to select nodes to route AppRequestAny to
	nodeSampler NodeSampler
}

// AppRequestAny issues an AppRequest to an arbitrary node decided by Client.
// If a specific node needs to be requested, use AppRequest instead.
// See AppRequest for more docs.
func (c *Client) AppRequestAny(
	ctx context.Context,
	appRequestBytes []byte,
	onResponse AppResponseCallback,
) error {
	sampled := c.nodeSampler.Sample(ctx, 1)
	if len(sampled) != 1 {
		return ErrNoPeers
	}

	nodeIDs := set.Of(sampled...)
	return c.AppRequest(ctx, nodeIDs, appRequestBytes, onResponse)
}

// AppRequest issues an arbitrary request to a node.
// [onResponse] is invoked upon an error or a response.
func (c *Client) AppRequest(
	ctx context.Context,
	nodeIDs set.Set[ids.NodeID],
	appRequestBytes []byte,
	onResponse AppResponseCallback,
) error {
	c.router.lock.Lock()
	defer c.router.lock.Unlock()

	appRequestBytes = c.prefixMessage(appRequestBytes)
	for nodeID := range nodeIDs {
		requestID := c.router.requestID
		if _, ok := c.router.pendingAppRequests[requestID]; ok {
			return fmt.Errorf(
				"failed to issue request with request id %d: %w",
				requestID,
				ErrRequestPending,
			)
		}

		if err := c.sender.SendAppRequest(
			ctx,
			set.Of(nodeID),
			requestID,
			appRequestBytes,
		); err != nil {
			return err
		}

		c.router.pendingAppRequests[requestID] = pendingAppRequest{
			AppResponseCallback: onResponse,
			metrics:             c.router.handlers[c.handlerID].metrics,
		}
		c.router.requestID += 2
	}

	return nil
}

// AppGossip sends a gossip message to a random set of peers.
func (c *Client) AppGossip(
	ctx context.Context,
	appGossipBytes []byte,
) error {
	return c.sender.SendAppGossip(
		ctx,
		c.prefixMessage(appGossipBytes),
	)
}

// AppGossipSpecific sends a gossip message to a predetermined set of peers.
func (c *Client) AppGossipSpecific(
	ctx context.Context,
	nodeIDs set.Set[ids.NodeID],
	appGossipBytes []byte,
) error {
	return c.sender.SendAppGossipSpecific(
		ctx,
		nodeIDs,
		c.prefixMessage(appGossipBytes),
	)
}

// CrossChainAppRequest sends a cross chain app request to another vm.
// [onResponse] is invoked upon an error or a response.
func (c *Client) CrossChainAppRequest(
	ctx context.Context,
	chainID ids.ID,
	appRequestBytes []byte,
	onResponse CrossChainAppResponseCallback,
) error {
	c.router.lock.Lock()
	defer c.router.lock.Unlock()

	requestID := c.router.requestID
	if _, ok := c.router.pendingCrossChainAppRequests[requestID]; ok {
		return fmt.Errorf(
			"failed to issue request with request id %d: %w",
			requestID,
			ErrRequestPending,
		)
	}

	if err := c.sender.SendCrossChainAppRequest(
		ctx,
		chainID,
		requestID,
		c.prefixMessage(appRequestBytes),
	); err != nil {
		return err
	}

	c.router.pendingCrossChainAppRequests[requestID] = pendingCrossChainAppRequest{
		CrossChainAppResponseCallback: onResponse,
		metrics:                       c.router.handlers[c.handlerID].metrics,
	}
	c.router.requestID += 2

	return nil
}

// prefixMessage prefixes the original message with the handler identifier
// corresponding to this client.
//
// Only gossip and request messages need to be prefixed.
// Response messages don't need to be prefixed because request ids are tracked
// which map to the expected response handler.
func (c *Client) prefixMessage(src []byte) []byte {
	messageBytes := make([]byte, len(c.handlerPrefix)+len(src))
	copy(messageBytes, c.handlerPrefix)
	copy(messageBytes[len(c.handlerPrefix):], src)
	return messageBytes
}
