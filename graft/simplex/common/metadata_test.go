// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package common

import (
	"crypto/rand"
	"crypto/sha256"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMetadata(t *testing.T) {
	var prev Digest
	var digest Digest

	_, err := rand.Read(prev[:])
	require.NoError(t, err)

	_, err = rand.Read(digest[:])
	require.NoError(t, err)

	bh := BlockHeader{
		ProtocolMetadata: ProtocolMetadata{
			Version: 1,
			Round:   2,
			Seq:     3,
			Epoch:   4,
			Prev:    prev,
		},
		Digest: digest,
	}

	var bh2 BlockHeader
	require.NoError(t, bh2.FromBytes(bh.Bytes()))
	require.True(t, bh.Equals(&bh2))
}

func FuzzMetadata(f *testing.F) {
	f.Fuzz(func(t *testing.T, version uint8, round uint64, seq uint64, epoch uint64, prevPreimage []byte, digestPreimage []byte) {

		prev := sha256.Sum256(prevPreimage)
		digest := sha256.Sum256(digestPreimage)

		bh := BlockHeader{
			ProtocolMetadata: ProtocolMetadata{
				Version: version,
				Round:   round,
				Seq:     seq,
				Epoch:   epoch,
				Prev:    prev,
			},
			Digest: digest,
		}

		var bh2 BlockHeader
		require.NoError(t, bh2.FromBytes(bh.Bytes()))
		require.True(t, bh.Equals(&bh2))
	})
}
