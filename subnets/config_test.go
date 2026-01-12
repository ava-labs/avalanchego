// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package subnets

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
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
			name: "invalid consensus parameters",
			s: Config{
				ConsensusParameters: snowball.Parameters{
					K:               2,
					AlphaPreference: 1,
				},
			},
			expectedErr: snowball.ErrParametersInvalid,
		},
		{
			name: "invalid allowed node IDs",
			s: Config{
				AllowedNodes:        set.Of(ids.GenerateTestNodeID()),
				ValidatorOnly:       false,
				ConsensusParameters: validParameters,
			},
			expectedErr: errAllowedNodesWhenNotValidatorOnly,
		},
		{
			name: "valid",
			s: Config{
				ConsensusParameters: validParameters,
				ValidatorOnly:       false,
			},
			expectedErr: nil,
		},
		{
			name: "valid relayer config",
			s: Config{
				ConsensusParameters: validParameters,
				RelayerIDs: set.Of(
					ids.GenerateTestNodeID(),
					ids.GenerateTestNodeID(),
				),
			},
			expectedErr: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.s.Valid()
			require.ErrorIs(t, err, tt.expectedErr)
		})
	}
}

func TestRelayerIDsUnmarshal(t *testing.T) {
	// Generate test node IDs
	nodeID1 := ids.GenerateTestNodeID()
	nodeID2 := ids.GenerateTestNodeID()

	tests := []struct {
		name            string
		json            string
		expectedLen     int
		expectedRelayer bool
	}{
		{
			name:            "empty relayerIDs array",
			json:            `{"relayerIDs":[]}`,
			expectedLen:     0,
			expectedRelayer: false,
		},
		{
			name:            "no relayerIDs field",
			json:            `{}`,
			expectedLen:     0,
			expectedRelayer: false,
		},
		{
			name:            "single relayer ID",
			json:            fmt.Sprintf(`{"relayerIDs":[%q]}`, nodeID1.String()),
			expectedLen:     1,
			expectedRelayer: true,
		},
		{
			name:            "multiple relayer IDs",
			json:            fmt.Sprintf(`{"relayerIDs":[%q,%q]}`, nodeID1.String(), nodeID2.String()),
			expectedLen:     2,
			expectedRelayer: true,
		},
		{
			name:            "same relayer ID twice",
			json:            fmt.Sprintf(`{"relayerIDs":[%q,%q]}`, nodeID1.String(), nodeID1.String()),
			expectedLen:     1,
			expectedRelayer: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			var config Config
			require.NoError(json.Unmarshal([]byte(tt.json), &config))
			require.Equal(tt.expectedLen, config.RelayerIDs.Len())
			require.Equal(tt.expectedRelayer, config.IsRelayerMode())
		})
	}
}
