// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txtest

import (
	"testing"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/libevm/common"
	"github.com/stretchr/testify/require"
)

func FuzzRoundTrip(f *testing.F) {
	f.Fuzz(func(t *testing.T, data []byte) {
		f := decoder{
			Data: data,
			Addresses: []common.Address{
				{1},
			},
			AssetIDs: []ids.ID{
				{2},
			},
		}
		want := f.Tx()

		var e encoder
		e.tx(want)
		f.Data = e
		got := f.Tx()
		require.Equal(t, want, got, "decode(encode(tx)) == tx")
	})
}
