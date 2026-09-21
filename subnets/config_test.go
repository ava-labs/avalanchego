// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package subnets

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/consensus/simplex"
	"github.com/ava-labs/avalanchego/snow/consensus/snowball"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/utils/units"
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

func TestValidParameters(t *testing.T) {
	tests := []struct {
		name        string
		s           Config
		expectedErr error
	}{
		{
			name: "invalid snow consensus parameters",
			s: Config{
				SnowParameters: &snowball.Parameters{
					K:               2,
					AlphaPreference: 1,
				},
			},
			expectedErr: snowball.ErrParametersInvalid,
		},
		{
			name: "invalid allowed node IDs",
			s: Config{
				AllowedNodes:   set.Of(ids.GenerateTestNodeID()),
				ValidatorOnly:  false,
				SnowParameters: &validParameters,
			},
			expectedErr: errAllowedNodesWhenNotValidatorOnly,
		},
		{
			name: "valid snowball parameters",
			s: Config{
				SnowParameters: &validParameters,
				ValidatorOnly:  false,
			},
			expectedErr: nil,
		},
		{
			name: "valid simplex parameters",
			s: Config{
				SimplexParameters: &simplex.Parameters{
					MaxNetworkDelay:    1 * time.Second,
					MaxRebroadcastWait: 1 * time.Second,
					InitialValidators:  []simplex.ValidatorInfo{{NodeID: ids.GenerateTestNodeID(), PublicKey: []byte{0x01}}},
				},
				ValidatorOnly: false,
			},
			expectedErr: nil,
		},
		{
			name:        "no consensus parameters",
			s:           Config{},
			expectedErr: errNoParametersSet,
		},
		{
			name: "invalid simplex parameters",
			s: Config{
				SimplexParameters: &simplex.Parameters{
					MaxNetworkDelay:    -1,
					MaxRebroadcastWait: -10,
				},
			},
			expectedErr: simplex.ErrInvalidParameters,
		},
		{
			name: "valid largeMessages",
			s: Config{
				ValidatorOnly:  true,
				LargeMessages:  &LargeMessagesConfig{MaxMessageSize: 160 * units.MiB},
				SnowParameters: &validParameters,
			},
			expectedErr: nil,
		},
		{
			name: "largeMessages on public subnet",
			s: Config{
				LargeMessages:  &LargeMessagesConfig{MaxMessageSize: 160 * units.MiB},
				SnowParameters: &validParameters,
			},
			expectedErr: ErrLargeMessagesWhenNotValidatorOnly,
		},
		{
			name: "memberCA on public subnet",
			s: Config{
				MemberCAPEMs:   []string{"-----BEGIN CERTIFICATE-----"},
				SnowParameters: &validParameters,
			},
			expectedErr: ErrMemberCAWhenNotValidatorOnly,
		},
		{
			name: "largeMessages with member CA on public subnet",
			s: Config{
				MemberCAPEMs:   []string{"-----BEGIN CERTIFICATE-----"},
				LargeMessages:  &LargeMessagesConfig{MaxMessageSize: 160 * units.MiB},
				SnowParameters: &validParameters,
			},
			expectedErr: ErrLargeMessagesWhenNotValidatorOnly,
		},
		{
			name: "largeMessages at the default size",
			s: Config{
				ValidatorOnly:  true,
				LargeMessages:  &LargeMessagesConfig{MaxMessageSize: constants.DefaultMaxMessageSize},
				SnowParameters: &validParameters,
			},
			expectedErr: ErrLargeMessageSizeTooSmall,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.s.ValidParameters()
			require.ErrorIs(t, err, tt.expectedErr)
		})
	}
}

func TestMaxAncestorsBytes(t *testing.T) {
	require := require.New(t)

	var config Config
	require.Equal(constants.MaxContainersLen, config.MaxAncestorsBytes())

	config.LargeMessages = &LargeMessagesConfig{MaxMessageSize: 80 * constants.DefaultMaxMessageSize}
	require.Equal(64*constants.DefaultMaxMessageSize, config.MaxAncestorsBytes())
}

// TestValidConsensusConfiguration tests the three meaningful states of
// ValidConsensusConfiguration: zero, one, or more than one consensus parameter
// set.
func TestValidConsensusConfiguration(t *testing.T) {
	tests := []struct {
		name    string
		config  Config
		wantErr error
	}{
		{
			name:   "none set",
			config: Config{},
		},
		{
			name:   "one set",
			config: Config{SimplexParameters: &simplex.Parameters{}},
		},
		{
			name:    "two set",
			config:  Config{SimplexParameters: &simplex.Parameters{}, SnowParameters: &snowball.Parameters{}},
			wantErr: ErrTooManyConsensusParameters,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.ValidConsensusConfiguration()
			require.ErrorIs(t, err, tt.wantErr)
		})
	}
}
