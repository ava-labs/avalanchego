// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/network/p2p/gossip"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/avm/txs"

	xmempool "github.com/ava-labs/avalanchego/vms/avm/txs/mempool"
)

var (
	_ p2p.Handler                = (*txGossipHandler)(nil)
	_ gossip.Mempool[*txs.Tx]    = (*gossipMempool)(nil)
	_ gossip.Marshaller[*txs.Tx] = (*txParser)(nil)
)

// bloomChurnMultiplier is the number used to multiply the size of the mempool
// to determine how large of a bloom filter to create.
const (
	bloomChurnMultiplier          = 3
	droppedDuplicate              = "duplicate"
	droppedFailedVerification     = "failed_verification"
	droppedFailedBloomFilterReset = "failed_reset_bloom_filter"
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
	mempool xmempool.Mempool,
	registerer prometheus.Registerer,
	log logging.Logger,
	txVerifier TxVerifier,
	parser txs.Parser,
	minTargetElements int,
	targetFalsePositiveProbability,
	resetFalsePositiveProbability float64,
	gossipMetrics gossip.Metrics,
) (*gossipMempool, error) {
	bloom, err := gossip.NewBloomFilter(registerer, "mempool_bloom_filter", minTargetElements, targetFalsePositiveProbability, resetFalsePositiveProbability)
	if err != nil {
		return nil, err
	}
	gMempool, err := gossip.NewMempool[*txs.Tx](log, gossipMetrics)
	if err != nil {
		return nil, err
	}
	return &gossipMempool{
		Mempool:    mempool,
		log:        log,
		txVerifier: txVerifier,
		parser:     parser,
		bloom:      bloom,
		mempool:    gMempool,
	}, nil
}

type gossipMempool struct {
	xmempool.Mempool
	log        logging.Logger
	txVerifier TxVerifier
	parser     txs.Parser

	bloomLock sync.RWMutex
	bloom     *gossip.BloomFilter

	gossipMempoolLock sync.RWMutex
	mempool           gossip.Mempool[*txs.Tx]
}

// Add is called by the p2p SDK when handling transactions that were pushed to
// us and when handling transactions that were pulled from a peer. If this
// returns a nil error while handling push gossip, the p2p SDK will queue the
// transaction to push gossip as well.
func (g *gossipMempool) Add(tx *txs.Tx) error {
	txID := tx.ID()

	g.gossipMempoolLock.Lock()
	if g.mempool.Has(txID) {
		defer g.gossipMempoolLock.Unlock()
		// adding an entry would fail, and an error would be returned.
		return g.mempool.Add(tx)
	}
	// we can release the lock here, as it would get re-acquired and tested in AddWithoutVerification
	g.gossipMempoolLock.Unlock()

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
		return fmt.Errorf("transaction %s verification failed: %w", txID, err)
	}

	return g.AddWithoutVerification(tx)
}

func (g *gossipMempool) Has(txID ids.ID) bool {
	g.gossipMempoolLock.Lock()
	defer g.gossipMempoolLock.Unlock()

	return g.mempool.Has(txID)
}

func (g *gossipMempool) AddWithoutVerification(tx *txs.Tx) error {
	txID := tx.ID()
	g.gossipMempoolLock.Lock()
	if g.mempool.Has(txID) {
		// adding an entry would fail, and an error would be returned.
		err := g.mempool.Add(tx)
		g.gossipMempoolLock.Unlock()
		return err
	}

	if err := g.Mempool.Add(tx); err != nil {
		g.Mempool.MarkDropped(tx.ID(), err)
		g.gossipMempoolLock.Unlock()
		return err
	}
	if err := g.mempool.Add(tx); err != nil {
		return err
	}
	g.gossipMempoolLock.Unlock()

	g.bloomLock.Lock()
	defer g.bloomLock.Unlock()

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

	g.Mempool.RequestBuildBlock()
	return nil
}

func (g *gossipMempool) Iterate(f func(*txs.Tx) bool) {
	g.Mempool.Iterate(f)
}

func (g *gossipMempool) GetFilter() (bloom []byte, salt []byte) {
	g.bloomLock.RLock()
	defer g.bloomLock.RUnlock()

	return g.bloom.Marshal()
}

func (g *gossipMempool) Remove(removeTxs ...*txs.Tx) {
	g.gossipMempoolLock.Lock()
	defer g.gossipMempoolLock.Unlock()
	beforeEntries := make(map[*txs.Tx]bool, g.Mempool.Len())
	g.Mempool.Iterate(func(tx *txs.Tx) bool {
		beforeEntries[tx] = true
		return true
	})
	g.Mempool.Remove(removeTxs...)

	// we need to syncronize with the underlying mempool to find out which of the transction were removed.
	for tx := range beforeEntries {
		if _, ok := g.Mempool.Get(tx.GossipID()); !ok {
			g.mempool.Remove(tx)
		}
	}
}
