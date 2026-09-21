// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package p2p

import (
	"context"
	"sync"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/set"
)

// Added to a response's elapsed time to avoid dividing by zero.
const bandwidthEpsilon = 1e-6

// AppResponseVerifier is called upon receiving an AppResponse for an AppRequest
// issued by TrackingClient, and reports whether the peer served it usefully.
// Callers should check [err] to see whether the AppRequest failed or not.
//
// A non-nil return registers a failure against nodeID, so return nil for a
// failure that is not the peer's fault.
type AppResponseVerifier func(
	ctx context.Context,
	nodeID ids.NodeID,
	responseBytes []byte,
	err error,
) error

// TrackingClient issues requests through a Client and scores each one against
// its PeerTracker.
type TrackingClient struct {
	client *Client
	peers  *PeerTracker
}

// AppRequestAny issues an AppRequest to the peer the PeerTracker selects.
// See [Client.AppRequestAny] for more docs.
func (c *TrackingClient) AppRequestAny(
	ctx context.Context,
	appRequestBytes []byte,
	onResponse AppResponseVerifier,
) error {
	nodeID, ok := c.peers.SelectPeer()
	if !ok {
		return ErrNoPeers
	}

	return c.AppRequest(ctx, set.Of(nodeID), appRequestBytes, onResponse)
}

// AppRequest issues a request to each node in nodeIDs, scoring every one.
// See [Client.AppRequest] for more docs.
func (c *TrackingClient) AppRequest(
	ctx context.Context,
	nodeIDs set.Set[ids.NodeID],
	appRequestBytes []byte,
	onResponse AppResponseVerifier,
) error {
	// One node per call, since each needs its own scoring state. A shared
	// callback would collapse the whole set onto one sync.Once and one start.
	for nodeID := range nodeIDs {
		if err := c.request(ctx, nodeID, appRequestBytes, onResponse); err != nil {
			return err
		}
	}

	return nil
}

// request sends to nodeID and scores the outcome exactly once. A send that
// never leaves the node is balanced here, since nothing else will.
func (c *TrackingClient) request(
	ctx context.Context,
	nodeID ids.NodeID,
	appRequestBytes []byte,
	onResponse AppResponseVerifier,
) error {
	c.peers.RegisterRequest(nodeID)
	start := time.Now()

	var once sync.Once
	registerFailure := func() {
		once.Do(func() {
			c.peers.RegisterFailure(nodeID)
		})
	}

	// Settle the registration if the caller's context ends first, since the
	// reply may never arrive to settle it.
	stop := func() bool { return false }
	// Skipped for a context already done, since firing now would blame the peer
	// for a reply still on its way.
	if ctx.Err() == nil {
		stop = context.AfterFunc(ctx, registerFailure)
	}

	// Scoring always uses nodeID, the node we asked. The chain router keys a
	// response on its sender, so respNodeID agrees with it.
	score := func(
		respCtx context.Context,
		respNodeID ids.NodeID,
		responseBytes []byte,
		appErr error,
	) {
		stop()

		// Taken before onResponse so that the peer is not charged for the cost
		// of validating its own response.
		elapsed := time.Since(start)

		err := onResponse(respCtx, respNodeID, responseBytes, appErr)
		if appErr != nil || err != nil {
			registerFailure()
			return
		}

		once.Do(func() {
			bandwidth := float64(len(responseBytes)) / (elapsed.Seconds() + bandwidthEpsilon)
			c.peers.RegisterResponse(nodeID, bandwidth)
		})
	}

	if err := c.client.AppRequest(ctx, set.Of(nodeID), appRequestBytes, score); err != nil {
		stop()
		registerFailure()
		return err
	}

	return nil
}
