// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package bloom

import (
	"math/rand"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/utils/units"
)

func TestNewErrors(t *testing.T) {
	tests := []struct {
		numHashes  int
		numEntries int
		err        error
	}{
		{
			numHashes:  0,
			numEntries: 1,
			err:        errTooFewHashes,
		},
		{
			numHashes:  17,
			numEntries: 1,
			err:        errTooManyHashes,
		},
		{
			numHashes:  8,
			numEntries: 0,
			err:        errTooFewEntries,
		},
	}
	for _, test := range tests {
		t.Run(test.err.Error(), func(t *testing.T) {
			_, err := New(test.numHashes, test.numEntries)
			require.ErrorIs(t, err, test.err)
		})
	}
}

func TestAdd(t *testing.T) {
	require := require.New(t)

	initialNumHashes, initialNumBytes := OptimalParameters(1024, 0.01)
	filter, err := New(initialNumHashes, initialNumBytes)
	require.NoError(err)

	require.True(filter.Add(1))
	require.Equal(1, filter.Count())
	require.False(filter.Add(1))
	require.Equal(1, filter.Count())
}

func TestNormalUsage(t *testing.T) {
	require := require.New(t)

	toAdd := make([]uint64, 1024)
	for i := range toAdd {
		toAdd[i] = rand.Uint64() //#nosec G404
	}

	initialNumHashes, initialNumBytes := OptimalParameters(1024, 0.01)
	filter, err := New(initialNumHashes, initialNumBytes)
	require.NoError(err)

	for i, elem := range toAdd {
		filter.Add(elem)
		for _, elem := range toAdd[:i] {
			require.True(filter.Contains(elem))
		}
	}

	require.LessOrEqual(filter.Count(), len(toAdd))

	filterBytes := filter.Marshal()
	parsedFilter, err := Parse(filterBytes)
	require.NoError(err)

	for _, elem := range toAdd {
		require.True(parsedFilter.Contains(elem))
	}

	parsedFilterBytes := parsedFilter.Marshal()
	require.Equal(filterBytes, parsedFilterBytes)
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
