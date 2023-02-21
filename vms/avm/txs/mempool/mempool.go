// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package mempool

import (
	"errors"
	"fmt"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/ava-labs/avalanchego/cache"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/linkedhashmap"
	"github.com/ava-labs/avalanchego/utils/set"
	"github.com/ava-labs/avalanchego/utils/units"
	"github.com/ava-labs/avalanchego/vms/avm/txs"
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
	_ Mempool = &mempool{}

	errDuplicateTx          = errors.New("duplicate tx")
	errTxTooLarge           = errors.New("tx too large")
	errMempoolFull          = errors.New("mempool is full")
	errConflictsWithOtherTx = errors.New("tx conflicts with other tx")
)

type BlockTimer interface {
	// ResetBlockTimer schedules a timer to notify the consensus engine once
	// there is a block ready to be built. If a block is ready to be built when
	// this function is called, the engine will be notified directly.
	ResetBlockTimer()
}

// Mempool contains transactions that have not yet been put into a block.
type Mempool interface {
	Add(tx *txs.Tx) error
	Has(txID ids.ID) bool
	Get(txID ids.ID) *txs.Tx
	Remove(txs []*txs.Tx)

	// HasTxs returns true if there is at least one transaction in the mempool.
	HasTxs() bool

	// Peek returns the next first tx that was added to the mempool whose size
	// is less than or equal to maxTxSize.
	Peek(maxTxSize int) *txs.Tx

	// Note: Dropped txs are added to droppedTxIDs but not not evicted from
	// unissued. This allows previously dropped txs to be possibly reissued.
	MarkDropped(txID ids.ID, reason string)
	GetDropReason(txID ids.ID) (string, bool)
}

type mempool struct {
	bytesAvailableMetric prometheus.Gauge
	bytesAvailable       int

	unissuedTxs linkedhashmap.LinkedHashmap[ids.ID, *txs.Tx]
	numTxs      prometheus.Gauge

	// Key: Tx ID
	// Value: String representation of the verification error
	droppedTxIDs *cache.LRU[ids.ID, string]

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

	numTxsMetric := prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "count",
		Help:      "Number of transactions in the mempool",
	})
	if err := registerer.Register(numTxsMetric); err != nil {
		return nil, err
	}

	bytesAvailableMetric.Set(maxMempoolSize)
	return &mempool{
		bytesAvailableMetric: bytesAvailableMetric,
		bytesAvailable:       maxMempoolSize,
		unissuedTxs:          linkedhashmap.New[ids.ID, *txs.Tx](),
		numTxs:               numTxsMetric,
		droppedTxIDs:         &cache.LRU[ids.ID, string]{Size: droppedTxIDsCacheSize},
		consumedUTXOs:        set.NewSet[ids.ID](initialConsumedUTXOsSize),
		blkTimer:             blkTimer,
	}, nil
}

func (m *mempool) Add(tx *txs.Tx) error {
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

	m.bytesAvailable -= txSize
	m.bytesAvailableMetric.Set(float64(m.bytesAvailable))

	m.unissuedTxs.Put(txID, tx)
	m.numTxs.Inc()

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
	unissuedTxs, _ := m.unissuedTxs.Get(txID)
	return unissuedTxs
}

func (m *mempool) Remove(txsToRemove []*txs.Tx) {
	for _, tx := range txsToRemove {
		txID := tx.ID()
		if _, ok := m.unissuedTxs.Get(txID); !ok {
			// If tx isn't in the mempool, there is nothing to do.
			continue
		}

		txBytes := tx.Bytes()
		m.bytesAvailable += len(txBytes)
		m.bytesAvailableMetric.Set(float64(m.bytesAvailable))

		m.unissuedTxs.Delete(txID)
		m.numTxs.Dec()

		inputs := tx.Unsigned.InputIDs()
		m.consumedUTXOs.Difference(inputs)
	}
}

func (m *mempool) HasTxs() bool {
	return m.unissuedTxs.Len() > 0
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

func (m *mempool) MarkDropped(txID ids.ID, reason string) {
	m.droppedTxIDs.Put(txID, reason)
}

func (m *mempool) GetDropReason(txID ids.ID) (string, bool) {
	return m.droppedTxIDs.Get(txID)
}
