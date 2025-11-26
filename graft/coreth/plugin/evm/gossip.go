// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// TODO: move to network

package evm

import (
	"github.com/ava-labs/libevm/core/types"

	"github.com/ava-labs/avalanchego/graft/coreth/core/txpool"
	"github.com/ava-labs/avalanchego/graft/coreth/eth"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p/gossip"
	"github.com/ava-labs/avalanchego/utils"

	ethcommon "github.com/ava-labs/libevm/common"
)

const pendingTxsBuffer = 10

var (
	_ gossip.Gossipable            = (*gossipTx)(nil)
	_ gossip.Marshaller[*gossipTx] = (*gossipTxMarshaller)(nil)
	_ gossip.Set[*gossipTx]        = (*gossipTxPool)(nil)

	_ eth.PushGossiper = (*atomicPushGossiper)(nil)
)

func newGossipTxPool(mempool *txpool.TxPool) *gossipTxPool {
	return &gossipTxPool{
		mempool: mempool,
	}
}

type gossipTxPool struct {
	mempool *txpool.TxPool
}

// Add enqueues the transaction to the mempool.
func (g *gossipTxPool) Add(tx *gossipTx) error {
	return g.mempool.Add([]*types.Transaction{tx.tx}, false, false)[0]
}

// Has should just return whether or not the [txID] is still in the mempool,
// not whether it is in the mempool AND pending.
func (g *gossipTxPool) Has(txID ids.ID) bool {
	return g.mempool.Has(ethcommon.Hash(txID))
}

func (g *gossipTxPool) Iterate(f func(tx *gossipTx) bool) {
	g.mempool.IteratePending(func(tx *types.Transaction) bool {
		return f(&gossipTx{tx: tx})
	})
}

func (g *gossipTxPool) Len() int {
	return g.mempool.PendingSize(txpool.PendingFilter{})
}

type gossipTxMarshaller struct{}

func (gossipTxMarshaller) MarshalGossip(tx *gossipTx) ([]byte, error) {
	return tx.tx.MarshalBinary()
}

func (gossipTxMarshaller) UnmarshalGossip(bytes []byte) (*gossipTx, error) {
	tx := &gossipTx{
		tx: &types.Transaction{},
	}

	return tx, tx.tx.UnmarshalBinary(bytes)
}

type gossipTx struct {
	tx *types.Transaction
}

func (tx *gossipTx) GossipID() ids.ID {
	return ids.ID(tx.tx.Hash())
}

// atomicPushGossiper is used by the ETH backend to push transactions issued
// over the RPC and added to the mempool to peers.
type atomicPushGossiper struct {
	pusher *utils.Atomic[*gossip.PushGossiper[*gossipTx]]
	set    **gossip.SetWithBloomFilter[*gossipTx]
}

func (e *atomicPushGossiper) Add(tx *types.Transaction) {
	// eth.Backend is initialized before the pusher is created, so we just
	// ignore any gossip requests until it is set.
	pusher := e.pusher.Get()
	if pusher == nil {
		return
	}
	_ = (*e.set).AddToBloom(ids.ID(tx.Hash()))
	pusher.Add(&gossipTx{tx})
}
