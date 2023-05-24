// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package subnets

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/consensus/avalanche"
	"github.com/ava-labs/avalanchego/snow/consensus/snowball"
	"github.com/ava-labs/avalanchego/utils/set"
)

var validParameters = avalanche.Parameters{
	Parents:   2,
	BatchSize: 1,
	Parameters: snowball.Parameters{
		K:                     1,
		Alpha:                 1,
		BetaVirtuous:          1,
		BetaRogue:             1,
		ConcurrentRepolls:     1,
		OptimalProcessing:     1,
		MaxOutstandingItems:   1,
		MaxItemProcessingTime: 1,
	},
}

func TestValid(t *testing.T) {
	tests := []struct {
		name string
		s    Config
		err  string
	}{
		{
			name: "invalid consensus parameters",
			s: Config{
				ConsensusParameters: avalanche.Parameters{
					Parameters: snowball.Parameters{
						K:     2,
						Alpha: 1,
					},
				},
			},
			err: "consensus parameters are invalid",
		},
		{
			name: "invalid allowed node IDs",
			s: Config{
				AllowedNodes:        set.Set[ids.NodeID]{ids.GenerateTestNodeID(): struct{}{}},
				ValidatorOnly:       false,
				ConsensusParameters: validParameters,
			},
			err: errAllowedNodesWhenNotValidatorOnly.Error(),
		},
		{
			name: "valid",
			s: Config{
				ConsensusParameters: validParameters,
				ValidatorOnly:       false,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.s.Valid()
			if tt.err != "" {
				require.ErrorContains(t, err, tt.err)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
