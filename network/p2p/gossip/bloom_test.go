// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package gossip

import (
	"slices"
	"sync"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
)

func TestBloomFilterRefresh(t *testing.T) {
	tests := []struct {
		name                           string
		minTargetElements              int
		targetFalsePositiveProbability float64
		resetFalsePositiveProbability  float64
		resetCount                     uint64
		add                            []*testTx
		expected                       []*testTx
	}{
		{
			name:                           "no refresh",
			minTargetElements:              1,
			targetFalsePositiveProbability: 0.01,
			resetFalsePositiveProbability:  1,
			resetCount:                     0, // maxCount = 9223372036854775807
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
			resetCount:                     1,
			add: []*testTx{
				{id: ids.ID{0}},
				{id: ids.ID{1}},
				{id: ids.ID{2}},
			},
			expected: []*testTx{
				{id: ids.ID{2}},
			},
		},
		{
			name:                           "multiple refresh",
			minTargetElements:              1,
			targetFalsePositiveProbability: 0.01,
			resetFalsePositiveProbability:  0.0000000000000001, // maxCount = 1
			resetCount:                     2,
			add: []*testTx{
				{id: ids.ID{0}},
				{id: ids.ID{1}},
				{id: ids.ID{2}},
				{id: ids.ID{3}},
				{id: ids.ID{4}},
			},
			expected: []*testTx{
				{id: ids.ID{4}},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)
			bloom, err := NewBloomFilter(prometheus.NewRegistry(), "", tt.minTargetElements, tt.targetFalsePositiveProbability, tt.resetFalsePositiveProbability)
			require.NoError(err)

			var resetCount uint64
			for _, item := range tt.add {
				bloomBytes, saltBytes := bloom.Marshal()
				initialBloomBytes := slices.Clone(bloomBytes)
				initialSaltBytes := slices.Clone(saltBytes)

				reset, err := ResetBloomFilterIfNeeded(bloom, len(tt.add))
				require.NoError(err)
				if reset {
					resetCount++
				}
				bloom.Add(item)

				require.Equal(initialBloomBytes, bloomBytes)
				require.Equal(initialSaltBytes, saltBytes)
			}

			require.Equal(tt.resetCount, resetCount)
			require.Equal(float64(tt.resetCount+1), testutil.ToFloat64(bloom.metrics.ResetCount))
			for _, expected := range tt.expected {
				require.True(bloom.Has(expected))
			}
		})
	}
}

func TestBloomFilterClobber(t *testing.T) {
	b, err := NewBloomFilter(prometheus.NewRegistry(), "", 1, 0.5, 0.5)
	require.NoError(t, err, "NewBloomFilter()")

	start := make(chan struct{})
	var wg sync.WaitGroup

	for _, fn := range []func(){
		func() { b.Add(&testTx{}) },
		func() { b.Has(&testTx{}) },
		func() { b.Marshal() },
		func() {
			_, err := b.ResetIfNeeded(1)
			require.NoErrorf(t, err, "%T.ResetIfNeeded()", b)
		},
	} {
		for range 10_000 {
			wg.Add(1)
			go func() {
				<-start
				fn()
				wg.Done()
			}()
		}
	}

	close(start)
	wg.Wait()
}
