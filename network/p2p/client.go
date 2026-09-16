// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package p2p

import (
	"context"
	"errors"
	"fmt"

	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/message"
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

type Client struct {
	handlerIDStr  string
	handlerPrefix []byte
	router        *router
	sender        common.AppSender
	// nodeSampler is used to select nodes to route Client.AppRequestAny to
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
	// Cancellation is removed from this context to avoid erroring unexpectedly.
	// SendAppRequest should be non-blocking and any error other than context
	// cancellation is unexpected.
	//
	// This guarantees that the router should never receive an unexpected
	// AppResponse.
	ctxWithoutCancel := context.WithoutCancel(ctx)

	c.router.lock.Lock()
	defer c.router.lock.Unlock()

	appRequestBytes = PrefixMessage(c.handlerPrefix, appRequestBytes)
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
			ctxWithoutCancel,
			set.Of(nodeID),
			requestID,
			appRequestBytes,
		); err != nil {
			c.router.log.Error("unexpected error when sending message",
				zap.Stringer("op", message.AppRequestOp),
				zap.Stringer("nodeID", nodeID),
				zap.Uint32("requestID", requestID),
				zap.Error(err),
			)
			return err
		}

		c.router.pendingAppRequests[requestID] = pendingAppRequest{
			handlerID: c.handlerIDStr,
			callback:  onResponse,
		}
		c.router.requestID += 2
	}

	return nil
}

// AppGossip sends a gossip message to a random set of peers.
func (c *Client) AppGossip(
	ctx context.Context,
	config common.SendConfig,
	appGossipBytes []byte,
) error {
	// Cancellation is removed from this context to avoid erroring unexpectedly.
	// SendAppGossip should be non-blocking and any error other than context
	// cancellation is unexpected.
	ctxWithoutCancel := context.WithoutCancel(ctx)

	return c.sender.SendAppGossip(
		ctxWithoutCancel,
		config,
		PrefixMessage(c.handlerPrefix, appGossipBytes),
	)
}

// PrefixMessage prefixes the original message with the protocol identifier.
//
// Only gossip and request messages need to be prefixed.
// Response messages don't need to be prefixed because request ids are tracked
// which map to the expected response handler.
func PrefixMessage(prefix, msg []byte) []byte {
	messageBytes := make([]byte, len(prefix)+len(msg))
	copy(messageBytes, prefix)
	copy(messageBytes[len(prefix):], msg)
	return messageBytes
}
