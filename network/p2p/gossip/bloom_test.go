// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package gossip

import (
	"testing"

	"github.com/stretchr/testify/require"

	"golang.org/x/exp/slices"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/bloom"
)

func TestBloomFilterRefresh(t *testing.T) {
	tests := []struct {
		name                           string
		minTargetElements              int
		targetFalsePositiveProbability float64
		resetFalsePositiveProbability  float64
		reset                          bool
		add                            []*testTx
		expected                       []*testTx
	}{
		{
			name:                           "no refresh",
			minTargetElements:              1,
			targetFalsePositiveProbability: 0.01,
			resetFalsePositiveProbability:  1,
			reset:                          false, // maxCount = 9223372036854775807
			add: []*testTx{
				{id: ids.ID{0}},
				{id: ids.ID{1}},
				{id: ids.ID{2}},
			},
			expected: []*testTx{
				{id: ids.ID{0}},
				{id: ids.ID{1}},
				{id: ids.ID{2}},
			},
		},
		{
			name:                           "refresh",
			minTargetElements:              1,
			targetFalsePositiveProbability: 0.01,
			resetFalsePositiveProbability:  0.0000000000000001, // maxCount = 1
			reset:                          true,
			add: []*testTx{
				{id: ids.ID{0}},
				{id: ids.ID{1}},
				{id: ids.ID{2}},
			},
			expected: []*testTx{
				{id: ids.ID{2}},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)
			numHashes, numEntries := bloom.OptimalParameters(
				tt.minTargetElements,
				tt.targetFalsePositiveProbability,
			)
			b, err := bloom.New(numHashes, numEntries)
			require.NoError(err)
			bloom := BloomFilter{
				bloom:                          b,
				maxCount:                       bloom.EstimateCount(numHashes, numEntries, tt.resetFalsePositiveProbability),
				minTargetElements:              tt.minTargetElements,
				targetFalsePositiveProbability: tt.targetFalsePositiveProbability,
				resetFalsePositiveProbability:  tt.resetFalsePositiveProbability,
			}

			var didReset bool
			for _, item := range tt.add {
				bloomBytes, saltBytes := bloom.Marshal()
				initialBloomBytes := slices.Clone(bloomBytes)
				initialSaltBytes := slices.Clone(saltBytes)

				reset, err := ResetBloomFilterIfNeeded(&bloom, len(tt.add))
				require.NoError(err)
				if reset {
					didReset = reset
				}
				bloom.Add(item)

				require.Equal(initialBloomBytes, bloomBytes)
				require.Equal(initialSaltBytes, saltBytes)
			}

			require.Equal(tt.reset, didReset)
			for _, expected := range tt.expected {
				require.True(bloom.Has(expected))
			}
		})
	}
}
