// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p"
	"github.com/ava-labs/avalanchego/network/p2p/gossip"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"

	pmempool "github.com/ava-labs/avalanchego/vms/platformvm/txs/mempool"
)

var (
	_ p2p.Handler                = (*txGossipHandler)(nil)
	_ gossip.Marshaller[*txs.Tx] = (*txMarshaller)(nil)
	_ gossip.Gossipable          = (*txs.Tx)(nil)
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

type txMarshaller struct{}

func (txMarshaller) MarshalGossip(tx *txs.Tx) ([]byte, error) {
	return tx.Bytes(), nil
}

func (txMarshaller) UnmarshalGossip(bytes []byte) (*txs.Tx, error) {
	return txs.Parse(txs.Codec, bytes)
}

func newGossipMempool(
	mempool pmempool.Mempool,
	registerer prometheus.Registerer,
	log logging.Logger,
	txVerifier TxVerifier,
	minTargetElements int,
	targetFalsePositiveProbability,
	resetFalsePositiveProbability float64,
	gossipMetrics *gossip.Metrics,
) (*gossipMempool, error) {
	bloom, err := gossip.NewBloomFilter(registerer, "mempool_bloom_filter", minTargetElements, targetFalsePositiveProbability, resetFalsePositiveProbability)
	if err != nil {
		return nil, err
	}
	gMempool, err := gossip.NewMempool[*txs.Tx](gossipMetrics)
	if err != nil {
		return nil, err
	}
	return &gossipMempool{
		Mempool:    mempool,
		log:        log,
		txVerifier: txVerifier,
		bloom:      bloom,
		mempool:    gMempool,
	}, nil
}

type gossipMempool struct {
	pmempool.Mempool
	log        logging.Logger
	txVerifier TxVerifier

	lock    sync.RWMutex
	bloom   *gossip.BloomFilter
	mempool *gossip.Mempool[*txs.Tx]
}

func (g *gossipMempool) Add(tx *txs.Tx) error {
	txID := tx.ID()

	g.lock.Lock()
	defer g.lock.Unlock()
	if g.mempool.Has(txID) {
		// adding an entry would fail, and an error would be returned.
		return g.mempool.Add(tx)
	}

	if reason := g.Mempool.GetDropReason(txID); reason != nil {
		// If the tx is being dropped - just ignore it
		//
		// TODO: Should we allow re-verification of the transaction even if it
		// failed previously?
		return reason
	}

	if err := g.txVerifier.VerifyTx(tx); err != nil {
		g.log.Debug("transaction failed verification",
			zap.Stringer("txID", txID),
			zap.Error(err),
		)

		g.Mempool.MarkDropped(txID, err)
		return fmt.Errorf("failed verification: %w", err)
	}

	if err := g.Mempool.Add(tx); err != nil {
		g.Mempool.MarkDropped(txID, err)
		return err
	}

	if err := g.mempool.Add(tx); err != nil {
		return err
	}

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

	g.Mempool.RequestBuildBlock(false)
	return nil
}

func (g *gossipMempool) Has(txID ids.ID) bool {
	g.lock.RLock()
	defer g.lock.RUnlock()

	return g.mempool.Has(txID)
}

func (g *gossipMempool) GetFilter() (bloom []byte, salt []byte) {
	g.lock.RLock()
	defer g.lock.RUnlock()

	return g.bloom.Marshal()
}

func (g *gossipMempool) Remove(removeTxs ...*txs.Tx) {
	g.lock.Lock()
	defer g.lock.Unlock()

	g.Mempool.Remove(removeTxs...)
	g.mempool.Remove(removeTxs...)
}
