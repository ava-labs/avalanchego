// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

// Package txgossip provides a mempool for [Streaming Asynchronous Execution],
// which is also compatible with AvalancheGo's [gossip] mechanism.
//
// [Streaming Asynchronous Execution]: https://github.com/avalanche-foundation/ACPs/tree/main/ACPs/194-continuous-execution
package txgossip

import (
	"errors"
	"fmt"
	"math/bits"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/txpool"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/rlp"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p/gossip"
	"github.com/ava-labs/avalanchego/vms/saevm/saexec"
	"github.com/ava-labs/avalanchego/vms/saevm/worstcase"

	saeparams "github.com/ava-labs/avalanchego/vms/saevm/params"
)

// errInsufficientGasPerByte is returned for a transaction that carries fewer
// than x/M gas per serialized byte. See [eligible].
var errInsufficientGasPerByte = errors.New("insufficient gas per serialized byte")

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
//
// Transactions are vetted against the gas limit of the next block, derived
// from exec's last-executed block; see [eligible].
func NewSet(
	exec *saexec.Executor,
	pool *txpool.TxPool,
	config gossip.BloomSetConfig,
) (*Set, error) {
	s := &txSet{
		exec: exec,
		pool: pool,
	}
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
	exec *saexec.Executor
	pool *txpool.TxPool
}

// blockGasLimit returns the worst-case gas limit of the next block, based on
// the gas clock of the last-executed block.
func (s *txSet) blockGasLimit() uint64 {
	return uint64(worstcase.SafeMaxBlockSize(s.exec.LastExecuted().ExecutedByGasTime()))
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

// addToPool adds the eligible transactions (see [eligible]) to the pool. A
// transaction's entry is the pool's result for it, while a rejected
// transaction's entry is [errInsufficientGasPerByte] and it never reaches the pool
func (s *txSet) addToPool(local bool, txs ...*types.Transaction) []error {
	blockGasLimit := s.blockGasLimit()

	errs := make([]error, len(txs))
	eligibleTxs := make([]*types.Transaction, 0, len(txs))
	eligibleIdx := make([]int, 0, len(txs))
	for i, tx := range txs {
		if eligible(tx, blockGasLimit, saeparams.MaxBlockTxBytes) {
			eligibleTxs = append(eligibleTxs, tx)
			eligibleIdx = append(eligibleIdx, i)
			continue
		}
		errs[i] = fmt.Errorf("%w: %d gas over %d serialized bytes for a block gas limit of %d",
			errInsufficientGasPerByte, tx.Gas(), tx.Size(), blockGasLimit)
	}

	if len(eligibleTxs) == 0 {
		return errs
	}
	for j, err := range s.pool.Add(eligibleTxs, local, false /*sync*/) {
		errs[eligibleIdx[j]] = err
	}
	return errs
}

// eligible reports whether tx MAY be included in a block, using the notation:
//
//   - M = maxBytes, the block's transaction byte budget (typically
//     [saeparams.MaxBlockTxBytes])
//   - x = blockGasLimit, the block's gas limit
//   - g = tx.Gas(), the transaction's gas limit
//   - y = tx.Size(), the transaction's serialized size in bytes
//
// The transaction's byte share is y/M and its gas share is g/x. The rule rejects
// the transaction if its byte share exceeds its gas share:
//
//	accept if  y/M <= g/x  <->   y·x <= g·M
//
// Equivalently, it must carry at least x/M gas per serialized byte, which
// bounds the cumulative size of a block's worth of transactions by M.
func eligible(tx *types.Transaction, blockGasLimit, maxBytes uint64) bool {
	// Defensive check: if blockGasLimit == 0, all transactions would be
	// incorrectly eligible
	if blockGasLimit == 0 {
		return false
	}

	yxHi, yxLo := bits.Mul64(tx.Size(), blockGasLimit) // y·x
	gmHi, gmLo := bits.Mul64(tx.Gas(), maxBytes)       // g·M
	if yxHi != gmHi {
		return yxHi < gmHi
	}
	return yxLo <= gmLo
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
