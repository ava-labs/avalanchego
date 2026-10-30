// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txgossip

import (
	"maps"
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
				alice: {tx(1, 15, 10, 1)}, // effective tip of 5
				bob:   {tx(2, highFeeCap, 7, 2)},
			},
			baseFee: uint256.NewInt(10),
			want:    []ordered{{bob, 2}, {alice, 1}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got []ordered
			for _, ltx := range byPriority(tt.pending, tt.baseFee) {
				got = append(got, ordered{ltx.Sender, ltx.Hash[0]})
			}
			require.Equal(t, tt.want, got, "byPriority()")
		})
	}
}

// FuzzByPriority checks that each output transaction outranks the next one
// from every other sender. Only one output can pass, so map iteration order
// can't change it.
func FuzzByPriority(f *testing.F) {
	// sender, tip, seen (a sender's rows are in nonce order)
	f.Add([]byte{
		0, 1, 2, // Low tip holds back...
		0, 3, 3, // ...this higher tip from the same sender.
		1, 2, 1, // Same tip and time as sender 2,
		2, 2, 1, // so the lower address goes first.
	}) // Output senders: 1, 2, 0, 0
	f.Add([]byte{
		0, 1, 1, // Everything ties except the sender.
		1, 1, 1,
		2, 1, 1,
		3, 1, 1,
	}) // Output senders: 0, 1, 2, 3

	f.Fuzz(func(t *testing.T, data []byte) {
		// Every 3 bytes are a transaction's sender, tip, and first-seen time.
		// Each has only 4 values, so ties are common.
		pending := make(map[common.Address][]*txpool.LazyTransaction)
		for ; len(data) >= 3; data = data[3:] {
			sender, tip, seen := data[0]%4, data[1]%4, data[2]%4
			from := common.Address{sender}
			pending[from] = append(pending[from], &txpool.LazyTransaction{
				GasTipCap: uint256.NewInt(uint64(tip)),
				Time:      time.Unix(int64(seen), 0),
			})
		}

		// outranks reports whether `a` has higher priority than `b`, which is
		// from `bSender`. With a nil base fee, the effective tip is the tip cap.
		outranks := func(a *LazyTransaction, bSender common.Address, b *txpool.LazyTransaction) bool {
			if c := a.GasTipCap.Cmp(b.GasTipCap); c != 0 {
				return c > 0
			}
			if c := a.Time.Compare(b.Time); c != 0 {
				return c < 0
			}
			return a.Sender.Cmp(bSender) < 0
		}

		// remaining holds each sender's transactions that are not output yet.
		remaining := maps.Clone(pending)
		for i, got := range byPriority(pending, nil) {
			txs := remaining[got.Sender]
			require.NotEmptyf(t, txs, "byPriority()[%d] is an extra transaction from %v", i, got.Sender)
			require.Samef(t, txs[0], got.LazyTransaction, "byPriority()[%d] is not the next transaction from %v", i, got.Sender)
			remaining[got.Sender] = txs[1:]

			for sender, txs := range remaining {
				if sender == got.Sender || len(txs) == 0 {
					continue
				}
				require.Truef(t, outranks(got, sender, txs[0]), "byPriority()[%d] from %v does not outrank the next transaction from %v", i, got.Sender, sender)
			}
		}
		for sender, txs := range remaining {
			require.Emptyf(t, txs, "byPriority() is missing transactions from %v", sender)
		}
	})
}
