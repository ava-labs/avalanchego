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
	"math"

	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/core/txpool"
	"github.com/ava-labs/libevm/core/types"
	"github.com/ava-labs/libevm/rlp"
	"github.com/holiman/uint256"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/network/p2p/gossip"
	"github.com/ava-labs/avalanchego/vms/saevm/saexec"
	"github.com/ava-labs/avalanchego/vms/saevm/worstcase"

	saeparams "github.com/ava-labs/avalanchego/vms/saevm/params"
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
//
// Transactions are vetted against the gas limit of the next block, derived
// from exec's last-executed block; see [minGasForSize].
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
	return uint64(worstcase.SafeMaxBlockSize(s.exec.LastExecuted().ExecutedByGasTime().Rate()))
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

// errInsufficientGasPerByte is returned for a transaction that carries fewer
// than x/M gas per serialized byte. See [minGasForSize].
var errInsufficientGasPerByte = errors.New("insufficient gas limit for tx size")

// addToPool adds the eligible transactions (see [minGasForSize]) to the pool.
func (s *txSet) addToPool(local bool, txs ...*types.Transaction) []error {
	blockGasLimit := s.blockGasLimit()

	errs := make([]error, len(txs))
	eligibleTxs := make([]*types.Transaction, 0, len(txs))
	eligibleIdx := make([]int, 0, len(txs))
	for i, tx := range txs {
		minGas := minGasForSize(tx.Size(), blockGasLimit)
		if tx.Gas() >= minGas {
			eligibleTxs = append(eligibleTxs, tx)
			eligibleIdx = append(eligibleIdx, i)
			continue
		}
		errs[i] = fmt.Errorf("%w: tx size %d bytes must specify gas limit at least %d when the block gas limit is %d",
			errInsufficientGasPerByte, tx.Size(), minGas, blockGasLimit)
	}

	if len(eligibleTxs) == 0 {
		return errs
	}
	for j, err := range s.pool.Add(eligibleTxs, local, false /*sync*/) {
		errs[eligibleIdx[j]] = err
	}
	return errs
}

// minGasForSize returns the minimum gas limit a transaction of size bytes must
// carry to be included in a block, using the notation:
//
//   - M = [saeparams.TargetBlockBytes], the block's transaction byte budget
//   - x = blockGasLimit, the block's gas limit
//   - y = size, the transaction's serialized size in bytes
//   - g = the transaction's gas limit, the value this function bounds
//
// A transaction's byte share is y/M and its gas share is g/x. It is eligible
// only if its byte share does not exceed its gas share (y/M <= g/x, i.e.
// y·x <= g·M), which holds exactly when g >= ceil(y·x / M). Requiring at least
// x/M gas per byte bounds the cumulative size of a block's worth of
// transactions by M.
func minGasForSize(size, blockGasLimit uint64) uint64 {
	// Defensive check: with a zero gas limit no transaction fits, but the ceil
	// division below would return 0 and make every transaction eligible.
	if blockGasLimit == 0 {
		return math.MaxUint64
	}

	var (
		minGas uint256.Int
		temp   uint256.Int
	)
	minGas.SetUint64(size) // y
	temp.SetUint64(blockGasLimit)
	minGas.Mul(&minGas, &temp)                              // y·x
	minGas.AddUint64(&minGas, saeparams.TargetBlockBytes-1) // round up
	temp.SetUint64(saeparams.TargetBlockBytes)
	minGas.Div(&minGas, &temp) // ceil(y·x / M)
	// No uint64 gas limit can satisfy the rule, no transaction is eligible.
	if !minGas.IsUint64() {
		return math.MaxUint64
	}
	return minGas.Uint64()
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
