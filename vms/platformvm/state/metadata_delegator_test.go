// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/database/memdb"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/wrappers"
)

func TestParseDelegatorMetadata(t *testing.T) {
	type test struct {
		name        string
		bytes       []byte
		expected    *delegatorMetadata
		expectedErr error
	}
	tests := []test{
		{
			name: "potential reward only no codec",
			bytes: []byte{
				0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x7b,
			},
			expected: &delegatorMetadata{
				PotentialReward: 123,
				StakerStartTime: 0,
			},
			expectedErr: nil,
		},
		{
			name: "potential reward + staker start time with codec v1",
			bytes: []byte{
				// codec version
				0x00, 0x01,
				// potential reward
				0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x7b,
				// staker start time
				0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x01, 0xc8,
			},
			expected: &delegatorMetadata{
				PotentialReward: 123,
				StakerStartTime: 456,
			},
			expectedErr: nil,
		},
		{
			name: "invalid codec version",
			bytes: []byte{
				// codec version
				0x00, 0x02,
				// potential reward
				0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x7b,
				// staker start time
				0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x01, 0xc8,
			},
			expected:    nil,
			expectedErr: codec.ErrUnknownVersion,
		},
		{
			name: "short byte len",
			bytes: []byte{
				// codec version
				0x00, 0x01,
				// potential reward
				0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x7b,
				// staker start time
				0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
			},
			expected:    nil,
			expectedErr: wrappers.ErrInsufficientLength,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)
			var metadata delegatorMetadata
			err := parseDelegatorMetadata(tt.bytes, &metadata)
			require.ErrorIs(err, tt.expectedErr)
			if tt.expectedErr != nil {
				return
			}
			require.Equal(tt.expected, &metadata)
		})
	}
}

func TestWriteDelegatorMetadata(t *testing.T) {
	type test struct {
		name     string
		version  uint16
		metadata *delegatorMetadata
		expected []byte
	}
	tests := []test{
		{
			name:    CodecVersion0Tag,
			version: CodecVersion0,
			metadata: &delegatorMetadata{
				PotentialReward: 123,
				StakerStartTime: 456,
			},
			expected: []byte{
				// potential reward
				0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x7b,
			},
		},
		{
			name:    CodecVersion1Tag,
			version: CodecVersion1,
			metadata: &delegatorMetadata{
				PotentialReward: 123,
				StakerStartTime: 456,
			},
			expected: []byte{
				// codec version
				0x00, 0x01,
				// potential reward
				0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x7b,
				// staker start time
				0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x01, 0xc8,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)
			db := memdb.New()
			tt.metadata.txID = ids.GenerateTestID()
			require.NoError(writeDelegatorMetadata(db, tt.metadata, tt.version))
			bytes, err := db.Get(tt.metadata.txID[:])
			require.NoError(err)
			require.Equal(tt.expected, bytes)
		})
	}
}
