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

// Mempool contains transactions that have not yet been put into a block.
type Mempool interface {
	Add(tx *txs.Tx) error
	Has(txID ids.ID) bool
	Get(txID ids.ID) *txs.Tx
	Remove(txs []*txs.Tx)

	// Peek returns the next first tx that was added to the mempool whose size
	// is less than or equal to maxTxSize.
	Peek(maxTxSize int) *txs.Tx

	// RequestBuildBlock notifies the consensus engine that a block should be
	// built if there is at least one transaction in the mempool.
	RequestBuildBlock()

	// Note: Dropped txs are added to droppedTxIDs but not evicted from
	// unissued. This allows previously dropped txs to be possibly reissued.
	MarkDropped(txID ids.ID, reason error)
	GetDropReason(txID ids.ID) error
}

type mempool struct {
	bytesAvailableMetric prometheus.Gauge
	bytesAvailable       int

	unissuedTxs linkedhashmap.LinkedHashmap[ids.ID, *txs.Tx]
	numTxs      prometheus.Gauge

	toEngine chan<- common.Message

	// Key: Tx ID
	// Value: Verification error
	droppedTxIDs *cache.LRU[ids.ID, error]

	consumedUTXOs set.Set[ids.ID]
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
		toEngine:             toEngine,
		droppedTxIDs:         &cache.LRU[ids.ID, error]{Size: droppedTxIDsCacheSize},
		consumedUTXOs:        set.NewSet[ids.ID](initialConsumedUTXOsSize),
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

func (m *mempool) RequestBuildBlock() {
	if m.unissuedTxs.Len() == 0 {
		return
	}

	select {
	case m.toEngine <- common.PendingTxs:
	default:
	}
}

func (m *mempool) MarkDropped(txID ids.ID, reason error) {
	m.droppedTxIDs.Put(txID, reason)
}

func (m *mempool) GetDropReason(txID ids.ID) error {
	err, _ := m.droppedTxIDs.Get(txID)
	return err
}
