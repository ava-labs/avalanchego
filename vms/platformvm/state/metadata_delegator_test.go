// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseDelegatorMetadata(t *testing.T) {
	type test struct {
		name     string
		version  uint16
		bytes    []byte
		expected *delegatorMetadata
	}
	tests := []test{
		{
			name:    "delegator metadata v1",
			version: v1,
			bytes: []byte{
				// codec version
				0x0, 0x1,
				// potential reward
				0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x7b,
				// staker start time
				0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x1, 0xc8,
			},
			expected: &delegatorMetadata{
				PotentialReward: 123,
				StakerStartTime: 456,
			},
		},
		{
			name:    "delegator metadata v0",
			version: v0,
			bytes: []byte{
				// codec version
				0x0, 0x0,
				// potential reward
				0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x0, 0x7b,
			},
			expected: &delegatorMetadata{
				PotentialReward: 123,
				StakerStartTime: 0,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			// marshal with the right version
			metadataBytes, err := metadataCodec.Marshal(tt.version, tt.expected)
			require.NoError(err)
			require.Equal(metadataBytes, tt.bytes)

			// unmarshal and compare results
			var metadata delegatorMetadata
			require.NoError(parseDelegatorMetadata(tt.bytes, &metadata))
			require.Equal(tt.expected, &metadata)
		})
	}
}
