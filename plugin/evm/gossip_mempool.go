// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"context"
	"fmt"
	"sync"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ethereum/go-ethereum/log"

	"github.com/ava-labs/avalanchego/network/p2p/gossip"

	"github.com/ava-labs/subnet-evm/core"
	"github.com/ava-labs/subnet-evm/core/txpool"
	"github.com/ava-labs/subnet-evm/core/types"
)

var (
	_ gossip.Gossipable     = (*GossipTx)(nil)
	_ gossip.Set[*GossipTx] = (*GossipTxPool)(nil)
)

func NewGossipTxPool(mempool *txpool.TxPool) (*GossipTxPool, error) {
	bloom, err := gossip.NewBloomFilter(txGossipBloomMaxItems, txGossipBloomFalsePositiveRate)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize bloom filter: %w", err)
	}

	return &GossipTxPool{
		mempool:    mempool,
		pendingTxs: make(chan core.NewTxsEvent),
		bloom:      bloom,
	}, nil
}

type GossipTxPool struct {
	mempool    *txpool.TxPool
	pendingTxs chan core.NewTxsEvent

	bloom *gossip.BloomFilter
	lock  sync.RWMutex
}

func (g *GossipTxPool) Subscribe(ctx context.Context) {
	g.mempool.SubscribeNewTxsEvent(g.pendingTxs)

	for {
		select {
		case <-ctx.Done():
			log.Debug("shutting down subscription")
			return
		case pendingTxs := <-g.pendingTxs:
			g.lock.Lock()
			for _, pendingTx := range pendingTxs.Txs {
				tx := &GossipTx{Tx: pendingTx}
				g.bloom.Add(tx)
				reset, err := gossip.ResetBloomFilterIfNeeded(g.bloom, txGossipMaxFalsePositiveRate)
				if err != nil {
					log.Error("failed to reset bloom filter", "err", err)
					continue
				}

				if reset {
					log.Debug("resetting bloom filter", "reason", "reached max filled ratio")

					g.mempool.IteratePending(func(tx *types.Transaction) bool {
						g.bloom.Add(&GossipTx{Tx: pendingTx})
						return true
					})
				}
			}
			g.lock.Unlock()
		}
	}
}

// Add enqueues the transaction to the mempool. Subscribe should be called
// to receive an event if tx is actually added to the mempool or not.
func (g *GossipTxPool) Add(tx *GossipTx) error {
	return g.mempool.AddRemotes([]*types.Transaction{tx.Tx})[0]
}

func (g *GossipTxPool) Iterate(f func(tx *GossipTx) bool) {
	g.mempool.IteratePending(func(tx *types.Transaction) bool {
		return f(&GossipTx{Tx: tx})
	})
}

func (g *GossipTxPool) GetFilter() ([]byte, []byte, error) {
	g.lock.RLock()
	defer g.lock.RUnlock()

	bloom, err := g.bloom.Bloom.MarshalBinary()
	salt := g.bloom.Salt

	return bloom, salt[:], err
}

type GossipTx struct {
	Tx *types.Transaction
}

func (tx *GossipTx) GetID() ids.ID {
	return ids.ID(tx.Tx.Hash())
}

func (tx *GossipTx) Marshal() ([]byte, error) {
	return tx.Tx.MarshalBinary()
}

func (tx *GossipTx) Unmarshal(bytes []byte) error {
	tx.Tx = &types.Transaction{}
	return tx.Tx.UnmarshalBinary(bytes)
}
