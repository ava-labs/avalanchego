// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package p2ptest

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/snow/engine/enginetest"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/set"
)

func NewSelfClient(t *testing.T, ctx context.Context, nodeID ids.NodeID, handler p2p.Handler) *p2p.Client {
	return NewClient(t, ctx, nodeID, handler, nodeID, handler)
}

// NewClient generates a client-server pair and returns the client used to
// communicate with a server with the specified handler
func NewClient(
	t *testing.T,
	ctx context.Context,
	clientNodeID ids.NodeID,
	clientHandler p2p.Handler,
	serverNodeID ids.NodeID,
	serverHandler p2p.Handler,
) *p2p.Client {
	return NewClientWithPeers(
		t,
		ctx,
		clientNodeID,
		clientHandler,
		map[ids.NodeID]p2p.Handler{
			serverNodeID: serverHandler,
		},
	)
}

// NewClientWithPeers generates a client to communicate to a set of peers
func NewClientWithPeers(
	t *testing.T,
	ctx context.Context,
	clientNodeID ids.NodeID,
	clientHandler p2p.Handler,
	peers map[ids.NodeID]p2p.Handler,
) *p2p.Client {
	peers[clientNodeID] = clientHandler

	peerSenders := make(map[ids.NodeID]*enginetest.Sender)
	peerNetworks := make(map[ids.NodeID]*p2p.Network)
	for nodeID := range peers {
		peerSenders[nodeID] = &enginetest.Sender{}
		peerNetwork, err := p2p.NewNetwork(
			logging.NoLog{},
			peerSenders[nodeID],
			prometheus.NewRegistry(),
			"",
		)
		require.NoError(t, err)
		peerNetworks[nodeID] = peerNetwork
	}

	peerSenders[clientNodeID].SendAppGossipF = func(ctx context.Context, sendConfig common.SendConfig, gossipBytes []byte) error {
		// Send the request asynchronously to avoid deadlock when the server
		// sends the response back to the client
		for nodeID := range sendConfig.NodeIDs {
			go func() {
				_ = peerNetworks[nodeID].AppGossip(ctx, nodeID, gossipBytes)
			}()
		}

		return nil
	}

	peerSenders[clientNodeID].SendAppRequestF = func(ctx context.Context, nodeIDs set.Set[ids.NodeID], requestID uint32, requestBytes []byte) error {
		for nodeID := range nodeIDs {
			network, ok := peerNetworks[nodeID]
			if !ok {
				return fmt.Errorf("%s is not connected", nodeID)
			}

			// Send the request asynchronously to avoid deadlock when the server
			// sends the response back to the client
			go func() {
				_ = network.AppRequest(ctx, clientNodeID, requestID, time.Time{}, requestBytes)
			}()
		}

		return nil
	}

	for nodeID := range peers {
		peerSenders[nodeID].SendAppResponseF = func(ctx context.Context, _ ids.NodeID, requestID uint32, responseBytes []byte) error {
			// Send the request asynchronously to avoid deadlock when the server
			// sends the response back to the client
			go func() {
				_ = peerNetworks[clientNodeID].AppResponse(ctx, nodeID, requestID, responseBytes)
			}()

			return nil
		}
	}

	for nodeID := range peers {
		peerSenders[nodeID].SendAppErrorF = func(ctx context.Context, _ ids.NodeID, requestID uint32, errorCode int32, errorMessage string) error {
			go func() {
				_ = peerNetworks[clientNodeID].AppRequestFailed(ctx, nodeID, requestID, &common.AppError{
					Code:    errorCode,
					Message: errorMessage,
				})
			}()

			return nil
		}
	}

	for nodeID := range peers {
		require.NoError(t, peerNetworks[nodeID].Connected(ctx, clientNodeID, nil))
		require.NoError(t, peerNetworks[nodeID].Connected(ctx, nodeID, nil))
		require.NoError(t, peerNetworks[nodeID].AddHandler(0, peers[nodeID]))
	}

	peerSampler := p2p.PeerSampler{Peers: &p2p.Peers{}}
	for nodeID := range peers {
		peerSampler.Peers.Connected(nodeID)
	}

	return peerNetworks[clientNodeID].NewClient(0, peerSampler)
}
