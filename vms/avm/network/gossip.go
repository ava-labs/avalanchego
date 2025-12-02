// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"context"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/network/p2p/gossip"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/avm/txs"
	"github.com/ava-labs/avalanchego/vms/txs/mempool"
)

var (
	_ p2p.Handler                = (*txGossipHandler)(nil)
	_ gossip.Set[*txs.Tx]        = (*gossipMempool)(nil)
	_ gossip.Marshaller[*txs.Tx] = (*txParser)(nil)
)

// bloomChurnMultiplier is the number used to multiply the size of the mempool
// to determine how large of a bloom filter to create.
const bloomChurnMultiplier = 3

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
) ([]byte, *common.AppError) {
	return t.appRequestHandler.AppRequest(ctx, nodeID, deadline, requestBytes)
}

type txParser struct {
	parser txs.Parser
}

func (*txParser) MarshalGossip(tx *txs.Tx) ([]byte, error) {
	return tx.Bytes(), nil
}

func (g *txParser) UnmarshalGossip(bytes []byte) (*txs.Tx, error) {
	return g.parser.ParseTx(bytes)
}

func newGossipMempool(
	mempool mempool.Mempool[*txs.Tx],
	registerer prometheus.Registerer,
	log logging.Logger,
	txVerifier TxVerifier,
	minTargetElements int,
	targetFalsePositiveProbability,
	resetFalsePositiveProbability float64,
) (*gossipMempool, error) {
	bloom, err := gossip.NewBloomFilter(registerer, "mempool_bloom_filter", minTargetElements, targetFalsePositiveProbability, resetFalsePositiveProbability)
	return &gossipMempool{
		Mempool:    mempool,
		log:        log,
		txVerifier: txVerifier,
		bloom:      bloom,
	}, err
}

type gossipMempool struct {
	mempool.Mempool[*txs.Tx]
	log        logging.Logger
	txVerifier TxVerifier

	lock  sync.RWMutex
	bloom *gossip.BloomFilter
}

// Add is called by the p2p SDK when handling transactions that sent pushed to
// us and when handling transactions that were pulled from a peer.
func (g *gossipMempool) Add(tx *txs.Tx) error {
	txID := tx.ID()
	if _, ok := g.Mempool.Get(txID); ok {
		return nil
	}

	if reason := g.Mempool.GetDropReason(txID); reason != nil {
		// If the tx is being dropped - just ignore it
		//
		// TODO: Should we allow re-verification of the transaction even if it
		// failed previously?
		return reason
	}

	// Verify the tx at the currently preferred state
	if err := g.txVerifier.VerifyTx(tx); err != nil {
		g.Mempool.MarkDropped(txID, err)
		return err
	}

	return g.AddWithoutVerification(tx)
}

func (g *gossipMempool) Has(txID ids.ID) bool {
	_, ok := g.Mempool.Get(txID)
	return ok
}

func (g *gossipMempool) AddWithoutVerification(tx *txs.Tx) error {
	if err := g.Mempool.Add(tx); err != nil {
		g.Mempool.MarkDropped(tx.ID(), err)
		return err
	}

	g.lock.Lock()
	defer g.lock.Unlock()

	g.bloom.Add(tx)
	reset, err := gossip.ResetBloomFilterIfNeeded(g.bloom, g.Mempool.Len()*bloomChurnMultiplier)
	if err != nil {
		return err
	}

	if reset {
		g.log.Debug("resetting bloom filter")
		g.Mempool.Iterate(func(tx *txs.Tx) bool {
			g.bloom.Add(tx)
			return true
		})
	}
	return nil
}

func (g *gossipMempool) Iterate(f func(*txs.Tx) bool) {
	g.Mempool.Iterate(f)
}

func (g *gossipMempool) GetFilter() (bloom []byte, salt []byte) {
	g.lock.RLock()
	defer g.lock.RUnlock()

	return g.bloom.Marshal()
}
