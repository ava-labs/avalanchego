// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package mempool

import (
	"errors"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
	txmempool "github.com/ava-labs/avalanchego/vms/txs/mempool"
)

var (
	_ Mempool = (*mempool)(nil)

	ErrDuplicateTx                = txmempool.ErrDuplicateTx
	ErrCantIssueAdvanceTimeTx     = errors.New("can not issue an advance time tx")
	ErrCantIssueRewardValidatorTx = errors.New("can not issue a reward validator tx")
)

type Mempool interface {
	Add(tx *txs.Tx) error
	Get(txID ids.ID) (*txs.Tx, bool)
	// Remove [txs] and any conflicts of [txs] from the mempool.
	Remove(txs ...*txs.Tx)

	// Peek returns the oldest tx in the mempool.
	Peek() (tx *txs.Tx, exists bool)

	// Iterate iterates over the txs until f returns false
	Iterate(f func(tx *txs.Tx) bool)

	// RequestBuildBlock notifies the consensus engine that a block should be
	// built. If [emptyBlockPermitted] is true, the notification will be sent
	// regardless of whether there are no transactions in the mempool. If not,
	// a notification will only be sent if there is at least one transaction in
	// the mempool.
	RequestBuildBlock(emptyBlockPermitted bool)

	// Note: dropped txs are added to droppedTxIDs but are not evicted from
	// unissued decision/staker txs. This allows previously dropped txs to be
	// possibly reissued.
	MarkDropped(txID ids.ID, reason error)
	GetDropReason(txID ids.ID) error

	// Len returns the number of txs in the mempool.
	Len() int
}

type mempool struct {
	*txmempool.Mempool[*txs.Tx]
}

func New(
	namespace string,
	registerer prometheus.Registerer,
	toEngine chan<- common.Message,
) (Mempool, error) {
	m, err := txmempool.New[*txs.Tx](
		namespace,
		registerer,
		toEngine,
		"txs",
		"Number of decision/staker transactions in the mempool",
	)
	return &mempool{m}, err
}

func (m *mempool) Add(tx *txs.Tx) error {
	switch tx.Unsigned.(type) {
	case *txs.AdvanceTimeTx:
		return ErrCantIssueAdvanceTimeTx
	case *txs.RewardValidatorTx:
		return ErrCantIssueRewardValidatorTx
	default:
	}

	return m.Mempool.Add(tx)
}
