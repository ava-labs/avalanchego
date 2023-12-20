// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/log"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/network/p2p/gossip"
	"github.com/ava-labs/avalanchego/vms/avm/txs"
	"github.com/ava-labs/avalanchego/vms/avm/txs/mempool"
)

var (
	_ p2p.Handler                  = (*txGossipHandler)(nil)
	_ gossip.Set[*gossipTx]        = (*gossipMempool)(nil)
	_ gossip.Gossipable            = (*gossipTx)(nil)
	_ gossip.Marshaller[*gossipTx] = (*gossipTxParser)(nil)
)

// txGossipHandler is the handler called when serving gossip messages
type txGossipHandler struct {
	p2p.NoOpHandler
	appGossipHandler  p2p.Handler
	appRequestHandler p2p.Handler
}

func (t txGossipHandler) AppGossip(
	ctx context.Context,
	nodeID ids.NodeID,
	gossipBytes []byte,
) {
	t.appGossipHandler.AppGossip(ctx, nodeID, gossipBytes)
}

func (t txGossipHandler) AppRequest(
	ctx context.Context,
	nodeID ids.NodeID,
	deadline time.Time,
	requestBytes []byte,
) ([]byte, error) {
	return t.appRequestHandler.AppRequest(ctx, nodeID, deadline, requestBytes)
}

type gossipTx struct {
	tx *txs.Tx
}

func (g *gossipTx) GossipID() ids.ID {
	return g.tx.ID()
}

type gossipTxParser struct {
	parser txs.Parser
}

func (*gossipTxParser) MarshalGossip(tx *gossipTx) ([]byte, error) {
	return tx.tx.Bytes(), nil
}

func (g *gossipTxParser) UnmarshalGossip(bytes []byte) (*gossipTx, error) {
	tx, err := g.parser.ParseTx(bytes)
	return &gossipTx{
		tx: tx,
	}, err
}

func newGossipMempool(
	mempool mempool.Mempool,
	txVerifier TxVerifier,
	parser txs.Parser,
	maxExpectedElements uint64,
	falsePositiveProbability,
	maxFalsePositiveProbability float64,
) (*gossipMempool, error) {
	bloom, err := gossip.NewBloomFilter(maxExpectedElements, falsePositiveProbability)
	return &gossipMempool{
		Mempool:                     mempool,
		txVerifier:                  txVerifier,
		parser:                      parser,
		maxFalsePositiveProbability: maxFalsePositiveProbability,
		bloom:                       bloom,
	}, err
}

type gossipMempool struct {
	mempool.Mempool
	txVerifier                  TxVerifier
	parser                      txs.Parser
	maxFalsePositiveProbability float64

	lock  sync.RWMutex
	bloom *gossip.BloomFilter
}

func (g *gossipMempool) Add(tx *gossipTx) error {
	txID := tx.tx.ID()
	if _, ok := g.Mempool.Get(txID); ok {
		// The tx is already in the mempool
		return fmt.Errorf("tx %s already known", txID)
	}

	if reason := g.Mempool.GetDropReason(txID); reason != nil {
		// If the tx is being dropped - just ignore it
		//
		// TODO: Should we allow re-verification of the transaction even if it
		// failed previously?
		return reason
	}

	// Verify the tx at the currently preferred state
	if err := g.txVerifier.VerifyTx(tx.tx); err != nil {
		g.Mempool.MarkDropped(txID, err)
		return err
	}

	return g.AddVerifiedTx(tx)
}

func (g *gossipMempool) AddVerifiedTx(tx *gossipTx) error {
	if err := g.Mempool.Add(tx.tx); err != nil {
		g.Mempool.MarkDropped(tx.tx.ID(), err)
		return err
	}

	g.lock.Lock()
	defer g.lock.Unlock()

	g.bloom.Add(tx)
	reset, err := gossip.ResetBloomFilterIfNeeded(g.bloom, g.maxFalsePositiveProbability)
	if err != nil {
		return err
	}

	if reset {
		log.Debug("resetting bloom filter", "reason", "reached max filled ratio")

		g.Mempool.Iterate(func(tx *txs.Tx) bool {
			g.bloom.Add(&gossipTx{
				tx: tx,
			})
			return true
		})
	}

	g.Mempool.RequestBuildBlock()
	return nil
}

func (g *gossipMempool) Iterate(f func(*gossipTx) bool) {
	g.Mempool.Iterate(func(tx *txs.Tx) bool {
		return f(&gossipTx{
			tx: tx,
		})
	})
}

func (g *gossipMempool) GetFilter() (bloom []byte, salt []byte, err error) {
	g.lock.RLock()
	defer g.lock.RUnlock()

	bloomBytes, err := g.bloom.Bloom.MarshalBinary()
	return bloomBytes, g.bloom.Salt[:], err
}
