// Copyright (C) 2025-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package txgossip

import (
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/state"
	"github.com/ava-labs/libevm/core/txpool"
	"github.com/ava-labs/libevm/core/txpool/legacypool"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/params"

	"github.com/ava-labs/strevm/blocks"
	"github.com/ava-labs/strevm/saexec"

	// Imported for [core.ChainHeadEvent] comment resolution. Already a
	// downstream dependency.
	_ "github.com/ava-labs/libevm/core"
)

// A BlockChain is the union of [txpool.BlockChain] and [legacypool.BlockChain].
type BlockChain interface {
	txpool.BlockChain
	legacypool.BlockChain
}

// NewBlockChain wraps an [saexec.Executor] to be compatible with a
// non-blob-transaction mempool.
//
// The wrappers's `CurrentBlock()` method returns the last executed, while the
// `StateAt()` method ignores its argument and always opens the latest
// post-execution state root. The [core.ChainHeadEvent] subscription therefore
// acts only to inform the mempool of some new state, but not which specific
// root as the event contains a [types.Header] carrying the (ignored)
// last-settled state root.
func NewBlockChain(exec *saexec.Executor, blocks blocks.EthBlockSource) BlockChain {
	return &blockchain{
		Executor: exec,
		blocks:   blocks,
	}
}

type blockchain struct {
	*saexec.Executor // exposes SubscribeChainHeadEvent()
	blocks           blocks.EthBlockSource
}

func (bc *blockchain) Config() *params.ChainConfig {
	return bc.ChainConfig()
}

func (bc *blockchain) CurrentBlock() *types.Header {
	return bc.LastExecuted().Header()
}

func (bc *blockchain) GetBlock(hash common.Hash, number uint64) *types.Block {
	b, ok := bc.blocks(hash, number)
	if !ok {
		return nil
	}
	return b
}

func (bc *blockchain) StateAt(common.Hash) (*state.StateDB, error) {
	return bc.StateDB(bc.LastExecuted().PostExecutionStateRoot())
}
