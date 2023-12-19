// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package evm

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ethereum/go-ethereum/log"

	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/network/p2p/gossip"

	"github.com/ava-labs/coreth/core"
	"github.com/ava-labs/coreth/core/txpool"
	"github.com/ava-labs/coreth/core/types"
)

var (
	_ p2p.Handler = (*txGossipHandler)(nil)

	_ gossip.Gossipable        = (*GossipEthTx)(nil)
	_ gossip.Gossipable        = (*GossipAtomicTx)(nil)
	_ gossip.Set[*GossipEthTx] = (*GossipEthTxPool)(nil)
)

func newTxGossipHandler[T any, U gossip.GossipableAny[T]](
	log logging.Logger,
	mempool gossip.Set[U],
	metrics gossip.Metrics,
	maxMessageSize int,
	throttlingPeriod time.Duration,
	throttlingLimit int,
	validators *p2p.Validators,
) txGossipHandler {
	// push gossip messages can be handled from any peer
	handler := gossip.NewHandler[T, U](
		log,
		// Don't forward gossip to avoid double-forwarding
		gossip.NoOpAccumulator[U]{},
		mempool,
		metrics,
		maxMessageSize,
	)

	// pull gossip requests are filtered by validators and are throttled
	// to prevent spamming
	validatorHandler := p2p.NewValidatorHandler(
		p2p.NewThrottlerHandler(
			handler,
			p2p.NewSlidingWindowThrottler(throttlingPeriod, throttlingLimit),
			log,
		),
		validators,
		log,
	)

	return txGossipHandler{
		appGossipHandler:  handler,
		appRequestHandler: validatorHandler,
	}
}

type txGossipHandler struct {
	p2p.NoOpHandler
	appGossipHandler  p2p.Handler
	appRequestHandler p2p.Handler
}

func (t txGossipHandler) AppGossip(ctx context.Context, nodeID ids.NodeID, gossipBytes []byte) {
	t.appGossipHandler.AppGossip(ctx, nodeID, gossipBytes)
}

func (t txGossipHandler) AppRequest(ctx context.Context, nodeID ids.NodeID, deadline time.Time, requestBytes []byte) ([]byte, error) {
	return t.appRequestHandler.AppRequest(ctx, nodeID, deadline, requestBytes)
}

type GossipAtomicTx struct {
	Tx *Tx
}

func (tx *GossipAtomicTx) GetID() ids.ID {
	return tx.Tx.ID()
}

func (tx *GossipAtomicTx) Marshal() ([]byte, error) {
	return tx.Tx.SignedBytes(), nil
}

func (tx *GossipAtomicTx) Unmarshal(bytes []byte) error {
	atomicTx, err := ExtractAtomicTx(bytes, Codec)
	tx.Tx = atomicTx

	return err
}

func NewGossipEthTxPool(mempool *txpool.TxPool) (*GossipEthTxPool, error) {
	bloom, err := gossip.NewBloomFilter(txGossipBloomMaxItems, txGossipBloomFalsePositiveRate)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize bloom filter: %w", err)
	}

	return &GossipEthTxPool{
		mempool:    mempool,
		pendingTxs: make(chan core.NewTxsEvent),
		bloom:      bloom,
	}, nil
}

type GossipEthTxPool struct {
	mempool    *txpool.TxPool
	pendingTxs chan core.NewTxsEvent

	bloom *gossip.BloomFilter
	lock  sync.RWMutex
}

func (g *GossipEthTxPool) Subscribe(ctx context.Context) {
	g.mempool.SubscribeNewTxsEvent(g.pendingTxs)

	for {
		select {
		case <-ctx.Done():
			log.Debug("shutting down subscription")
			return
		case pendingTxs := <-g.pendingTxs:
			g.lock.Lock()
			for _, pendingTx := range pendingTxs.Txs {
				tx := &GossipEthTx{Tx: pendingTx}
				g.bloom.Add(tx)
				reset, err := gossip.ResetBloomFilterIfNeeded(g.bloom, txGossipMaxFalsePositiveRate)
				if err != nil {
					log.Error("failed to reset bloom filter", "err", err)
					continue
				}

				if reset {
					log.Debug("resetting bloom filter", "reason", "reached max filled ratio")

					g.mempool.IteratePending(func(tx *types.Transaction) bool {
						g.bloom.Add(&GossipEthTx{Tx: pendingTx})
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
	return g.mempool.AddRemotes([]*types.Transaction{tx.Tx})[0]
}

func (g *GossipEthTxPool) Iterate(f func(tx *GossipEthTx) bool) {
	g.mempool.IteratePending(func(tx *types.Transaction) bool {
		return f(&GossipEthTx{Tx: tx})
	})
}

func (g *GossipEthTxPool) GetFilter() ([]byte, []byte, error) {
	g.lock.RLock()
	defer g.lock.RUnlock()

	bloom, err := g.bloom.Bloom.MarshalBinary()
	salt := g.bloom.Salt

	return bloom, salt[:], err
}

type GossipEthTx struct {
	Tx *types.Transaction
}

func (tx *GossipEthTx) GetID() ids.ID {
	return ids.ID(tx.Tx.Hash())
}

func (tx *GossipEthTx) Marshal() ([]byte, error) {
	return tx.Tx.MarshalBinary()
}

func (tx *GossipEthTx) Unmarshal(bytes []byte) error {
	tx.Tx = &types.Transaction{}
	return tx.Tx.UnmarshalBinary(bytes)
}
