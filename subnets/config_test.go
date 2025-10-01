// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package subnets

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/consensus/simplex"
	"github.com/ava-labs/avalanchego/snow/consensus/snowball"
	"github.com/ava-labs/avalanchego/utils/set"
)

var validParameters = snowball.Parameters{
	K:                     1,
	AlphaPreference:       1,
	AlphaConfidence:       1,
	Beta:                  1,
	ConcurrentRepolls:     1,
	OptimalProcessing:     1,
	MaxOutstandingItems:   1,
	MaxItemProcessingTime: 1,
}

func TestValid(t *testing.T) {
	tests := []struct {
		name        string
		s           Config
		expectedErr error
	}{
		{
			name: "invalid snowball consensus parameters",
			s: Config{
				ConsensusConfig: ConsensusConfig{
					SnowballParams: &snowball.Parameters{
						K:               2,
						AlphaPreference: 1,
					},
				},
			},
			expectedErr: snowball.ErrParametersInvalid,
		},
		{
			name: "invalid allowed node IDs",
			s: Config{
				AllowedNodes:  set.Of(ids.GenerateTestNodeID()),
				ValidatorOnly: false,
				ConsensusConfig: ConsensusConfig{
					SnowballParams: &validParameters,
				},
			},
			expectedErr: errAllowedNodesWhenNotValidatorOnly,
		},
		{
			name: "valid snowball parameters",
			s: Config{
				ConsensusConfig: ConsensusConfig{
					SnowballParams: &validParameters,
				},
				ValidatorOnly: false,
			},
			expectedErr: nil,
		},
		{
			name: "valid simplex parameters",
			s: Config{
				ConsensusConfig: ConsensusConfig{
					SimplexParams: &simplex.Parameters{
						MaxProposalWait:    1 * time.Second,
						MaxRebroadcastWait: 1 * time.Second,
					},
				},
				ValidatorOnly: false,
			},
			expectedErr: nil,
		},
		{
			name: "no consensus parameters",
			s: Config{
				ConsensusConfig: ConsensusConfig{},
			},
			expectedErr: errMissingConsensusParameters,
		},
		{
			name: "both consensus parameters set",
			s: Config{
				ConsensusConfig: ConsensusConfig{
					SnowballParams: &validParameters,
					SimplexParams:  &simplex.Parameters{},
				},
			},
			expectedErr: errTwoConfigs,
		},
		{
			name: "invalid simplex parameters",
			s: Config{
				ConsensusConfig: ConsensusConfig{
					SimplexParams: &simplex.Parameters{
						MaxProposalWait:    -1,
						MaxRebroadcastWait: -10,
					},
				},
			},
			expectedErr: simplex.ErrInvalidParameters,
		},
		{
			name: "empty simplex parameters",
			s: Config{
				ConsensusConfig: ConsensusConfig{
					SimplexParams: &simplex.Parameters{
						MaxProposalWait:    0,
						MaxRebroadcastWait: 0,
					},
				},
			},
			expectedErr: simplex.ErrInvalidParameters,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.s.Valid()
			require.ErrorIs(t, err, tt.expectedErr)
		})
	}
}
