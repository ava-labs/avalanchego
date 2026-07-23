// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package node

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/set"
)

func TestBootstrapMaxContainersBytes(t *testing.T) {
	tests := map[string]struct {
		config network.LargeMessageConfig
		want   int
	}{
		"default message size": {
			want: constants.MaxContainersLen,
		},
		"elevated message size": {
			config: network.LargeMessageConfig{
				Enabled:        true,
				MaxMessageSize: 80 * constants.DefaultMaxMessageSize,
				Allowlist:      set.Of(ids.GenerateTestNodeID()),
			},
			want: 64 * constants.DefaultMaxMessageSize,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			require.Equal(t, test.want, bootstrapMaxContainersBytes(test.config))
		})
	}
}
