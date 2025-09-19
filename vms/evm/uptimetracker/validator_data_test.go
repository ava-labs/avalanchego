// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package uptimetracker

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/wrappers"
)

func TestParseValidator(t *testing.T) {
	testNodeID, err := ids.NodeIDFromString("NodeID-CaBYJ9kzHvrQFiYWowMkJGAQKGMJqZoat")
	require.NoError(t, err)
	type test struct {
		name        string
		bytes       []byte
		expected    *validatorData
		expectedErr error
	}
	tests := []test{
		{
			name:  "nil",
			bytes: nil,
			expected: &validatorData{
				LastUpdated:   0,
				StartTime:     0,
				validationID:  ids.Empty,
				NodeID:        ids.EmptyNodeID,
				UpDuration:    0,
				Weight:        0,
				IsActive:      false,
				IsL1Validator: false,
			},
			expectedErr: nil,
		},
		{
			name:  "empty",
			bytes: []byte{},
			expected: &validatorData{
				LastUpdated:   0,
				StartTime:     0,
				validationID:  ids.Empty,
				NodeID:        ids.EmptyNodeID,
				UpDuration:    0,
				Weight:        0,
				IsActive:      false,
				IsL1Validator: false,
			},
			expectedErr: nil,
		},
		{
			name: "valid",
			bytes: []byte{
				// codec version
				0x00, 0x00,
				// up duration
				0x00, 0x00, 0x00, 0x00, 0x00, 0x5B, 0x8D, 0x80,
				// last updated
				0x00, 0x00, 0x00, 0x00, 0x00, 0x0D, 0xBB, 0xA0,
				// node ID
				0x7e, 0xef, 0xe8, 0x8a, 0x45, 0xfb, 0x7a, 0xc4,
				0xb0, 0x59, 0xc9, 0x33, 0x71, 0x0a, 0x57, 0x33,
				0xff, 0x9f, 0x4b, 0xab,
				// weight
				0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x01,
				// start time
				0x00, 0x00, 0x00, 0x00, 0x00, 0x5B, 0x8D, 0x80,
				// status
				0x01,
				// IsL1Validator
				0x01,
			},
			expected: &validatorData{
				UpDuration:    time.Duration(6000000),
				LastUpdated:   900000,
				NodeID:        testNodeID,
				StartTime:     6000000,
				IsActive:      true,
				Weight:        1,
				IsL1Validator: true,
			},
		},
		{
			name: "invalid codec version",
			bytes: []byte{
				// codec version
				0x00, 0x02,
				// up duration
				0x00, 0x00, 0x00, 0x00, 0x00, 0x5B, 0x8D, 0x80,
				// last updated
				0x00, 0x00, 0x00, 0x00, 0x00, 0x0D, 0xBB, 0xA0,
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
			},
			expected:    nil,
			expectedErr: wrappers.ErrInsufficientLength,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)
			var data validatorData
			err := parseValidatorData(tt.bytes, &data)
			require.ErrorIs(err, tt.expectedErr)
			if tt.expectedErr != nil {
				return
			}
			require.Equal(tt.expected, &data)
		})
	}
}
