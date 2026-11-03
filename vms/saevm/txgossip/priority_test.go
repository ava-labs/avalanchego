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
		alice  = common.Address{1}
		bob    = common.Address{2}
		carlos = common.Address{3}
	)

	tx := func(id byte, feeCap, tipCap uint64, seen int64) *txpool.LazyTransaction {
		return &txpool.LazyTransaction{
			Hash:      common.Hash{id},
			GasFeeCap: uint256.NewInt(feeCap),
			GasTipCap: uint256.NewInt(tipCap),
			Time:      time.Unix(seen, 0),
		}
	}
	const (
		highFeeCap = 1_000
		lowFeeCap  = 15
	)

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
				alice:  {tx(1, highFeeCap, 1, 2), tx(2, highFeeCap, 10, 3)},
				bob:    {tx(3, highFeeCap, 5, 1)},
				carlos: {tx(4, highFeeCap, 7, 4)},
			},
			want: []ordered{{carlos, 4}, {bob, 3}, {alice, 1}, {alice, 2}},
		},
		{
			name: "sender_interleaved_by_next_tip",
			pending: map[common.Address][]*txpool.LazyTransaction{
				alice: {tx(1, highFeeCap, 9, 1), tx(2, highFeeCap, 2, 2)},
				bob:   {tx(3, highFeeCap, 5, 3)},
			},
			want: []ordered{{alice, 1}, {bob, 3}, {alice, 2}},
		},
		{
			name: "equal_tips_by_first_seen",
			pending: map[common.Address][]*txpool.LazyTransaction{
				alice: {tx(1, highFeeCap, 3, 2)},
				bob:   {tx(2, highFeeCap, 3, 1)},
			},
			want: []ordered{{bob, 2}, {alice, 1}},
		},
		{
			name: "equal_tips_and_first_seen_by_sender",
			pending: map[common.Address][]*txpool.LazyTransaction{
				bob:   {tx(1, highFeeCap, 3, 1)},
				alice: {tx(2, highFeeCap, 3, 1)},
			},
			want: []ordered{{alice, 2}, {bob, 1}},
		},
		{
			name: "effective_tip_capped_by_base_fee",
			pending: map[common.Address][]*txpool.LazyTransaction{
				alice: {tx(1, lowFeeCap, 10, 1)},
				bob:   {tx(2, highFeeCap, 7, 2)},
			},
			baseFee: uint256.NewInt(10),
			want:    []ordered{{bob, 2}, {alice, 1}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got []ordered
			for ltx := range byPriority(tt.pending, tt.baseFee) {
				got = append(got, ordered{ltx.Sender, ltx.Hash[0]})
			}
			require.Equal(t, tt.want, got, "byPriority()")
		})
	}
}
