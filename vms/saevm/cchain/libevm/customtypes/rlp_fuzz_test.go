// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package customtypes

import (
	"testing"

	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/rlp"
	"github.com/stretchr/testify/require"
)

// FuzzHeaderRLP checks that any byte string the header decoder accepts
// re-encodes to exactly the same bytes, so a header's hash is a function of
// its decoded contents alone.
func FuzzHeaderRLP(f *testing.F) {
	for _, fork := range forkHeaders() {
		encoded, err := rlp.EncodeToBytes(WithHeaderExtra(fork.header, fork.extra))
		require.NoError(f, err, "rlp.EncodeToBytes(%s header)", fork.name)
		f.Add(encoded)
	}

	f.Fuzz(func(t *testing.T, input []byte) {
		header := new(types.Header)
		if err := rlp.DecodeBytes(input, header); err != nil {
			return
		}

		encoded, err := rlp.EncodeToBytes(header)
		require.NoError(t, err, "rlp.EncodeToBytes(decoded header)")
		require.Equal(t, input, encoded, "re-encoded header")

		again := new(types.Header)
		require.NoError(t, rlp.DecodeBytes(encoded, again), "rlp.DecodeBytes(re-encoded header)")
		require.Equal(t, header, again, "header after second decode")
		require.Equal(t, GetHeaderExtra(header), GetHeaderExtra(again), "HeaderExtra after second decode")
		require.Equal(t, header.Hash(), again.Hash(), "Header.Hash() after second decode")
	})
}

// FuzzBodyRLP is the same property for block bodies, which carry the block
// version and ExtData after the transactions and uncles.
func FuzzBodyRLP(f *testing.F) {
	body, extra := bodyWithNonZeroFields()
	extras.Body.Set(body, extra)
	encoded, err := rlp.EncodeToBytes(body)
	require.NoError(f, err, "rlp.EncodeToBytes(body)")
	f.Add(encoded)
	f.Add([]byte{0xc4, 0xc0, 0xc0, 0x80, 0x80})       // empty body: [[], [], 0, ""]
	f.Add([]byte{0xc4, 0xc0, 0xc3, 0xc0, 0x30, 0x30}) // uncle list holding an empty-list header; once decoded as [[], [], 0x30, "0"]

	f.Fuzz(func(t *testing.T, input []byte) {
		body := new(types.Body)
		if err := rlp.DecodeBytes(input, body); err != nil {
			return
		}

		encoded, err := rlp.EncodeToBytes(body)
		require.NoError(t, err, "rlp.EncodeToBytes(decoded body)")
		require.Equal(t, input, encoded, "re-encoded body")

		again := new(types.Body)
		require.NoError(t, rlp.DecodeBytes(encoded, again), "rlp.DecodeBytes(re-encoded body)")
		require.Equal(t, extras.Body.Get(body), extras.Body.Get(again), "BlockBodyExtra after second decode")
	})
}
