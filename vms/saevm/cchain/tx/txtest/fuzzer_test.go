// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txtest

import (
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/ids"
)

func FuzzRoundTrip(f *testing.F) {
	f.Fuzz(func(t *testing.T, data []byte) {
		d := decoder{
			data: data,
			addresses: []common.Address{
				{1},
			},
			assetIDs: []ids.ID{
				{2},
			},
		}
		want := d.tx()

		var e encoder
		e.tx(want)
		d.data = e
		got := d.tx()
		require.Equal(t, want, got, "decode(encode(tx)) == tx")
	})
}
