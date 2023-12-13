// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package mempool

import (
	"errors"
	"fmt"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/cache"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/snow/engine/common"
	"github.com/ava-labs/avalanchego/utils/linkedhashmap"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"
)

const (
	// MaxTxSize is the maximum number of bytes a transaction can use to be
	// allowed into the mempool.
	MaxTxSize = 64 * units.KiB

	// droppedTxIDsCacheSize is the maximum number of dropped txIDs to cache
	droppedTxIDsCacheSize = 64

	initialConsumedUTXOsSize = 512

	// maxMempoolSize is the maximum number of bytes allowed in the mempool
	maxMempoolSize = 64 * units.MiB
)

var (
	_ Mempool = (*mempool)(nil)

	errDuplicateTx                = errors.New("duplicate tx")
	errTxTooLarge                 = errors.New("tx too large")
	errMempoolFull                = errors.New("mempool is full")
	errConflictsWithOtherTx       = errors.New("tx conflicts with other tx")
	errCantIssueAdvanceTimeTx     = errors.New("can not issue an advance time tx")
	errCantIssueRewardValidatorTx = errors.New("can not issue a reward validator tx")
)

type Mempool interface {
	Add(tx *txs.Tx) error
	Has(txID ids.ID) bool
	Get(txID ids.ID) *txs.Tx
	Remove(txs []*txs.Tx)

	// Peek returns the oldest tx in the mempool.
	Peek() (tx *txs.Tx, exists bool)

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
}

// Transactions from clients that have not yet been put into blocks and added to
// consensus
type mempool struct {
	bytesAvailableMetric prometheus.Gauge
	bytesAvailable       int

	unissuedTxs linkedhashmap.LinkedHashmap[ids.ID, *txs.Tx]
	numTxs      prometheus.Gauge

	// Key: Tx ID
	// Value: Verification error
	droppedTxIDs *cache.LRU[ids.ID, error]

	consumedUTXOs set.Set[ids.ID]

	toEngine chan<- common.Message
}

func New(
	namespace string,
	registerer prometheus.Registerer,
	toEngine chan<- common.Message,
) (Mempool, error) {
	bytesAvailableMetric := prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "bytes_available",
		Help:      "Number of bytes of space currently available in the mempool",
	})
	if err := registerer.Register(bytesAvailableMetric); err != nil {
		return nil, err
	}

	numTxs := prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "txs",
		Help:      "Number of decision/staker transactions in the mempool",
	})
	if err := registerer.Register(numTxs); err != nil {
		return nil, err
	}

	bytesAvailableMetric.Set(maxMempoolSize)
	return &mempool{
		bytesAvailableMetric: bytesAvailableMetric,
		bytesAvailable:       maxMempoolSize,

		unissuedTxs: linkedhashmap.New[ids.ID, *txs.Tx](),
		numTxs:      numTxs,

		droppedTxIDs:  &cache.LRU[ids.ID, error]{Size: droppedTxIDsCacheSize},
		consumedUTXOs: set.NewSet[ids.ID](initialConsumedUTXOsSize),
		toEngine:      toEngine,
	}, nil
}

func (m *mempool) Add(tx *txs.Tx) error {
	switch tx.Unsigned.(type) {
	case *txs.AdvanceTimeTx:
		return errCantIssueAdvanceTimeTx
	case *txs.RewardValidatorTx:
		return errCantIssueRewardValidatorTx
	default:
	}

	// Note: a previously dropped tx can be re-added
	txID := tx.ID()
	if m.Has(txID) {
		return fmt.Errorf("%w: %s", errDuplicateTx, txID)
	}

	txSize := len(tx.Bytes())
	if txSize > MaxTxSize {
		return fmt.Errorf("%w: %s size (%d) > max size (%d)",
			errTxTooLarge,
			txID,
			txSize,
			MaxTxSize,
		)
	}
	if txSize > m.bytesAvailable {
		return fmt.Errorf("%w: %s size (%d) > available space (%d)",
			errMempoolFull,
			txID,
			txSize,
			m.bytesAvailable,
		)
	}

	inputs := tx.Unsigned.InputIDs()
	if m.consumedUTXOs.Overlaps(inputs) {
		return fmt.Errorf("%w: %s", errConflictsWithOtherTx, txID)
	}

	m.unissuedTxs.Put(tx.ID(), tx)
	m.numTxs.Inc()
	m.bytesAvailable -= txSize
	m.bytesAvailableMetric.Set(float64(m.bytesAvailable))

	// Mark these UTXOs as consumed in the mempool
	m.consumedUTXOs.Union(inputs)

	// An explicitly added tx must not be marked as dropped.
	m.droppedTxIDs.Evict(txID)

	return nil
}

func (m *mempool) Has(txID ids.ID) bool {
	return m.Get(txID) != nil
}

func (m *mempool) Get(txID ids.ID) *txs.Tx {
	tx, _ := m.unissuedTxs.Get(txID)
	return tx
}

func (m *mempool) Remove(txsToRemove []*txs.Tx) {
	for _, tx := range txsToRemove {
		txID := tx.ID()
		if !m.unissuedTxs.Delete(txID) {
			continue
		}
		m.numTxs.Dec()

		m.bytesAvailable += len(tx.Bytes())
		m.bytesAvailableMetric.Set(float64(m.bytesAvailable))

		inputs := tx.Unsigned.InputIDs()
		m.consumedUTXOs.Difference(inputs)
	}
}

func (m *mempool) Peek() (*txs.Tx, bool) {
	_, tx, exists := m.unissuedTxs.Oldest()
	return tx, exists
}

func (m *mempool) MarkDropped(txID ids.ID, reason error) {
	m.droppedTxIDs.Put(txID, reason)
}

func (m *mempool) GetDropReason(txID ids.ID) error {
	err, _ := m.droppedTxIDs.Get(txID)
	return err
}

func (m *mempool) RequestBuildBlock(emptyBlockPermitted bool) {
	if !emptyBlockPermitted && m.unissuedTxs.Len() == 0 {
		return
	}

	select {
	case m.toEngine <- common.PendingTxs:
	default:
	}
}
