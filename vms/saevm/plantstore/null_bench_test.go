// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package plantstore

import (
	"math/big"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/holiman/uint256"

	"github.com/ava-labs/avalanchego/utils/logging"
)

// BenchmarkNullTipSlotRead: a slot read at the newest root with 128 blocks of history
// (1,000 slot writes each) above a base of 100,000 slots.
func BenchmarkNullTipSlotRead(b *testing.B) {
	n := newNullDB(logging.NoLog{})
	c := common.BigToAddress(big.NewInt(0x300))
	g := newWrites()
	g.accts[c] = &types.StateAccount{Nonce: 1, Balance: uint256.NewInt(0), Root: types.EmptyRootHash, CodeHash: types.EmptyCodeHash[:]}
	g.slots[c] = map[common.Hash]common.Hash{}
	for i := 0; i < 100_000; i++ {
		g.slots[c][common.BigToHash(big.NewInt(int64(i)))] = common.BigToHash(big.NewInt(int64(i + 1)))
	}
	root, err := n.commit(types.EmptyRootHash, g)
	if err != nil {
		b.Fatal(err)
	}
	for blk := 0; blk < 2*Window; blk++ {
		w := newWrites()
		w.slots[c] = map[common.Hash]common.Hash{}
		for i := 0; i < 1000; i++ {
			w.slots[c][common.BigToHash(big.NewInt(int64(blk*1000+i)))] = common.BigToHash(big.NewInt(int64(blk + 7)))
		}
		if root, err = n.commit(root, w); err != nil {
			b.Fatal(err)
		}
	}
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		if _, err := n.slot(root, c, common.BigToHash(big.NewInt(int64(i%100_000)))); err != nil {
			b.Fatal(err)
		}
	}
}
