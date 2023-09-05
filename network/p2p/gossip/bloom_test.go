// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package gossip

import (
	"testing"

	bloomfilter "github.com/holiman/bloomfilter/v2"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
)

func TestBloomFilterRefresh(t *testing.T) {
	tests := []struct {
		name                     string
		falsePositiveProbability float64
		add                      []*testTx
		expected                 []*testTx
	}{
		{
			name:                     "no refresh",
			falsePositiveProbability: 1,
			add: []*testTx{
				{id: ids.ID{0}},
			},
			expected: []*testTx{
				{id: ids.ID{0}},
			},
		},
		{
			name:                     "refresh",
			falsePositiveProbability: 0.1,
			add: []*testTx{
				{id: ids.ID{0}},
				{id: ids.ID{1}},
			},
			expected: []*testTx{
				{id: ids.ID{1}},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)
			b, err := bloomfilter.New(10, 1)
			require.NoError(err)
			bloom := BloomFilter{
				Bloom: b,
			}

			for _, item := range tt.add {
				_, err = ResetBloomFilterIfNeeded(&bloom, tt.falsePositiveProbability)
				require.NoError(err)
				bloom.Add(item)
			}

			require.Equal(uint64(len(tt.expected)), bloom.Bloom.N())

			for _, expected := range tt.expected {
				require.True(bloom.Has(expected))
			}
		})
	}
}
