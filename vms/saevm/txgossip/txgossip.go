// Copyright (C) 2025-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package txgossip provides a mempool for [Streaming Asynchronous Execution],
// which is also compatible with AvalancheGo's [gossip] mechanism.
//
// [Streaming Asynchronous Execution]: https://github.com/avalanche-foundation/ACPs/tree/main/ACPs/194-streaming-asynchronous-execution
package txgossip

import (
	"errors"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p/gossip"
	"github.com/ava-labs/avalanchego/utils/logging"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/txpool"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/rlp"
)

var _ gossip.Gossipable = Transaction{}

// A Transaction is a [gossip.Gossipable] wrapper for a [types.Transaction].
type Transaction struct {
	*types.Transaction
}

// GossipID returns the transaction hash.
func (tx Transaction) GossipID() ids.ID {
	return ids.ID(tx.Hash())
}

var _ gossip.Marshaller[Transaction] = Marshaller{}

// A Marshaller implements [gossip.Marshaller] for [Transaction], based on RLP
// encoding.
type Marshaller struct{}

// MarshalGossip returns the [rlp] encoding of the underlying
// [types.Transaction].
func (Marshaller) MarshalGossip(tx Transaction) ([]byte, error) {
	return rlp.EncodeToBytes(tx.Transaction)
}

// UnmarshalGossip [rlp] decodes the buffer into a [types.Transaction].
func (Marshaller) UnmarshalGossip(buf []byte) (Transaction, error) {
	tx := Transaction{new(types.Transaction)}
	if err := rlp.DecodeBytes(buf, tx.Transaction); err != nil {
		return Transaction{}, err
	}
	return tx, nil
}

// Set couples a [gossip.BloomSet] with a [txpool.TxPool] that acts as the
// backing for the set.
type Set struct {
	*gossip.BloomSet[Transaction]
	Pool *txpool.TxPool

	set    *txSet
	pushTo []func(...Transaction)
}

// NewSet returns a new Set. Use [gossip.BloomSet.Add] or [Set.SendTx] to add
// transactions to the pool, which SHOULD NOT be populated directly.
func NewSet(logger logging.Logger, pool *txpool.TxPool, config gossip.BloomSetConfig) (*Set, error) {
	s := &txSet{pool}
	bs, err := gossip.NewBloomSet(s, config)
	if err != nil {
		return nil, err
	}
	return &Set{
		BloomSet: bs,
		Pool:     pool,
		set:      s,
	}, nil
}

var _ gossip.Set[Transaction] = (*txSet)(nil)

type txSet struct {
	pool *txpool.TxPool
}

func (s *txSet) Add(tx Transaction) error {
	errs := s.addToPool(false, tx.Transaction)
	for i, err := range errs {
		if errors.Is(err, txpool.ErrAlreadyKnown) {
			errs[i] = nil
		}
	}
	return errors.Join(errs...)
}

func (s *txSet) addToPool(local bool, txs ...*types.Transaction) []error {
	return s.pool.Add(txs, local, false /*sync*/)
}

func (s *txSet) Has(id ids.ID) bool {
	return s.pool.Has(common.Hash(id))
}

func (s *txSet) Iterate(fn func(Transaction) bool) {
	// TODO(arr4n) implement a method on libevm's [txpool.TxPool] that returns
	// a more efficient iterator.
	pending, queued := s.pool.Content()
	for _, group := range []map[common.Address][]*types.Transaction{pending, queued} {
		for _, txs := range group {
			for _, tx := range txs {
				if !fn(Transaction{tx}) {
					return
				}
			}
		}
	}
}

func (s *txSet) Len() int {
	pending, queued := s.pool.Stats()
	return pending + queued
}
