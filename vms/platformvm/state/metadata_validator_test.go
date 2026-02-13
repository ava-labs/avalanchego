// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/utils/wrappers"
)

func TestParseValidatorMetadata(t *testing.T) {
	type test struct {
		name        string
		bytes       []byte
		expected    *validatorMetadata
		expectedErr error
	}
	tests := []test{
		{
			name:        "nil",
			bytes:       nil,
			expected:    &validatorMetadata{},
			expectedErr: nil,
		},
		{
			name:        "nil",
			bytes:       []byte{},
			expected:    &validatorMetadata{},
			expectedErr: nil,
		},
		{
			name: "potential reward only",
			bytes: []byte{
				0x00, 0x00, 0x00, 0x00, 0x00, 0x01, 0x86, 0xA0,
			},
			expected: &validatorMetadata{
				PotentialReward: 100000,
			},
			expectedErr: nil,
		},
		{
			name: "uptime + potential reward",
			bytes: []byte{
				// codec version
				0x00, 0x00,
				// up duration
				0x00, 0x00, 0x00, 0x00, 0x00, 0x5B, 0x8D, 0x80,
				// last updated
				0x00, 0x00, 0x00, 0x00, 0x00, 0x0D, 0xBB, 0xA0,
				// potential reward
				0x00, 0x00, 0x00, 0x00, 0x00, 0x01, 0x86, 0xA0,
			},
			expected: &validatorMetadata{
				UpDuration:      6000000,
				LastUpdated:     900000,
				PotentialReward: 100000,
			},
			expectedErr: nil,
		},
		{
			name: "uptime + potential reward + potential delegatee reward",
			bytes: []byte{
				// codec version
				0x00, 0x00,
				// up duration
				0x00, 0x00, 0x00, 0x00, 0x00, 0x5B, 0x8D, 0x80,
				// last updated
				0x00, 0x00, 0x00, 0x00, 0x00, 0x0D, 0xBB, 0xA0,
				// potential reward
				0x00, 0x00, 0x00, 0x00, 0x00, 0x01, 0x86, 0xA0,
				// potential delegatee reward
				0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x4E, 0x20,
			},
			expected: &validatorMetadata{
				UpDuration:               6000000,
				LastUpdated:              900000,
				PotentialReward:          100000,
				PotentialDelegateeReward: 20000,
			},
			expectedErr: nil,
		},
		{
			name: "uptime + potential reward + potential delegatee reward + staker start time",
			bytes: []byte{
				// codec version
				0x00, 0x01,
				// up duration
				0x00, 0x00, 0x00, 0x00, 0x00, 0x5B, 0x8D, 0x80,
				// last updated
				0x00, 0x00, 0x00, 0x00, 0x00, 0x0D, 0xBB, 0xA0,
				// potential reward
				0x00, 0x00, 0x00, 0x00, 0x00, 0x01, 0x86, 0xA0,
				// potential delegatee reward
				0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x4E, 0x20,
				// staker start time
				0x00, 0x00, 0x00, 0x00, 0x00, 0x04, 0x93, 0xE0,
			},
			expected: &validatorMetadata{
				UpDuration:               6000000,
				LastUpdated:              900000,
				PotentialReward:          100000,
				PotentialDelegateeReward: 20000,
				StakerStartTime:          300000,
			},
			expectedErr: nil,
		},
		{
			name: "codec v2 fields",
			bytes: []byte{
				// codec version
				0x00, 0x02,
				// up duration
				0x00, 0x00, 0x00, 0x00, 0x00, 0x5B, 0x8D, 0x80,
				// last updated
				0x00, 0x00, 0x00, 0x00, 0x00, 0x0D, 0xBB, 0xA0,
				// potential reward
				0x00, 0x00, 0x00, 0x00, 0x00, 0x01, 0x86, 0xA0,
				// potential delegatee reward
				0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x4E, 0x20,
				// staker start time
				0x00, 0x00, 0x00, 0x00, 0x00, 0x04, 0x93, 0xE0,
				// accrued rewards
				0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x03, 0xE8,
				// accrued delegatee rewards
				0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x01, 0xF4,
				// auto restake shares
				0x00, 0x04, 0x93, 0xE0,
				// continuation period
				0x00, 0x00, 0x00, 0x00, 0x00, 0x01, 0x51, 0x80,
			},
			expected: &validatorMetadata{
				UpDuration:               6000000,
				LastUpdated:              900000,
				PotentialReward:          100000,
				PotentialDelegateeReward: 20000,
				StakerStartTime:          300000,
				AccruedRewards:           1000,
				AccruedDelegateeRewards:  500,
				AutoRestakeShares:        300000,
				ContinuationPeriod:       86400,
			},
			expectedErr: nil,
		},
		{
			name: "invalid codec version",
			bytes: []byte{
				// codec version
				0x00, 0x03,
				// up duration
				0x00, 0x00, 0x00, 0x00, 0x00, 0x5B, 0x8D, 0x80,
				// last updated
				0x00, 0x00, 0x00, 0x00, 0x00, 0x0D, 0xBB, 0xA0,
				// potential reward
				0x00, 0x00, 0x00, 0x00, 0x00, 0x01, 0x86, 0xA0,
				// potential delegatee reward
				0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x4E, 0x20,
			},
			expected:    nil,
			expectedErr: codec.ErrUnknownVersion,
		},
		{
			name: "short byte len",
			bytes: []byte{
				// codec version
				0x00, 0x00,
				// up duration
				0x00, 0x00, 0x00, 0x00, 0x00, 0x5B, 0x8D, 0x80,
				// last updated
				0x00, 0x00, 0x00, 0x00, 0x00, 0x0D, 0xBB, 0xA0,
				// potential reward
				0x00, 0x00, 0x00, 0x00, 0x00, 0x01, 0x86, 0xA0,
				// potential delegatee reward
				0x00, 0x00, 0x00, 0x00, 0x4E, 0x20,
			},
			expected:    nil,
			expectedErr: wrappers.ErrInsufficientLength,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)
			var metadata validatorMetadata
			err := parseValidatorMetadata(tt.bytes, &metadata)
			require.ErrorIs(err, tt.expectedErr)
			if tt.expectedErr != nil {
				return
			}
			require.Equal(tt.expected, &metadata)
		})
	}
}
