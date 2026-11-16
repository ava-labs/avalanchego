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

	// A tx describes a [txpool.LazyTransaction]. The fee cap is only relevant
	// when a base fee is set.
	type tx struct {
		id     byte
		feeCap uint64
		tipCap uint64
		seen   int64 // Unix seconds
	}

	//nolint:unused // Fields are only compared by require.Equal.
	type ordered struct {
		from common.Address
		id   byte
	}
	tests := []struct {
		name    string
		pending map[common.Address][]tx
		baseFee *uint256.Int
		want    []ordered
	}{
		{
			name: "empty",
		},
		{
			name: "low_tip_nonce_blocks_higher_tip_of_same_sender",
			pending: map[common.Address][]tx{
				alice:  {{id: 1, tipCap: 1, seen: 2}, {id: 2, tipCap: 10, seen: 3}},
				bob:    {{id: 3, tipCap: 5, seen: 1}},
				carlos: {{id: 4, tipCap: 7, seen: 4}},
			},
			want: []ordered{{carlos, 4}, {bob, 3}, {alice, 1}, {alice, 2}},
		},
		{
			name: "sender_interleaved_by_next_tip",
			pending: map[common.Address][]tx{
				alice: {{id: 1, tipCap: 9, seen: 1}, {id: 2, tipCap: 2, seen: 2}},
				bob:   {{id: 3, tipCap: 5, seen: 3}},
			},
			want: []ordered{{alice, 1}, {bob, 3}, {alice, 2}},
		},
		{
			name: "equal_tips_by_first_seen",
			pending: map[common.Address][]tx{
				alice: {{id: 1, tipCap: 3, seen: 2}},
				bob:   {{id: 2, tipCap: 3, seen: 1}},
			},
			want: []ordered{{bob, 2}, {alice, 1}},
		},
		{
			name: "equal_tips_and_first_seen_by_sender",
			pending: map[common.Address][]tx{
				bob:   {{id: 1, tipCap: 3, seen: 1}},
				alice: {{id: 2, tipCap: 3, seen: 1}},
			},
			want: []ordered{{alice, 2}, {bob, 1}},
		},
		{
			name: "effective_tip_capped_by_base_fee",
			pending: map[common.Address][]tx{
				alice: {{id: 1, feeCap: 15, tipCap: 10, seen: 1}},
				bob:   {{id: 2, feeCap: 1_000, tipCap: 7, seen: 2}},
			},
			baseFee: uint256.NewInt(10),
			want:    []ordered{{bob, 2}, {alice, 1}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pending := make(map[common.Address][]*txpool.LazyTransaction, len(tt.pending))
			for from, txs := range tt.pending {
				for _, tx := range txs {
					pending[from] = append(pending[from], &txpool.LazyTransaction{
						Hash:      common.Hash{tx.id},
						GasFeeCap: uint256.NewInt(tx.feeCap),
						GasTipCap: uint256.NewInt(tx.tipCap),
						Time:      time.Unix(tx.seen, 0),
					})
				}
			}

			var got []ordered
			for ltx := range byPriority(pending, tt.baseFee) {
				got = append(got, ordered{ltx.Sender, ltx.Hash[0]})
			}
			require.Equal(t, tt.want, got, "byPriority()")
		})
	}
}
