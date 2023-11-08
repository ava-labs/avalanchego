// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package mempool

import (
	"errors"
	"fmt"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/cache"
	"github.com/ava-labs/avalanchego/ids"
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

type BlockTimer interface {
	// ResetBlockTimer schedules a timer to notify the consensus engine once
	// there is a block ready to be built. If a block is ready to be built when
	// this function is called, the engine will be notified directly.
	ResetBlockTimer()
}

type Mempool interface {
	Add(tx *txs.Tx) error
	Has(txID ids.ID) bool
	Get(txID ids.ID) *txs.Tx
	Remove(txs []*txs.Tx)

	// Peek returns the first tx in the mempool whose size is <= [maxTxSize].
	Peek(maxTxSize int) *txs.Tx

	// Note: Dropped txs are added to droppedTxIDs but not evicted from
	// unissued. This allows previously dropped txs to be possibly reissued.
	MarkDropped(txID ids.ID, reason error)
	GetDropReason(txID ids.ID) error

	// Drops all [txs.Staker] transactions whose [StartTime] is before
	// [minStartTime]. The dropped tx ids are returned.
	//
	// TODO: Remove once [StartTime] field is ignored in staker txs
	DropExpiredStakerTxs(minStartTime time.Time) []ids.ID
}

// Transactions from clients that have not yet been put into blocks and added to
// consensus
type mempool struct {
	// If true, drop transactions added to the mempool via Add.
	dropIncoming bool

	bytesAvailableMetric prometheus.Gauge
	bytesAvailable       int

	unissuedTxs linkedhashmap.LinkedHashmap[ids.ID, *txs.Tx]
	numTxs      prometheus.Gauge

	// Key: Tx ID
	// Value: Verification error
	droppedTxIDs *cache.LRU[ids.ID, error]

	consumedUTXOs set.Set[ids.ID]

	blkTimer BlockTimer
}

func New(
	namespace string,
	registerer prometheus.Registerer,
	blkTimer BlockTimer,
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
		dropIncoming:  false, // enable tx adding by default
		blkTimer:      blkTimer,
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
		return fmt.Errorf("%w, tx %s size (%d) exceeds available space (%d)",
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
	m.bytesAvailable -= txSize
	m.bytesAvailableMetric.Set(float64(m.bytesAvailable))

	// Mark these UTXOs as consumed in the mempool
	m.consumedUTXOs.Union(inputs)

	// An explicitly added tx must not be marked as dropped.
	m.droppedTxIDs.Evict(txID)

	m.blkTimer.ResetBlockTimer()
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
		if m.unissuedTxs.Delete(txID) {
			txBytes := tx.Bytes()
			m.bytesAvailable += len(txBytes)
			m.bytesAvailableMetric.Set(float64(m.bytesAvailable))

			m.numTxs.Dec()

			inputs := tx.Unsigned.InputIDs()
			m.consumedUTXOs.Difference(inputs)
		}
	}
}

func (m *mempool) Peek(maxTxSize int) *txs.Tx {
	txIter := m.unissuedTxs.NewIterator()
	for txIter.Next() {
		tx := txIter.Value()
		txSize := len(tx.Bytes())
		if txSize <= maxTxSize {
			return tx
		}
	}
	return nil
}

func (m *mempool) MarkDropped(txID ids.ID, reason error) {
	m.droppedTxIDs.Put(txID, reason)
}

func (m *mempool) GetDropReason(txID ids.ID) error {
	err, _ := m.droppedTxIDs.Get(txID)
	return err
}

func (m *mempool) DropExpiredStakerTxs(minStartTime time.Time) []ids.ID {
	var droppedTxIDs []ids.ID

	txIter := m.unissuedTxs.NewIterator()
	for txIter.Next() {
		tx := txIter.Value()
		stakerTx, ok := tx.Unsigned.(txs.Staker)
		if !ok {
			continue
		}

		startTime := stakerTx.StartTime()
		if !startTime.Before(minStartTime) {
			continue
		}

		txID := tx.ID()
		err := fmt.Errorf(
			"synchrony bound (%s) is later than staker start time (%s)",
			minStartTime,
			startTime,
		)

		m.Remove([]*txs.Tx{tx})
		m.MarkDropped(txID, err) // cache tx as dropped
		droppedTxIDs = append(droppedTxIDs, txID)
	}

	return droppedTxIDs
}
