// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package bloom

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

func NewMaliciousFilter(numSeeds, numBytes int) *Filter {
	f := &Filter{
		numBits: uint64(numBytes * bitsPerByte),
		seeds:   make([]uint64, numSeeds),
		entries: make([]byte, numBytes),
		count:   0,
	}
	for i := range f.entries {
		f.entries[i] = math.MaxUint8
	}
	return f
}

func TestParseErrors(t *testing.T) {
	tests := []struct {
		bytes []byte
		err   error
	}{
		{
			bytes: nil,
			err:   errInvalidNumSeeds,
		},
		{
			bytes: NewMaliciousFilter(0, 1).Marshal(),
			err:   errTooFewSeeds,
		},
		{
			bytes: NewMaliciousFilter(17, 1).Marshal(),
			err:   errTooManySeeds,
		},
		{
			bytes: NewMaliciousFilter(1, 0).Marshal(),
			err:   errTooFewEntries,
		},
		{
			bytes: []byte{
				0x01, // num seeds = 1
			},
			err: errTooFewEntries,
		},
	}
	for _, test := range tests {
		t.Run(test.err.Error(), func(t *testing.T) {
			_, err := Parse(test.bytes)
			require.ErrorIs(t, err, test.err)
		})
	}
}

func BenchmarkParse(b *testing.B) {
	f, err := New(OptimalParameters(10_000, .01))
	require.NoError(b, err)
	bytes := f.Marshal()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = Parse(bytes)
	}
}

func BenchmarkContains(b *testing.B) {
	f := NewMaliciousFilter(maxSeeds, 1)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		f.Contains(1)
	}
}

func FuzzParseThenMarshal(f *testing.F) {
	f.Fuzz(func(t *testing.T, bytes []byte) {
		f, err := Parse(bytes)
		if err != nil {
			return
		}

		marshalledBytes := marshal(f.seeds, f.entries)
		require.Equal(t, bytes, marshalledBytes)
	})
}

func FuzzMarshalThenParse(f *testing.F) {
	f.Fuzz(func(t *testing.T, numSeeds int, entries []byte) {
		require := require.New(t)

		seeds, err := newSeeds(numSeeds)
		if err != nil {
			return
		}
		if len(entries) < minEntries {
			return
		}

		marshalledBytes := marshal(seeds, entries)
		rf, err := Parse(marshalledBytes)
		require.NoError(err)
		require.Equal(seeds, rf.seeds)
		require.Equal(entries, rf.entries)
	})
}
