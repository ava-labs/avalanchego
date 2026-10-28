// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package warpmsg

import (
	"slices"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/codec"
	"github.com/ava-labs/avalanchego/utils/set"
)

// Valid result parsing is tested by [TestBlockResultsBytes]
func TestParseBlockResultsInvalid(t *testing.T) {
	tests := []struct {
		name    string
		b       []byte
		wantErr error
	}{
		{
			name:    "nil",
			b:       nil,
			wantErr: codec.ErrCantUnpackVersion,
		},
		{
			name: "too_big",
			b: slices.Concat(
				[]byte{
					// codecID
					0x00, 0x00,
					// BlockResults length
					0x00, 0x00, 0x00, 0x01,
					// txHash
					0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
					0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
					0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
					0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
					// tx results length
					0x00, 0x00, 0x00, 0x01,
					// precompile address
					0x02, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
					0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
					0x00, 0x00, 0x00, 0x00,
					// Length of bitset
					0x01, 0x00, 0x00, 0x00, // 2^24
				},
				make([]byte, 1<<24), // Append the bitset
			),
			wantErr: codec.ErrUnmarshalTooBig,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := ParseBlockResults(test.b)
			require.ErrorIs(t, err, test.wantErr)
		})
	}
}

func TestParseBlockResultsIgnoresOtherAddresses(t *testing.T) {
	results := encodedBlockResults{
		{1}: {common.Address{2}: set.NewBits(1).Bytes()},
		{2}: {
			common.Address{2}: set.NewBits(1).Bytes(),
			ContractAddress:   set.NewBits(3).Bytes(),
		},
	}
	b, err := resultsCodec.Marshal(version, results)
	require.NoError(t, err, "resultsCodec.Marshal()")

	got, err := ParseBlockResults(b)
	require.NoError(t, err, "ParseBlockResults()")
	want := BlockResults{
		{2}: set.NewBits(3),
	}
	require.Equal(t, want, got, "ParseBlockResults()")
}

func TestBlockResultsGet(t *testing.T) {
	txHash := common.Hash{1}
	tests := []struct {
		name    string
		results BlockResults
		want    set.Bits
	}{
		{
			name: "nil",
			want: set.NewBits(),
		},
		{
			name:    "empty",
			results: make(BlockResults),
			want:    set.NewBits(),
		},
		{
			name: "missing_tx",
			results: BlockResults{
				{3}: set.NewBits(1, 2, 3),
			},
			want: set.NewBits(),
		},
		{
			name: "found",
			results: BlockResults{
				txHash: set.NewBits(1, 2, 3),
			},
			want: set.NewBits(1, 2, 3),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.results.Get(txHash)
			require.Equal(t, tt.want, got, "BlockResults.Get()")
		})
	}
}

func TestBlockResultsSet(t *testing.T) {
	txHash := common.Hash{1}
	tests := []struct {
		name  string
		start BlockResults
		toSet set.Bits
		want  BlockResults
	}{
		{
			name:  "nil",
			toSet: set.NewBits(1),
			want: BlockResults{
				txHash: set.NewBits(1),
			},
		},
		{
			name:  "empty_bits",
			start: make(BlockResults),
			toSet: set.NewBits(),
			want: BlockResults{
				txHash: set.NewBits(),
			},
		},
		{
			name: "overwrite",
			start: BlockResults{
				txHash: set.NewBits(1),
				{2}:    set.NewBits(2),
			},
			toSet: set.NewBits(3),
			want: BlockResults{
				txHash: set.NewBits(3),
				{2}:    set.NewBits(2),
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.start.Set(txHash, tt.toSet)
			require.Equal(t, tt.want, tt.start, "BlockResults.Set()")
		})
	}
}

func TestBlockResultsBytes(t *testing.T) {
	tests := []struct {
		name  string
		input BlockResults
		want  []byte
	}{
		{
			name: "nil",
			want: []byte{
				// codecID
				0x00, 0x00,
				// results length
				0x00, 0x00, 0x00, 0x00,
			},
		},
		{
			name:  "empty",
			input: make(BlockResults),
			want: []byte{
				// codecID
				0x00, 0x00,
				// results length
				0x00, 0x00, 0x00, 0x00,
			},
		},
		{
			name: "single_tx",
			input: BlockResults{
				{1}: set.NewBits(1, 2, 3),
			},
			want: []byte{
				// codecID
				0x00, 0x00,
				// BlockResults length
				0x00, 0x00, 0x00, 0x01,
				// txHash
				0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
				0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
				0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
				0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
				// tx results length
				0x00, 0x00, 0x00, 0x01,
				// Warp precompile address
				0x02, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
				0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
				0x00, 0x00, 0x00, 0x05,
				// Length of bitset
				0x00, 0x00, 0x00, 0x01,
				// bitset
				0b00001110,
			},
		},
		{
			name: "all_valid",
			input: BlockResults{
				{1}: set.NewBits(),
			},
			want: []byte{
				// codecID
				0x00, 0x00,
				// BlockResults length
				0x00, 0x00, 0x00, 0x01,
				// txHash
				0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
				0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
				0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
				0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
				// tx results length
				0x00, 0x00, 0x00, 0x01,
				// Warp precompile address
				0x02, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
				0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
				0x00, 0x00, 0x00, 0x05,
				// Length of bitset
				0x00, 0x00, 0x00, 0x00,
			},
		},
		{
			name: "multiple_txs",
			input: BlockResults{
				{1}: set.NewBits(1, 2, 3),
				{2}: set.NewBits(0),
			},
			want: []byte{
				// codecID
				0x00, 0x00,
				// BlockResults length
				0x00, 0x00, 0x00, 0x02,
				// txHash
				0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
				0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
				0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
				0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
				// tx results length
				0x00, 0x00, 0x00, 0x01,
				// Warp precompile address
				0x02, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
				0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
				0x00, 0x00, 0x00, 0x05,
				// Length of bitset
				0x00, 0x00, 0x00, 0x01,
				// bitset
				0b00001110,
				// txHash
				0x02, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
				0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
				0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
				0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
				// tx results length
				0x00, 0x00, 0x00, 0x01,
				// Warp precompile address
				0x02, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
				0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
				0x00, 0x00, 0x00, 0x05,
				// Length of bitset
				0x00, 0x00, 0x00, 0x01,
				// bitset
				0b00000001,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)

			got, err := tt.input.Bytes()
			require.NoError(err, "BlockResults.Bytes()")
			require.Equal(tt.want, got, "BlockResults.Bytes()")

			input := tt.input
			if input == nil {
				// nil and empty should be considered equivalent
				input = make(BlockResults)
			}

			parsed, err := ParseBlockResults(got)
			require.NoError(err, "ParseBlockResults()")
			require.Equal(input, parsed, "ParseBlockResults()")
		})
	}
}
