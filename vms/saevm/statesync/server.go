package statesync

import (
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/vms/evm/sync/block"
	"github.com/ava-labs/avalanchego/vms/evm/sync/code"
	"github.com/ava-labs/avalanchego/vms/evm/sync/evmstate"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/ethdb"
	"github.com/ava-labs/libevm/triedb"
)

func (h *SummaryHandler) RegisterServer(tdb *triedb.Database) error {
	blockHandler := block.NewHandler(h.snowCtx.Log, block.NewResponder(&blockProvider{h.db}))
	if err := h.network.AddHandler(p2p.EVMBlockRequestHandlerID, blockHandler); err != nil {
		return err
	}

	evmHandler := evmstate.NewHandler(h.snowCtx.Log, evmstate.NewResponder(tdb, common.HashLength, nil /*snapshot TODO*/))
	if err := h.network.AddHandler(p2p.EVMLeafRequestHandlerID, evmHandler); err != nil {
		return err
	}

	codeHandler := code.NewHandler(h.snowCtx.Log, code.NewResponder(h.db))
	if err := h.network.AddHandler(p2p.EVMCodeRequestHandlerID, codeHandler); err != nil {
		return err
	}

	return nil
}

var _ block.Provider = (*blockProvider)(nil)

type blockProvider struct {
	db ethdb.Database
}

func (b *blockProvider) GetBlock(hash common.Hash, height uint64) *types.Block {
	return rawdb.ReadBlock(b.db, hash, height)
}

func (b *blockProvider) GetBlockByHeight(height uint64) *types.Block {
	hash := rawdb.ReadCanonicalHash(b.db, height)
	return b.GetBlock(hash, height)
}
