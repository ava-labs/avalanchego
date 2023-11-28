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
	PeekTxs(maxTxsBytes int) []*txs.Tx

	// Drops all [txs.Staker] transactions whose [StartTime] is before
	// [minStartTime] from [mempool]. The dropped tx ids are returned.
	//
	// TODO: Remove once [StartTime] field is ignored in staker txs
	DropExpiredStakerTxs(minStartTime time.Time) []ids.ID

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
		dropIncoming:  false, // enable tx adding by default
		toEngine:      toEngine,
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

func (m *mempool) HasTxs() bool {
	return m.unissuedTxs.Len() > 0
}

func (m *mempool) PeekTxs(maxTxsBytes int) []*txs.Tx {
	var txs []*txs.Tx
	txIter := m.unissuedTxs.NewIterator()
	size := 0
	for txIter.Next() {
		tx := txIter.Value()
		size += len(tx.Bytes())
		if size > maxTxsBytes {
			return txs
		}
		txs = append(txs, tx)
	}
	return txs
}

func (m *mempool) MarkDropped(txID ids.ID, reason error) {
	m.droppedTxIDs.Put(txID, reason)
}

func (m *mempool) GetDropReason(txID ids.ID) error {
	err, _ := m.droppedTxIDs.Get(txID)
	return err
}

func (m *mempool) RequestBuildBlock(emptyBlockPermitted bool) {
	if !emptyBlockPermitted && !m.HasTxs() {
		return
	}

	select {
	case m.toEngine <- common.PendingTxs:
	default:
	}
}

// Drops all [txs.Staker] transactions whose [StartTime] is before
// [minStartTime] from [mempool]. The dropped tx ids are returned.
//
// TODO: Remove once [StartTime] field is ignored in staker txs
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
