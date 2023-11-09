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

// Defines transaction iterator with mempool.
type TxIterator interface {
	// Moves the iterator to the next transaction.
	// Returns false once there's no more transaction to return.
	Next() bool

	// Returns the current transaction.
	// Should only be called after a call to Next which returned true.
	Value() *txs.Tx
}

type Mempool interface {
	// we may want to be able to stop valid transactions
	// from entering the mempool, e.g. during blocks creation
	EnableAdding()
	DisableAdding()

	Add(tx *txs.Tx) error
	Has(txID ids.ID) bool
	Get(txID ids.ID) *txs.Tx
	Remove(txs []*txs.Tx)

	// Following Banff activation, all mempool transactions,
	// (both decision and staker) are included into Standard blocks.
	// HasTxs allow to check for availability of any mempool transaction.
	HasTxs() bool

	// PeekTxs returns the next txs for Banff blocks
	// up to maxTxsBytes without removing them from the mempool.
	// TODO: remove this
	PeekTxs(maxTxsBytes int) []*txs.Tx

	GetTxIterator() TxIterator

	// Note: dropped txs are added to droppedTxIDs but are not evicted from
	// unissued decision/staker txs. This allows previously dropped txs to be
	// possibly reissued.
	MarkDropped(txID ids.ID, reason error)
	GetDropReason(txID ids.ID) error
}

// Transactions from clients that have not yet been put into blocks and added to
// consensus
type mempool struct {
	// If true, drop transactions added to the mempool via Add.
	dropIncoming bool

	bytesAvailableMetric prometheus.Gauge
	bytesAvailable       int

	unissuedTxs linkedhashmap.LinkedHashmap[ids.ID, *txs.Tx]
	txsNum      prometheus.Gauge

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

	txsNum := prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "txs",
		Help:      "Number of decision/staker transactions in the mempool",
	})
	if err := registerer.Register(txsNum); err != nil {
		return nil, err
	}

	bytesAvailableMetric.Set(maxMempoolSize)
	return &mempool{
		bytesAvailableMetric: bytesAvailableMetric,
		bytesAvailable:       maxMempoolSize,

		unissuedTxs: linkedhashmap.New[ids.ID, *txs.Tx](),
		txsNum:      txsNum,

		droppedTxIDs:  &cache.LRU[ids.ID, error]{Size: droppedTxIDsCacheSize},
		consumedUTXOs: set.NewSet[ids.ID](initialConsumedUTXOsSize),
		dropIncoming:  false, // enable tx adding by default
		blkTimer:      blkTimer,
	}, nil
}

func (m *mempool) EnableAdding() {
	m.dropIncoming = false
}

func (m *mempool) DisableAdding() {
	m.dropIncoming = true
}

func (m *mempool) Add(tx *txs.Tx) error {
	if m.dropIncoming {
		return fmt.Errorf("tx %s not added because mempool is closed", tx.ID())
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

	if err := tx.Unsigned.Visit(&issuer{
		m:  m,
		tx: tx,
	}); err != nil {
		return err
	}

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
	remover := &remover{
		m: m,
	}

	for _, tx := range txsToRemove {
		remover.tx = tx
		_ = tx.Unsigned.Visit(remover)
	}
}

func (m *mempool) HasTxs() bool {
	return m.unissuedTxs.Len() > 0
}

// TODO: remove this
func (m *mempool) PeekTxs(maxTxsBytes int) []*txs.Tx {
	var txs []*txs.Tx
	txsSizeSum := 0

	txIter := m.unissuedTxs.NewIterator()
	for txIter.Next() {
		tx := txIter.Value()
		txsSizeSum += tx.Size()
		if txsSizeSum > maxTxsBytes {
			break
		}
		txs = append(txs, tx)
	}

	return txs
}

func (m *mempool) addTx(tx *txs.Tx) {
	m.unissuedTxs.Put(tx.ID(), tx)
	m.register(tx)
}

func (m *mempool) removeTxs(txs ...*txs.Tx) {
	for _, tx := range txs {
		txID := tx.ID()
		if m.unissuedTxs.Delete(txID) {
			m.deregister(tx)
		}
	}
}

func (m *mempool) GetTxIterator() TxIterator {
	return m.unissuedTxs.NewIterator()
}

func (m *mempool) MarkDropped(txID ids.ID, reason error) {
	m.droppedTxIDs.Put(txID, reason)
}

func (m *mempool) GetDropReason(txID ids.ID) error {
	err, _ := m.droppedTxIDs.Get(txID)
	return err
}

func (m *mempool) register(tx *txs.Tx) {
	txBytes := tx.Bytes()
	m.bytesAvailable -= len(txBytes)
	m.bytesAvailableMetric.Set(float64(m.bytesAvailable))
}

func (m *mempool) deregister(tx *txs.Tx) {
	txBytes := tx.Bytes()
	m.bytesAvailable += len(txBytes)
	m.bytesAvailableMetric.Set(float64(m.bytesAvailable))

	inputs := tx.Unsigned.InputIDs()
	m.consumedUTXOs.Difference(inputs)
}

// Drops all [txs.Staker] transactions whose [StartTime] is before
// [minStartTime] from [mempool]. The dropped tx ids are returned.
//
// TODO: Remove once [StartTime] field is ignored in staker txs
func DropExpiredStakerTxs(mempool Mempool, minStartTime time.Time) []ids.ID {
	var droppedTxIDs []ids.ID

	for mempool.HasStakerTx() {
		tx := mempool.PeekStakerTx()
		startTime := tx.Unsigned.(txs.Staker).StartTime()
		if !startTime.Before(minStartTime) {
			// The next proposal tx in the mempool starts sufficiently far in
			// the future.
			break
		}

		txID := tx.ID()
		err := fmt.Errorf(
			"synchrony bound (%s) is later than staker start time (%s)",
			minStartTime,
			startTime,
		)

		mempool.Remove([]*txs.Tx{tx})
		mempool.MarkDropped(txID, err) // cache tx as dropped
		droppedTxIDs = append(droppedTxIDs, txID)
	}

	return droppedTxIDs
}
