// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package bloom

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

func NewMaliciousFilter(numHashes, numEntries int) *Filter {
	f := &Filter{
		numBits:   uint64(numEntries * bitsPerByte),
		hashSeeds: make([]uint64, numHashes),
		entries:   make([]byte, numEntries),
		count:     0,
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
			err:   errInvalidNumHashes,
		},
		{
			bytes: NewMaliciousFilter(0, 1).Marshal(),
			err:   errTooFewHashes,
		},
		{
			bytes: NewMaliciousFilter(17, 1).Marshal(),
			err:   errTooManyHashes,
		},
		{
			bytes: NewMaliciousFilter(1, 0).Marshal(),
			err:   errTooFewEntries,
		},
		{
			bytes: []byte{
				0x01, // num hashes = 1
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

	for b.Loop() {
		_, _ = Parse(bytes)
	}
}

func BenchmarkContains(b *testing.B) {
	f := NewMaliciousFilter(maxHashes, 1)

	for b.Loop() {
		f.Contains(1)
	}
}

func FuzzParseThenMarshal(f *testing.F) {
	f.Fuzz(func(t *testing.T, bytes []byte) {
		f, err := Parse(bytes)
		if err != nil {
			return
		}

		marshalledBytes := marshal(f.hashSeeds, f.entries)
		require.Equal(t, bytes, marshalledBytes)
	})
}

func FuzzMarshalThenParse(f *testing.F) {
	f.Fuzz(func(t *testing.T, numHashes int, entries []byte) {
		require := require.New(t)

		hashSeeds, err := newHashSeeds(numHashes)
		if err != nil {
			return
		}
		if len(entries) < minEntries {
			return
		}

		marshalledBytes := marshal(hashSeeds, entries)
		rf, err := Parse(marshalledBytes)
		require.NoError(err)
		require.Equal(hashSeeds, rf.hashSeeds)
		require.Equal(entries, rf.entries)
	})
}
