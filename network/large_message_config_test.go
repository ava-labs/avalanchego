// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/set"
)

func TestLargeMessageConfigMaxAncestorsBytes(t *testing.T) {
	tests := map[string]struct {
		config LargeMessageConfig
		want   int
	}{
		"default message size": {
			want: constants.MaxContainersLen,
		},
		"elevated message size": {
			config: LargeMessageConfig{
				Enabled:        true,
				MaxMessageSize: 80 * constants.DefaultMaxMessageSize,
				Allowlist:      set.Of(ids.GenerateTestNodeID()),
			},
			want: 64 * constants.DefaultMaxMessageSize,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, test.want, test.config.MaxAncestorsBytes())
		})
	}
}

func TestLargeMessageConfigAppliesTo(t *testing.T) {
	allowedPeer := ids.GenerateTestNodeID()
	otherPeer := ids.GenerateTestNodeID()

	tests := map[string]struct {
		config LargeMessageConfig
		nodeID ids.NodeID
		want   bool
	}{
		"allowlisted peer": {
			config: LargeMessageConfig{
				Allowlist: set.Of(allowedPeer),
			},
			nodeID: allowedPeer,
			want:   true,
		},
		"peer outside allowlist": {
			config: LargeMessageConfig{
				Allowlist: set.Of(allowedPeer),
			},
			nodeID: otherPeer,
			want:   false,
		},
		"all peers": {
			config: LargeMessageConfig{
				AllowAll: true,
			},
			nodeID: otherPeer,
			want:   true,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, test.want, test.config.AppliesTo(test.nodeID))
		})
	}
}
