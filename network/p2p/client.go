// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package p2p

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

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

// Added to a response's elapsed time to avoid dividing by zero.
const bandwidthEpsilon = 1e-6

// AppResponseCallback is called upon receiving an AppResponse for an AppRequest
// issued by Client.
// Callers should check [err] to see whether the AppRequest failed or not.
//
// A non-nil return de-scores nodeID on a tracked Client, so return nil for a
// failure that is not the peer's fault. It never reaches the engine.
type AppResponseCallback func(
	ctx context.Context,
	nodeID ids.NodeID,
	responseBytes []byte,
	err error,
) error

type Client struct {
	handlerIDStr  string
	handlerPrefix []byte
	router        *router
	sender        common.AppSender
	// nodeSampler is used to select nodes to route Client.AppRequestAny to
	nodeSampler NodeSampler
	// peers scores every request this Client issues. nil when untracked.
	peers *PeerTracker
}

// track marks a request to nodeID outstanding, returning a callback that scores
// it once. Call after a successful send, so an unsent request scores nothing.
func (c *Client) track(
	ctx context.Context,
	nodeID ids.NodeID,
	onResponse AppResponseCallback,
) AppResponseCallback {
	if c.peers == nil {
		return onResponse
	}

	c.peers.RegisterRequest(nodeID)
	start := time.Now()

	var once sync.Once
	registerFailure := func() {
		once.Do(func() {
			c.peers.RegisterFailure(nodeID)
		})
	}

	// A request can outlive its callback. vms/transitionvm drops both the
	// response and the failure for requests issued before a transition.
	stop := func() bool { return false }
	if ctx.Err() == nil {
		stop = context.AfterFunc(ctx, registerFailure)
	}

	// Scoring always uses nodeID, the node we asked. The chain router keys a
	// response on its sender, so respNodeID agrees with it.
	return func(
		respCtx context.Context,
		respNodeID ids.NodeID,
		responseBytes []byte,
		appErr error,
	) error {
		stop()

		// Taken before onResponse so that the peer is not charged for the cost
		// of validating its own response.
		elapsed := time.Since(start)

		err := onResponse(respCtx, respNodeID, responseBytes, appErr)
		if appErr != nil || err != nil {
			registerFailure()
			return err
		}

		once.Do(func() {
			bandwidth := float64(len(responseBytes)) / (elapsed.Seconds() + bandwidthEpsilon)
			c.peers.RegisterResponse(nodeID, bandwidth)
		})
		return nil
	}
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

		// Registering here is safe because the response path needs the router
		// lock this call already holds, so it cannot arrive first.
		c.router.pendingAppRequests[requestID] = pendingAppRequest{
			handlerID: c.handlerIDStr,
			callback:  c.track(ctx, nodeID, onResponse),
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
