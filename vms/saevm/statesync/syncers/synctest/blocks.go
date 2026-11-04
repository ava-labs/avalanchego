// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package synctest

import (
	"math/big"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/libevm/options"
	"github.com/ava-labs/libevm/trie"
)

type chainConfig struct {
	txsPerBlock int
}

// ChainOption configures [MakeChain].
type ChainOption = options.Option[chainConfig]

// WithTxsPerBlock gives every non-genesis block n transactions.
func WithTxsPerBlock(n int) ChainOption {
	return options.Func[chainConfig](func(c *chainConfig) {
		if n > 0 {
			c.txsPerBlock = n
		}
	})
}

// MakeChain builds n+1 blocks linked by ParentHash, with empty bodies by
// default. blocks[0] is the genesis.
func MakeChain(tb testing.TB, n int, opts ...ChainOption) []*types.Block {
	tb.Helper()

	var cfg chainConfig
	options.ApplyTo(&cfg, opts...)

	out := make([]*types.Block, n+1)

	out[0] = types.NewBlock(&types.Header{
		Number:     big.NewInt(0),
		Difficulty: big.NewInt(1),
		GasLimit:   1_000_000,
		Root:       types.EmptyRootHash,
		Extra:      []byte{},
	}, nil, nil, nil, trie.NewStackTrie(nil))

	for i := 1; i <= n; i++ {
		txs := makeTxs(cfg.txsPerBlock, uint64(i))
		out[i] = types.NewBlock(&types.Header{
			ParentHash: out[i-1].Hash(),
			Number:     big.NewInt(int64(i)),
			Difficulty: big.NewInt(1),
			GasLimit:   1_000_000,
			Time:       uint64(i),
			Root:       types.EmptyRootHash,
			Extra:      []byte{},
		}, txs, nil, nil, trie.NewStackTrie(nil))
	}
	return out
}

// makeTxs returns n transactions unique to the block at height, so no two
// blocks share a transaction root.
func makeTxs(n int, height uint64) []*types.Transaction {
	txs := make([]*types.Transaction, n)
	for i := range txs {
		nonce := height*uint64(n) + uint64(i)
		txs[i] = types.NewTransaction(nonce, common.Address{}, big.NewInt(1), 21_000, big.NewInt(1), nil)
	}
	return txs
}

// NewBlockDB returns an in-memory database holding blocks as the canonical
// chain, ready to be served by a block handler.
func NewBlockDB(blocks []*types.Block) ethdb.Database {
	db := rawdb.NewMemoryDatabase()
	for _, b := range blocks {
		rawdb.WriteBlock(db, b)
		rawdb.WriteCanonicalHash(db, b.Hash(), b.NumberU64())
	}
	return db
}
