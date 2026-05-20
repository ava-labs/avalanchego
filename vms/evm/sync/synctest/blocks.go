// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package synctest

import (
	"math/big"
	"testing"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/trie"
)

// MakeChain builds n+1 empty-body blocks linked by ParentHash.
// blocks[0] is the genesis.
func MakeChain(t *testing.T, n int) []*types.Block {
	t.Helper()
	out := make([]*types.Block, n+1)

	out[0] = types.NewBlock(&types.Header{
		Number:      big.NewInt(0),
		Difficulty:  big.NewInt(1),
		GasLimit:    1_000_000,
		Root:        types.EmptyRootHash,
		TxHash:      types.EmptyTxsHash,
		ReceiptHash: types.EmptyReceiptsHash,
		UncleHash:   types.EmptyUncleHash,
		Extra:       []byte{},
	}, nil, nil, nil, trie.NewStackTrie(nil))

	for i := 1; i <= n; i++ {
		out[i] = types.NewBlock(&types.Header{
			ParentHash:  out[i-1].Hash(),
			Number:      big.NewInt(int64(i)),
			Difficulty:  big.NewInt(1),
			GasLimit:    1_000_000,
			Time:        uint64(i),
			Root:        types.EmptyRootHash,
			TxHash:      types.EmptyTxsHash,
			ReceiptHash: types.EmptyReceiptsHash,
			UncleHash:   types.EmptyUncleHash,
			Extra:       []byte{},
		}, nil, nil, nil, trie.NewStackTrie(nil))
	}
	return out
}

// BlockMap is an in-memory [block.Provider] keyed by hash and
// canonical height. [BlockMap.GetBlock] returns nil if the hash is
// missing or the height doesn't match.
type BlockMap struct {
	byHash   map[common.Hash]*types.Block
	byHeight map[uint64]*types.Block
}

func NewBlockMap(blocks []*types.Block) *BlockMap {
	m := &BlockMap{
		byHash:   make(map[common.Hash]*types.Block, len(blocks)),
		byHeight: make(map[uint64]*types.Block, len(blocks)),
	}
	for _, b := range blocks {
		m.byHash[b.Hash()] = b
		m.byHeight[b.NumberU64()] = b
	}
	return m
}

func (m *BlockMap) GetBlock(hash common.Hash, height uint64) *types.Block {
	b, ok := m.byHash[hash]
	if !ok || b.NumberU64() != height {
		return nil
	}
	return b
}

func (m *BlockMap) GetBlockByHeight(height uint64) *types.Block {
	return m.byHeight[height]
}
