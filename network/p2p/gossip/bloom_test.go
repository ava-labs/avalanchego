// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package gossip

import (
	"crypto/rand"
	"fmt"
	"math"
	"slices"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
)

func BenchmarkBloomFilterMarshaling(b *testing.B) {
	tests := []struct {
		name                           string
		minTargetElements              int
		targetFalsePositiveProbability float64
		resetFalsePositiveProbability  float64
	}{
		{
			name:                           "small-1K",
			minTargetElements:              1000,
			targetFalsePositiveProbability: 0.01,
			resetFalsePositiveProbability:  0.05,
		},
		{
			name:                           "medium-10K",
			minTargetElements:              10000,
			targetFalsePositiveProbability: 0.01,
			resetFalsePositiveProbability:  0.05,
		},
		{
			name:                           "large-100K",
			minTargetElements:              100000,
			targetFalsePositiveProbability: 0.01,
			resetFalsePositiveProbability:  0.05,
		},
		{
			name:                           "huge-1M",
			minTargetElements:              1000000,
			targetFalsePositiveProbability: 0.01,
			resetFalsePositiveProbability:  0.05,
		},
		{
			name:                           "common-8K",
			minTargetElements:              1024 * 8,
			targetFalsePositiveProbability: 0.01,
			resetFalsePositiveProbability:  0.05,
		},
	}

	for _, tt := range tests {
		name := fmt.Sprintf("%s-Marshaling", tt.name)
		b.Run(name, func(b *testing.B) {
			require := require.New(b)
			bloom, err := NewBloomFilter(prometheus.NewRegistry(), "", tt.minTargetElements, tt.targetFalsePositiveProbability, tt.resetFalsePositiveProbability)
			require.NoError(err)

			b.ResetTimer()
			for range b.N {
				bloom.Marshal()
			}
			b.StopTimer()
		})
	}
	for _, tt := range tests {
		name := fmt.Sprintf("%s-New", tt.name)
		b.Run(name, func(b *testing.B) {
			require := require.New(b)
			for range b.N {
				_, err := NewBloomFilter(prometheus.NewRegistry(), "", tt.minTargetElements, tt.targetFalsePositiveProbability, tt.resetFalsePositiveProbability)
				require.NoError(err)
			}
			b.StopTimer()
		})
	}
}

func TestBloomFilterSize(t *testing.T) {
	tests := []struct {
		name                           string
		minTargetElements              int
		multiplierAfterReset           int
		targetFalsePositiveProbability float64
		resetFalsePositiveProbability  float64
		expectedSize                   int
		expectedPostResetSize          int
		expectedPostResetElementsCount int
	}{
		{
			name:                           "small",
			minTargetElements:              1000,
			targetFalsePositiveProbability: 0.01,
			resetFalsePositiveProbability:  0.05,
			multiplierAfterReset:           3,
			expectedSize:                   1_256,
			expectedPostResetSize:          5_255,
			expectedPostResetElementsCount: 4_338,
		},
		{
			name:                           "medium",
			minTargetElements:              10000,
			targetFalsePositiveProbability: 0.01,
			resetFalsePositiveProbability:  0.05,
			multiplierAfterReset:           3,
			expectedSize:                   12_039,
			expectedPostResetSize:          51_993,
			expectedPostResetElementsCount: 43_347,
		},
		{
			name:                           "large",
			minTargetElements:              100000,
			targetFalsePositiveProbability: 0.01,
			resetFalsePositiveProbability:  0.05,
			multiplierAfterReset:           3,
			expectedSize:                   119_871,
			expectedPostResetSize:          519_351,
			expectedPostResetElementsCount: 433_419,
		},
		{
			name:                           "huge",
			minTargetElements:              1000000,
			targetFalsePositiveProbability: 0.01,
			resetFalsePositiveProbability:  0.05,
			multiplierAfterReset:           3,
			expectedSize:                   1_198_190,
			expectedPostResetSize:          5_192_954,
			expectedPostResetElementsCount: 4_334_160,
		},
		{
			name:                           "common",
			minTargetElements:              1024 * 8,
			targetFalsePositiveProbability: 0.01,
			resetFalsePositiveProbability:  0.05,
			multiplierAfterReset:           3,
			expectedSize:                   9_872,
			expectedPostResetSize:          42_601,
			expectedPostResetElementsCount: 35_508,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)
			bloom, err := NewBloomFilter(prometheus.NewRegistry(), "", tt.minTargetElements, tt.targetFalsePositiveProbability, tt.resetFalsePositiveProbability)
			require.NoError(err)

			bloomBytes, salt := bloom.Marshal()
			require.Equal(tt.expectedSize, len(bloomBytes))
			require.Equal(32, len(salt))
			elementsCount := 0

			for {
				// create a new item.
				tx := testTx{}
				_, err = rand.Read(tx.id[:])
				require.NoError(err)
				bloom.Add(&tx)
				reset, err := ResetBloomFilterIfNeeded(bloom, elementsCount*tt.multiplierAfterReset)
				require.NoError(err)
				if reset {
					break
				}
				elementsCount++
			}
			require.Equal(tt.expectedPostResetElementsCount, elementsCount*tt.multiplierAfterReset)

			bloomBytes, salt = bloom.Marshal()
			require.Equal(tt.expectedPostResetSize, len(bloomBytes))
			require.Equal(32, len(salt))
		})
	}
}

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

func TestBloomFilterFalsePositiveRate(t *testing.T) {
	tests := []struct {
		name                           string
		minTargetElements              int
		fillElementCount               int
		targetFalsePositiveProbability float64
		resetFalsePositiveProbability  float64
	}{
		{
			name:                           "filled",
			minTargetElements:              8 * 1024,
			fillElementCount:               8 * 1024,
			targetFalsePositiveProbability: 0.01,
			resetFalsePositiveProbability:  0.05,
		},
		{
			name:                           "overfilled",
			minTargetElements:              8 * 1024,
			fillElementCount:               12 * 1024,
			targetFalsePositiveProbability: 0.01,
			resetFalsePositiveProbability:  0.05,
		},
		{
			name:                           "low percision",
			minTargetElements:              8 * 1024,
			fillElementCount:               8 * 1024,
			targetFalsePositiveProbability: 0.1,
			resetFalsePositiveProbability:  0.5,
		},
		{
			name:                           "high percision",
			minTargetElements:              8 * 1024,
			fillElementCount:               8 * 1024,
			targetFalsePositiveProbability: 0.001,
			resetFalsePositiveProbability:  0.005,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require := require.New(t)
			filter, err := NewBloomFilter(prometheus.NewRegistry(), "", tt.minTargetElements, tt.targetFalsePositiveProbability, tt.resetFalsePositiveProbability)
			require.NoError(err)

			for range tt.fillElementCount {
				// create a new item.
				tx := testTx{}
				_, err = rand.Read(tx.id[:])
				require.NoError(err)
				filter.Add(&tx)
			}

			// select a sample size in inverse to the desired probability.
			sampleSize := int(1000.0 / tt.targetFalsePositiveProbability)
			falsePositivesCount := 0
			for range sampleSize {
				// create a new item.
				tx := testTx{}
				_, err = rand.Read(tx.id[:])
				require.NoError(err)
				if filter.Has(&tx) {
					falsePositivesCount++
				}
			}
			actualFalsePositivesRate := float64(falsePositivesCount) / float64(sampleSize)
			expectedProbability := tt.targetFalsePositiveProbability
			if tt.fillElementCount > tt.minTargetElements {
				expectedProbability = tt.resetFalsePositiveProbability
			}
			require.Lessf(math.Abs(expectedProbability-actualFalsePositivesRate), expectedProbability*0.7, "false positive rate mismatch, expected %v actual %v", expectedProbability, actualFalsePositivesRate)
		})
	}
}
