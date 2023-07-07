// (c) 2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package predicateutils

import (
	"bytes"
	"testing"

	"github.com/ava-labs/avalanchego/utils"
	"github.com/stretchr/testify/require"
)

func testBytesToHashSlice(t testing.TB, b []byte) {
	hashSlice := BytesToHashSlice(b)

	copiedBytes := HashSliceToBytes(hashSlice)

	if len(b)%32 == 0 {
		require.Equal(t, b, copiedBytes)
	} else {
		require.Equal(t, b, copiedBytes[:len(b)])
		// Require that any additional padding is all zeroes
		padding := copiedBytes[len(b):]
		require.Equal(t, bytes.Repeat([]byte{0x00}, len(padding)), padding)
	}
}

func FuzzHashSliceToBytes(f *testing.F) {
	for i := 0; i < 100; i++ {
		f.Add(utils.RandomBytes(i))
	}

	f.Fuzz(func(t *testing.T, b []byte) {
		testBytesToHashSlice(t, b)
	})
}

func testPackPredicate(t testing.TB, b []byte) {
	packedPredicate := PackPredicate(b)
	unpackedPredicated, err := UnpackPredicate(packedPredicate)
	require.NoError(t, err)
	require.Equal(t, b, unpackedPredicated)
}

func FuzzPackPredicate(f *testing.F) {
	for i := 0; i < 100; i++ {
		f.Add(utils.RandomBytes(i))
	}

	f.Fuzz(func(t *testing.T, b []byte) {
		testPackPredicate(t, b)
	})
}

func FuzzUnpackInvalidPredicate(f *testing.F) {
	// Seed the fuzzer with non-zero length padding of zeroes or non-zeroes.
	for i := 1; i < 100; i++ {
		f.Add(utils.RandomBytes(i))
		f.Add(make([]byte, i))
	}

	f.Fuzz(func(t *testing.T, b []byte) {
		// Ensure that adding the invalid padding to any length correctly packed predicate
		// results in failing to unpack it.
		for _, l := range []int{0, 1, 31, 32, 33, 63, 64, 65} {
			validPredicate := PackPredicate(utils.RandomBytes(l))
			invalidPredicate := append(validPredicate, b...)
			_, err := UnpackPredicate(invalidPredicate)
			require.Error(t, err)
		}
	})
}
