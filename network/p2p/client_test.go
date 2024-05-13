// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package p2p

import (
	"context"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/version"
)

var _ SamplingFilter = (*testFilter)(nil)

// AppRequestAny should send a request to a random peer that matches the
// configured SamplingFilter
func TestClientAppRequestAny(t *testing.T) {
	peerID := ids.GenerateTestNodeID()

	tests := []struct {
		name          string
		connected     []ids.NodeID
		filters       []SamplingFilter
		expectedPeers []ids.NodeID
		expected      error
	}{
		{
			name:     "no peers - no peers connected",
			expected: ErrNoPeers,
		},
		{
			name:      "no peers - no peers matching filter",
			connected: []ids.NodeID{peerID},
			filters: []SamplingFilter{
				testFilter{},
			},
			expected: ErrNoPeers,
		},
		{
			name:          "has peers",
			connected:     []ids.NodeID{peerID},
			expectedPeers: []ids.NodeID{peerID},
		},
		{
			name:      "has peers in filter",
			connected: []ids.NodeID{peerID},
			filters: []SamplingFilter{
				testFilter{
					nodeIDs: set.Of(peerID),
				},
			},
			expectedPeers: []ids.NodeID{peerID},
		},
		{
			name:      "has peers in multiple filters",
			connected: []ids.NodeID{peerID},
			filters: []SamplingFilter{
				testFilter{
					nodeIDs: set.Of(peerID),
				},
				testFilter{
					nodeIDs: set.Of(peerID),
				},
			},
			expectedPeers: []ids.NodeID{peerID},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			sent := set.Set[ids.NodeID]{}
			sender := &common.SenderTest{
				SendAppRequestF: func(_ context.Context, nodeIDs set.Set[ids.NodeID], _ uint32, _ []byte) error {
					sent = nodeIDs
					return nil
				},
			}

			n, err := NewNetwork(logging.NoLog{}, sender, prometheus.NewRegistry(), "")
			require.NoError(err)
			for _, peer := range tt.connected {
				require.NoError(n.Connected(context.Background(), peer, &version.Application{}))
			}

			client := n.NewClient(1, WithSamplingFilters(tt.filters...))

			err = client.AppRequestAny(context.Background(), []byte("foobar"), nil)
			require.ErrorIs(err, tt.expected)
			require.Subset(tt.expectedPeers, sent.List())

			if len(tt.expectedPeers) > 0 {
				require.Len(sent, 1)
			} else {
				require.Empty(sent)
			}
		})
	}
}

type testFilter struct {
	nodeIDs set.Set[ids.NodeID]
}

func (t testFilter) Filter(_ context.Context, nodeID ids.NodeID) bool {
	return t.nodeIDs.Contains(nodeID)
}
