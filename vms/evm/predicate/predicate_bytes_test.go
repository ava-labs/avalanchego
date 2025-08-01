// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package predicate

import (
	"bytes"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/utils"
)

func testPackPredicate(t testing.TB, b []byte) {
	packedPredicate := PackPredicate(b)
	unpackedPredicate, err := UnpackPredicate(packedPredicate)
	require.NoError(t, err)
	require.Equal(t, b, unpackedPredicate)
}

func FuzzPackPredicate(f *testing.F) {
	for i := range 100 {
		f.Add(utils.RandomBytes(i))
	}

	f.Fuzz(func(t *testing.T, b []byte) {
		testPackPredicate(t, b)
	})
}

func TestUnpackInvalidPredicate(t *testing.T) {
	require := require.New(t)
	// Predicate encoding requires a 0xff delimiter byte followed by padding of all zeroes, so any other
	// excess padding should invalidate the predicate.
	paddingCases := make([][]byte, 0, 200)
	for i := 1; i < 100; i++ {
		paddingCases = append(paddingCases, bytes.Repeat([]byte{0xee}, i))
		paddingCases = append(paddingCases, make([]byte, i))
	}

	for _, l := range []int{0, 1, 31, 32, 33, 63, 64, 65} {
		validPredicate := PackPredicate(utils.RandomBytes(l))

		for _, padding := range paddingCases {
			invalidPredicate := make([]byte, len(validPredicate)+len(padding))
			copy(invalidPredicate, validPredicate)
			copy(invalidPredicate[len(validPredicate):], padding)
			_, err := UnpackPredicate(invalidPredicate)

			// Check for either error type since different padding can cause different errors.
			// Zero padding cases trigger ErrInvalidPadding (padding length check fails).
			// Non-zero padding cases trigger ErrInvalidEndDelimiter (delimiter check fails).
			require.True(errors.Is(err, ErrInvalidPadding) || errors.Is(err, ErrInvalidEndDelimiter),
				"Predicate length %d, Padding length %d (0x%x)", len(validPredicate), len(padding), invalidPredicate)
		}
	}
}
