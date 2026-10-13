// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package tracking

import (
	"context"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/utils/set"
)

// Added to a response's elapsed time to avoid dividing by zero.
const bandwidthEpsilon = 1e-6

// ResponseVerifier is called upon receiving an AppResponse for an AppRequest
// issued by [Client]. Callers should check [err] to see whether the
// AppRequest failed or not.
//
// The return scores the peer and nothing else, so report false only when the
// peer is at fault. A failure of our own still counts as served.
type ResponseVerifier func(
	ctx context.Context,
	nodeID ids.NodeID,
	responseBytes []byte,
	err error,
) bool

// PeerScorer selects peers to request from and records how well they served.
type PeerScorer interface {
	SelectPeer() (ids.NodeID, bool)
	RegisterRequest(nodeID ids.NodeID)
	RegisterResponse(nodeID ids.NodeID, bandwidth float64)
	RegisterFailure(nodeID ids.NodeID)
}

func NewClient(n *p2p.Network, handlerID uint64, scorer PeerScorer) *Client {
	// No NodeSampler, since Client selects with scorer and never routes through
	// [p2p.Client.AppRequestAny].
	return &Client{
		handlerID: handlerID,
		client:    n.NewClient(handlerID, nil),
		scorer:    scorer,
	}
}

// Client issues requests through a [p2p.Client] and scores each one against
// its [PeerScorer].
type Client struct {
	handlerID uint64
	client    *p2p.Client
	scorer    PeerScorer
}

// HandlerID returns the ID of the protocol this client sends requests to.
func (c *Client) HandlerID() uint64 {
	return c.handlerID
}

// AppRequestAny issues an AppRequest to the peer the [PeerScorer] selects.
// See [p2p.Client.AppRequestAny] for more docs.
func (c *Client) AppRequestAny(
	ctx context.Context,
	appRequestBytes []byte,
	onResponse ResponseVerifier,
) error {
	nodeID, ok := c.scorer.SelectPeer()
	if !ok {
		return p2p.ErrNoPeers
	}

	return c.AppRequest(ctx, set.Of(nodeID), appRequestBytes, onResponse)
}

// AppRequest issues a request to each node in nodeIDs, scoring every one.
// See [p2p.Client.AppRequest] for more docs.
func (c *Client) AppRequest(
	ctx context.Context,
	nodeIDs set.Set[ids.NodeID],
	appRequestBytes []byte,
	onResponse ResponseVerifier,
) error {
	for nodeID := range nodeIDs {
		if err := c.request(ctx, nodeID, appRequestBytes, onResponse); err != nil {
			return err
		}
	}

	return nil
}

// request registers a request to nodeID, sends it, and scores the outcome. The
// engine answers every request it sends, with a timeout failure if nothing else.
func (c *Client) request(
	ctx context.Context,
	nodeID ids.NodeID,
	appRequestBytes []byte,
	onResponse ResponseVerifier,
) error {
	c.scorer.RegisterRequest(nodeID)
	start := time.Now()

	// Scoring always uses nodeID, the node we asked. The chain router keys a
	// response on its sender, so respNodeID agrees with it.
	score := func(
		respCtx context.Context,
		respNodeID ids.NodeID,
		responseBytes []byte,
		appErr error,
	) {
		// Taken before onResponse so that the peer is not charged for the cost
		// of validating its own response.
		elapsed := time.Since(start)

		// Callers deliver their result from inside onResponse, so a
		// short-circuit that skips it hangs them.
		ok := onResponse(respCtx, respNodeID, responseBytes, appErr)
		if appErr != nil || !ok {
			c.scorer.RegisterFailure(nodeID)
			return
		}

		bandwidth := float64(len(responseBytes)) / (elapsed.Seconds() + bandwidthEpsilon)
		c.scorer.RegisterResponse(nodeID, bandwidth)
	}

	if err := c.client.AppRequest(ctx, set.Of(nodeID), appRequestBytes, score); err != nil {
		// The request never left the node, so balance the registration above.
		c.scorer.RegisterFailure(nodeID)
		return err
	}

	return nil
}
