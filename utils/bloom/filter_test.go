// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package bloom

import (
	"encoding/binary"
	"fmt"
	"math/rand"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/utils/units"
)

func TestNewErrors(t *testing.T) {
	tests := []struct {
		numSeeds int
		numBytes int
		err      error
	}{
		{
			numSeeds: 0,
			numBytes: 1,
			err:      errTooFewSeeds,
		},
		{
			numSeeds: 17,
			numBytes: 1,
			err:      errTooManySeeds,
		},
		{
			numSeeds: 8,
			numBytes: 0,
			err:      errTooFewEntries,
		},
	}
	for _, test := range tests {
		t.Run(test.err.Error(), func(t *testing.T) {
			_, err := New(test.numSeeds, test.numBytes)
			require.ErrorIs(t, err, test.err)
		})
	}
}

func TestNormalUsage(t *testing.T) {
	require := require.New(t)

	toAdd := make([]uint64, 1024)
	for i := range toAdd {
		toAdd[i] = rand.Uint64() //#nosec G404
	}

	initialNumSeeds, initialNumBytes := OptimalParameters(1024, 0.01)
	filter, err := New(initialNumSeeds, initialNumBytes)
	require.NoError(err)

	for i, elem := range toAdd {
		filter.Add(elem)
		for _, elem := range toAdd[:i] {
			require.True(filter.Contains(elem))
		}
	}

	require.InDelta(filter.FalsePositiveProbability(), 0.01, 1e-4)

	numSeeds, numBytes := filter.Parameters()
	require.Equal(initialNumSeeds, numSeeds)
	require.Equal(initialNumBytes, numBytes)

	filterBytes := filter.Marshal()
	parsedFilter, err := Parse(filterBytes)
	require.NoError(err)

	for _, elem := range toAdd {
		require.True(parsedFilter.Contains(elem))
	}

	parsedFilterBytes := parsedFilter.Marshal()
	require.Equal(filterBytes, parsedFilterBytes)
}

func TestFalsePositiveProbability(t *testing.T) {
	tests := []struct {
		numSeeds                             float64
		numBits                              float64
		numAdded                             float64
		expectedFalsePositiveProbability     float64
		allowedFalsePositiveProbabilityDelta float64
	}{
		{
			numSeeds:                             8,
			numBits:                              10_000,
			numAdded:                             0,
			expectedFalsePositiveProbability:     0,
			allowedFalsePositiveProbabilityDelta: 0,
		},
		{ // params from OptimalParameters(10_000, .01)
			numSeeds:                             7,
			numBits:                              11_982 * 8,
			numAdded:                             10_000,
			expectedFalsePositiveProbability:     .01,
			allowedFalsePositiveProbabilityDelta: 1e-4,
		},
		{ // params from OptimalParameters(100_000, .001)
			numSeeds:                             10,
			numBits:                              179_720 * 8,
			numAdded:                             100_000,
			expectedFalsePositiveProbability:     .001,
			allowedFalsePositiveProbabilityDelta: 1e-7,
		},
		{ // params from OptimalParameters(10_000, .01)
			numSeeds:                             7,
			numBits:                              11_982 * 8,
			numAdded:                             15_000,
			expectedFalsePositiveProbability:     .05,
			allowedFalsePositiveProbabilityDelta: .01,
		},
	}
	for _, test := range tests {
		t.Run(fmt.Sprintf("%f_%f_%f", test.numSeeds, test.numBits, test.numAdded), func(t *testing.T) {
			p := falsePositiveProbability(test.numSeeds, test.numBits, test.numAdded)
			require.InDelta(t, test.expectedFalsePositiveProbability, p, test.allowedFalsePositiveProbabilityDelta)
		})
	}
}

func BenchmarkAdd(b *testing.B) {
	f, err := New(8, 16*units.KiB)
	require.NoError(b, err)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		f.Add(1)
	}
}

func BenchmarkMarshal(b *testing.B) {
	f, err := New(OptimalParameters(10_000, .01))
	require.NoError(b, err)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		f.Marshal()
	}
}

func FuzzUintSize(f *testing.F) {
	f.Add(uint64(0))
	for i := 0; i < 64; i++ {
		f.Add(uint64(1) << i)
	}
	f.Fuzz(func(t *testing.T, value uint64) {
		length := uintSize(value)
		expectedLength := len(binary.AppendUvarint(nil, value))
		require.Equal(t, expectedLength, length)
	})
}
