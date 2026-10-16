// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"context"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/snow/engine/enginetest"
	"github.com/ava-labs/avalanchego/snow/snowtest"
	"github.com/ava-labs/avalanchego/utils/set"
)

var wantSyncHandlerIDs = []uint64{
	p2p.EVMLeafRequestHandlerID,
	p2p.EVMCodeRequestHandlerID,
	p2p.EVMBlockRequestHandlerID,
	p2p.EVMAtomicLeafRequestHandlerID,
}

func TestWithAllowedTrackedPeers(t *testing.T) {
	peer := ids.GenerateTestNodeID()

	tests := []struct {
		name         string
		trackedIDs   set.Set[ids.NodeID]
		expectedSize int
	}{
		{
			name:         "empty",
			expectedSize: 1,
		},
		{
			name: "non_empty_filtered",
			trackedIDs: set.Of(
				ids.GenerateTestNodeID(),
				ids.GenerateTestNodeID(),
			),
			expectedSize: 0,
		},
		{
			name: "includeFilter",
			trackedIDs: set.Of(
				peer,
				ids.GenerateTestNodeID(),
			),
			expectedSize: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			snowCtx := snowtest.Context(t, snowtest.CChainID)
			net, err := New(
				snowCtx,
				&enginetest.Sender{},
				WithAllowedTrackedPeers(tt.trackedIDs),
			)
			require.NoError(t, err, "New()")

			require.NoError(t, net.Connected(t.Context(), peer, nil), "Connected()")
			require.True(t, net.Peers.Has(peer), "Peers.Has() connected peer")

			for _, handlerID := range wantSyncHandlerIDs {
				require.Equalf(t, tt.expectedSize, net.peerTrackers[handlerID].Size(), "peerTrackers[%d].Size()", handlerID)
			}
		})
	}
}

func TestTrackingClientsAreIsolated(t *testing.T) {
	for _, requestedID := range wantSyncHandlerIDs {
		t.Run(strconv.FormatUint(requestedID, 10), func(t *testing.T) {
			net, err := New(snowtest.Context(t, snowtest.CChainID), enginetest.SenderStub{})
			require.NoError(t, err, "New()")
			peer := ids.GenerateTestNodeID()
			require.NoError(t, net.Connected(t.Context(), peer, nil), "Connected()")

			client, err := net.TrackingClient(requestedID)
			require.NoErrorf(t, err, "TrackingClient(%d)", requestedID)
			onResponse := func(context.Context, ids.NodeID, []byte, error) bool { return true }
			require.NoError(t, client.AppRequestAny(t.Context(), []byte("request"), onResponse), "AppRequestAny()")

			for _, handlerID := range wantSyncHandlerIDs {
				want := set.Set[ids.NodeID]{}
				if handlerID == requestedID {
					want = set.Of(peer)
				}
				require.Equalf(t, want, net.peerTrackers[handlerID].TrackedPeers(), "peerTrackers[%d].TrackedPeers()", handlerID)
			}
		})
	}
}

func TestTrackingClientUnknownProtocol(t *testing.T) {
	net, err := New(snowtest.Context(t, snowtest.CChainID), enginetest.SenderStub{})
	require.NoError(t, err, "New()")

	_, err = net.TrackingClient(p2p.TxGossipHandlerID)
	require.ErrorIs(t, err, errUnknownSyncProtocol, "TrackingClient(TxGossipHandlerID)")
}
