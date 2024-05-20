package p2p

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/set"
)

// UniformSampler should sample a random peer that matches the configured set of
// SamplingFilter
func TestUniformSampler(t *testing.T) {
	peerID := ids.GenerateTestNodeID()

	tests := []struct {
		name        string
		peers       []ids.NodeID
		filters     []SamplingFilter
		wantSampled ids.NodeID
		wantOk      bool
	}{
		{
			name: "no peers - no filters",
		},
		{
			name: "no peers - filters",
		},
		{
			name:  "has peers - no match",
			peers: []ids.NodeID{peerID},
			filters: []SamplingFilter{
				&testFilter{
					nodeIDs: set.Of(ids.GenerateTestNodeID()),
				},
			},
		},
		{
			name:  "has peers - matches filter",
			peers: []ids.NodeID{peerID},
			filters: []SamplingFilter{
				&testFilter{
					nodeIDs: set.Of(peerID),
				},
			},
			wantSampled: peerID,
			wantOk:      true,
		},
		{
			name:  "has peers - matches multiple filters",
			peers: []ids.NodeID{peerID},
			filters: []SamplingFilter{
				&testFilter{
					nodeIDs: set.Of(peerID),
				},
				&testFilter{
					nodeIDs: set.Of(peerID),
				},
			},
			wantSampled: peerID,
			wantOk:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			sampler := NewUniformSampler(tt.filters...)

			gotSampled, gotOk := sampler.Sample(context.Background(), tt.peers)
			require.Equal(tt.wantOk, gotOk)
			require.Equal(tt.wantSampled, gotSampled)
		})
	}
}
