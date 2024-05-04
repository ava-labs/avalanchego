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
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/mempool"
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
) ([]byte, error) {
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
	mempool mempool.Mempool,
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
	mempool.Mempool
	log        logging.Logger
	txVerifier TxVerifier

	lock  sync.RWMutex
	bloom *gossip.BloomFilter
}

func (g *gossipMempool) Add(txns ...*txs.Tx) []error {
	errs := make([]error, len(txns))
	hasErr := false
	for i, tx := range txns {
		txID := tx.ID()
		if _, ok := g.Mempool.Get(txID); ok {
			errs[i] = fmt.Errorf("tx %s dropped: %w", txID, mempool.ErrDuplicateTx)
			hasErr = true
			continue
		}

		if errs[i] = g.Mempool.GetDropReason(txID); errs[i] != nil {
			// If the tx is being dropped - just ignore it
			//
			// TODO: Should we allow re-verification of the transaction even if it
			// failed previously?
			hasErr = true
			continue
		}

		if errs[i] = g.txVerifier.VerifyTx(tx); errs[i] != nil {
			g.Mempool.MarkDropped(txID, errs[i])
			hasErr = true
			continue
		}

		if errs[i] = g.Mempool.Add(tx); errs[i] != nil {
			g.Mempool.MarkDropped(txID, errs[i])
			hasErr = true
			continue
		}

		g.lock.Lock()

		g.bloom.Add(tx)
		var reset bool
		reset, errs[i] = gossip.ResetBloomFilterIfNeeded(g.bloom, g.Mempool.Len()*bloomChurnMultiplier)
		if errs[i] != nil {
			hasErr = true
			continue
		}

		if reset {
			g.log.Debug("resetting bloom filter")
			g.Mempool.Iterate(func(tx *txs.Tx) bool {
				g.bloom.Add(tx)
				return true
			})
		}

		g.lock.Unlock()
	}
	if !hasErr {
		g.Mempool.RequestBuildBlock(false)
	}

	return errs
}

func (g *gossipMempool) Has(txID ids.ID) bool {
	_, ok := g.Mempool.Get(txID)
	return ok
}

func (g *gossipMempool) GetFilter() (bloom []byte, salt []byte) {
	g.lock.RLock()
	defer g.lock.RUnlock()

	return g.bloom.Marshal()
}
