// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"fmt"
	"sync"

	"github.com/ava-labs/avalanchego/network/p2p/gossip"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs/mempool"
)

func newGossipMempool(
	mempool mempool.Mempool,
	log logging.Logger,
	verifier TxVerifier,
	maxExpectedElements uint64,
	falsePositiveProbability float64,
	maxFalsePositiveProbability float64,
) (*gossipMempool, error) {
	bloomFilter, err := gossip.NewBloomFilter(
		maxExpectedElements,
		falsePositiveProbability,
	)
	if err != nil {
		return nil, err
	}

	return &gossipMempool{
		Mempool:                     mempool,
		log:                         log,
		verifier:                    verifier,
		bloom:                       bloomFilter,
		maxFalsePositiveProbability: maxFalsePositiveProbability,
	}, nil
}

type gossipMempool struct {
	mempool.Mempool
	log                         logging.Logger
	verifier                    TxVerifier
	maxFalsePositiveProbability float64

	lock  sync.RWMutex
	bloom *gossip.BloomFilter
}

func (g *gossipMempool) Add(tx *txs.Tx) error {
	txID := tx.ID()
	if _, ok := g.Mempool.Get(txID); ok {
		return fmt.Errorf("tx %s dropped: %w", tx.ID(), mempool.ErrDuplicateTx)
	}

	if reason := g.Mempool.GetDropReason(txID); reason != nil {
		// If the tx is being dropped - just ignore it
		//
		// TODO: Should we allow re-verification of the transaction even if it
		// failed previously?
		return reason
	}

	if err := g.verifier.VerifyTx(tx); err != nil {
		g.Mempool.MarkDropped(txID, err)
		return err
	}

	if err := g.Mempool.Add(tx); err != nil {
		g.Mempool.MarkDropped(txID, err)
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
		g.log.Debug("resetting bloom filter")
		g.Mempool.Iterate(func(tx *txs.Tx) bool {
			g.bloom.Add(tx)
			return true
		})
	}

	return err
}

func (g *gossipMempool) GetFilter() (bloom []byte, salt []byte, err error) {
	g.lock.RLock()
	defer g.lock.RUnlock()

	bloomBytes, err := g.bloom.Bloom.MarshalBinary()
	return bloomBytes, g.bloom.Salt[:], err
}
