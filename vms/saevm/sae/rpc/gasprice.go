// Copyright (C) 2025-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package rpc

import (
	"github.com/ava-labs/libevm/core/rawdb"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/event"
	"github.com/ava-labs/libevm/rpc"

	"github.com/ava-labs/avalanchego/vms/saevm/blocks"
	"github.com/ava-labs/avalanchego/vms/saevm/gasprice"
)

type estimatorBackend struct {
	chain Chain
}

var _ gasprice.Backend = (*estimatorBackend)(nil)

func (e *estimatorBackend) BlockByNumber(n rpc.BlockNumber) (*types.Block, error) {
	return readByNumber(e.chain, n, rawdb.ReadBlock)
}

func (e *estimatorBackend) LastAcceptedBlock() *blocks.Block {
	return e.chain.LastAccepted()
}

func (e *estimatorBackend) ResolveBlockNumber(bn rpc.BlockNumber) (uint64, error) {
	return blocks.ResolveRPCNumber(e.chain, bn)
}

func (e *estimatorBackend) SubscribeAcceptedBlocks(ch chan<- *blocks.Block) event.Subscription {
	return e.chain.SubscribeAcceptedBlocks(ch)
}
