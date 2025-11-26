// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// TODO: move to network

package evm

import (
	"context"
	"fmt"
	"sync"

	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/event"
	"github.com/ava-labs/libevm/log"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/graft/coreth/core"
	"github.com/ava-labs/avalanchego/graft/coreth/core/txpool"
	"github.com/ava-labs/avalanchego/graft/coreth/eth"
	"github.com/ava-labs/avalanchego/graft/coreth/plugin/evm/config"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p/gossip"

	ethcommon "github.com/ava-labs/libevm/common"
)

const pendingTxsBuffer = 10

var (
	_ gossip.Gossipable               = (*GossipEthTx)(nil)
	_ gossip.Marshaller[*GossipEthTx] = (*GossipEthTxMarshaller)(nil)
	_ gossip.Set[*GossipEthTx]        = (*GossipEthTxPool)(nil)

	_ eth.PushGossiper = (*EthPushGossiper)(nil)

	errSubscribing = fmt.Errorf("subscribing to the mempool failed")
)

// NewGossipEthTxPool creates a new GossipEthTxPool.
//
// If a nil error is returned, UpdateBloomFilter must be called.
func NewGossipEthTxPool(mempool *txpool.TxPool, registerer prometheus.Registerer) (*GossipEthTxPool, error) {
	bloom, err := gossip.NewBloomFilter(
		registerer,
		"eth_tx_bloom_filter",
		config.TxGossipBloomMinTargetElements,
		config.TxGossipBloomTargetFalsePositiveRate,
		config.TxGossipBloomResetFalsePositiveRate,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize bloom filter: %w", err)
	}

	pendingTxs := make(chan core.NewTxsEvent, pendingTxsBuffer)
	sub := mempool.SubscribeTransactions(pendingTxs, true)
	if sub == nil {
		return nil, errSubscribing
	}

	return &GossipEthTxPool{
		mempool:    mempool,
		pendingTxs: make(chan core.NewTxsEvent, pendingTxsBuffer),
		sub:        sub,
		bloom:      bloom,
	}, nil
}

type GossipEthTxPool struct {
	mempool    *txpool.TxPool
	pendingTxs chan core.NewTxsEvent
	sub        event.Subscription

	lock  sync.RWMutex
	bloom *gossip.BloomFilter
}

// UpdateBloomFilter continuously listens for new pending transactions from the
// mempool and adds them to the bloom filter. If the bloom filter reaches its
// capacity, it is reset and all pending transactions are re-added.
func (g *GossipEthTxPool) UpdateBloomFilter(ctx context.Context) {
	defer g.sub.Unsubscribe()

	for {
		select {
		case <-ctx.Done():
			log.Debug("shutting down subscription")
			return
		case pendingTxs, ok := <-g.pendingTxs:
			if !ok {
				log.Debug("pending txs channel closed, shutting down subscription")
				return
			}

			g.lock.Lock()
			optimalElements := (g.mempool.PendingSize(txpool.PendingFilter{}) + len(pendingTxs.Txs)) * config.TxGossipBloomChurnMultiplier
			for _, pendingTx := range pendingTxs.Txs {
				tx := &GossipEthTx{Tx: pendingTx}
				g.bloom.Add(tx)
				reset, err := gossip.ResetBloomFilterIfNeeded(g.bloom, optimalElements)
				if err != nil {
					log.Error("failed to reset bloom filter", "err", err)
					continue
				}

				if reset {
					log.Debug("resetting bloom filter", "reason", "reached max filled ratio")

					g.mempool.IteratePending(func(tx *types.Transaction) bool {
						g.bloom.Add(&GossipEthTx{Tx: tx})
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
func (g *GossipEthTxPool) Add(tx *GossipEthTx) error {
	return g.mempool.Add([]*types.Transaction{tx.Tx}, false, false)[0]
}

// Has should just return whether or not the [txID] is still in the mempool,
// not whether it is in the mempool AND pending.
func (g *GossipEthTxPool) Has(txID ids.ID) bool {
	return g.mempool.Has(ethcommon.Hash(txID))
}

func (g *GossipEthTxPool) Iterate(f func(tx *GossipEthTx) bool) {
	g.mempool.IteratePending(func(tx *types.Transaction) bool {
		return f(&GossipEthTx{Tx: tx})
	})
}

func (g *GossipEthTxPool) GetFilter() ([]byte, []byte) {
	g.lock.RLock()
	defer g.lock.RUnlock()

	return g.bloom.Marshal()
}

type GossipEthTxMarshaller struct{}

func (GossipEthTxMarshaller) MarshalGossip(tx *GossipEthTx) ([]byte, error) {
	return tx.Tx.MarshalBinary()
}

func (GossipEthTxMarshaller) UnmarshalGossip(bytes []byte) (*GossipEthTx, error) {
	tx := &GossipEthTx{
		Tx: &types.Transaction{},
	}

	return tx, tx.Tx.UnmarshalBinary(bytes)
}

type GossipEthTx struct {
	Tx *types.Transaction
}

func (tx *GossipEthTx) GossipID() ids.ID {
	return ids.ID(tx.Tx.Hash())
}

// EthPushGossiper is used by the ETH backend to push transactions issued over
// the RPC and added to the mempool to peers.
type EthPushGossiper struct {
	vm *VM
}

func (e *EthPushGossiper) Add(tx *types.Transaction) {
	// eth.Backend is initialized before the [ethTxPushGossiper] is created, so
	// we just ignore any gossip requests until it is set.
	ethTxPushGossiper := e.vm.ethTxPushGossiper.Get()
	if ethTxPushGossiper == nil {
		return
	}
	ethTxPushGossiper.Add(&GossipEthTx{tx})
}
