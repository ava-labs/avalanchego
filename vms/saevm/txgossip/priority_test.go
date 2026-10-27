// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txgossip

import (
	"testing"
	"time"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/txpool"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/require"
)

func TestByPriority(t *testing.T) {
	var (
		juan   = common.Address{1}
		sam    = common.Address{2}
		austin = common.Address{3}
	)

	tx := func(id byte, feeCap, tipCap uint64, seen int64) *txpool.LazyTransaction {
		return &txpool.LazyTransaction{
			Hash:      common.Hash{id},
			GasFeeCap: uint256.NewInt(feeCap),
			GasTipCap: uint256.NewInt(tipCap),
			Time:      time.Unix(seen, 0),
		}
	}
	const highFeeCap = 1_000

	//nolint:unused // Fields are only compared by require.Equal.
	type ordered struct {
		from common.Address
		id   byte
	}
	tests := []struct {
		name    string
		pending map[common.Address][]*txpool.LazyTransaction
		baseFee *uint256.Int
		want    []ordered
	}{
		{
			name: "empty",
		},
		{
			name: "low_tip_nonce_blocks_higher_tip_of_same_sender",
			pending: map[common.Address][]*txpool.LazyTransaction{
				juan:   {tx(1, highFeeCap, 1, 2), tx(2, highFeeCap, 10, 3)},
				sam:    {tx(3, highFeeCap, 5, 1)},
				austin: {tx(4, highFeeCap, 7, 4)},
			},
			want: []ordered{{austin, 4}, {sam, 3}, {juan, 1}, {juan, 2}},
		},
		{
			name: "sender_interleaved_by_next_tip",
			pending: map[common.Address][]*txpool.LazyTransaction{
				juan: {tx(1, highFeeCap, 9, 1), tx(2, highFeeCap, 2, 2)},
				sam:  {tx(3, highFeeCap, 5, 3)},
			},
			want: []ordered{{juan, 1}, {sam, 3}, {juan, 2}},
		},
		{
			name: "equal_tips_by_first_seen",
			pending: map[common.Address][]*txpool.LazyTransaction{
				juan: {tx(1, highFeeCap, 3, 2)},
				sam:  {tx(2, highFeeCap, 3, 1)},
			},
			want: []ordered{{sam, 2}, {juan, 1}},
		},
		{
			name: "equal_tips_and_first_seen_by_sender",
			pending: map[common.Address][]*txpool.LazyTransaction{
				sam:  {tx(1, highFeeCap, 3, 1)},
				juan: {tx(2, highFeeCap, 3, 1)},
			},
			want: []ordered{{juan, 2}, {sam, 1}},
		},
		{
			name: "effective_tip_capped_by_base_fee",
			pending: map[common.Address][]*txpool.LazyTransaction{
				juan: {tx(1, 15, 10, 1)}, // effective tip of 5
				sam:  {tx(2, highFeeCap, 7, 2)},
			},
			baseFee: uint256.NewInt(10),
			want:    []ordered{{sam, 2}, {juan, 1}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Map iteration order is random, so repeat to catch any dependence.
			for range 100 {
				var got []ordered
				for _, ltx := range byPriority(tt.pending, tt.baseFee) {
					got = append(got, ordered{ltx.Sender, ltx.Hash[0]})
				}
				require.Equal(t, tt.want, got, "byPriority()")
			}
		})
	}
}
