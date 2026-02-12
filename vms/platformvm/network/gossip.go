// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"fmt"
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p/gossip"
	"github.com/ava-labs/avalanchego/utils/bloom"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/mempool"

	txmempool "github.com/ava-labs/avalanchego/vms/txs/mempool"
)

var (
	_ gossip.Marshaller[*txs.Tx] = (*txMarshaller)(nil)
	_ gossip.Gossipable          = (*txs.Tx)(nil)
)

// bloomChurnMultiplier is the number used to multiply the size of the mempool
// to determine how large of a bloom filter to create.
const bloomChurnMultiplier = 3

type txMarshaller struct{}

func (txMarshaller) MarshalGossip(tx *txs.Tx) ([]byte, error) {
	return tx.Bytes(), nil
}

func (txMarshaller) UnmarshalGossip(bytes []byte) (*txs.Tx, error) {
	return txs.Parse(txs.Codec, bytes)
}

func newGossipMempool(
	mempool *mempool.Mempool,
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
	*mempool.Mempool

	log        logging.Logger
	txVerifier TxVerifier

	lock  sync.RWMutex
	bloom *gossip.BloomFilter
}

func (g *gossipMempool) Add(tx *txs.Tx) error {
	txID := tx.ID()
	if _, ok := g.Mempool.Get(txID); ok {
		return fmt.Errorf("tx %s dropped: %w", txID, txmempool.ErrDuplicateTx)
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

	g.lock.Lock()
	defer g.lock.Unlock()

	g.bloom.Add(tx)
	reset, err := gossip.ResetBloomFilterIfNeeded(
		g.bloom,
		g.Mempool.Len()*bloomChurnMultiplier,
	)
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

func (g *gossipMempool) Has(txID ids.ID) bool {
	_, ok := g.Mempool.Get(txID)
	return ok
}

func (g *gossipMempool) BloomFilter() (*bloom.Filter, ids.ID) {
	g.lock.RLock()
	defer g.lock.RUnlock()

	return g.bloom.BloomFilter()
}
