// (c) 2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"errors"

	"github.com/ava-labs/avalanchego/cache"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/units"
)

const (
	// droppedTxIDsCacheSize is the maximum number of dropped txIDs to cache
	droppedTxIDsCacheSize = 50

	// maxMempoolSize is the maximum number of bytes allowed in the mempool
	maxMempoolSize = 64 * units.MiB
)

var (
	errUnknownTxType = errors.New("unknown transaction type")
	errDuplicatedTx  = errors.New("duplicated transaction")
	errMempoolFull   = errors.New("mempool is full")

	_ Mempool = &mempool{}
)

type Mempool interface {
	Add(tx *Tx) error
	Has(txID ids.ID) bool
	Get(txID ids.ID) *Tx

	AddDecisionTx(tx *Tx)
	AddAtomicTx(tx *Tx)
	AddProposalTx(tx *Tx)

	HasDecisionTxs() bool
	HasAtomicTx() bool
	HasProposalTx() bool

	RemoveDecisionTxs(txs []*Tx)
	RemoveAtomicTx(tx *Tx)
	RemoveProposalTx(tx *Tx)

	PopDecisionTxs(numTxs int) []*Tx
	PopAtomicTx() *Tx
	PopProposalTx() *Tx

	MarkDropped(txID ids.ID)
	WasDropped(txID ids.ID) bool
}

// Transactions from clients that have not yet been put into blocks and added to
// consensus
type mempool struct {
	unissuedProposalTxs TxHeap
	unissuedDecisionTxs TxHeap
	unissuedAtomicTxs   TxHeap
	totalBytesSize      int

	droppedTxIDs *cache.LRU
}

func NewMempool() Mempool {
	return &mempool{
		unissuedProposalTxs: NewTxHeapByStartTime(),
		unissuedDecisionTxs: NewTxHeapByAge(),
		unissuedAtomicTxs:   NewTxHeapByAge(),
		droppedTxIDs:        &cache.LRU{Size: droppedTxIDsCacheSize},
	}
}

func (m *mempool) Add(tx *Tx) error {
	// Note: a previously dropped tx can be re-added
	if txID := tx.ID(); m.Has(txID) {
		return errDuplicatedTx
	}
	if txBytes := tx.Bytes(); m.totalBytesSize+len(txBytes) > maxMempoolSize {
		return errMempoolFull
	}

	switch tx.UnsignedTx.(type) {
	case TimedTx:
		m.AddProposalTx(tx)
	case UnsignedDecisionTx:
		m.AddDecisionTx(tx)
	case UnsignedAtomicTx:
		m.AddAtomicTx(tx)
	default:
		return errUnknownTxType
	}

	m.droppedTxIDs.Evict(tx.ID())
	return nil
}

func (m *mempool) Has(txID ids.ID) bool {
	return m.Get(txID) != nil
}

func (m *mempool) Get(txID ids.ID) *Tx {
	if tx := m.unissuedDecisionTxs.Get(txID); tx != nil {
		return tx
	}
	if tx := m.unissuedAtomicTxs.Get(txID); tx != nil {
		return tx
	}
	return m.unissuedProposalTxs.Get(txID)
}

func (m *mempool) AddDecisionTx(tx *Tx) {
	m.unissuedDecisionTxs.Add(tx)
	m.register(tx)
}

func (m *mempool) AddAtomicTx(tx *Tx) {
	m.unissuedAtomicTxs.Add(tx)
	m.register(tx)
}

func (m *mempool) AddProposalTx(tx *Tx) {
	m.unissuedProposalTxs.Add(tx)
	m.register(tx)
}

func (m *mempool) HasDecisionTxs() bool { return m.unissuedDecisionTxs.Len() > 0 }

func (m *mempool) HasAtomicTx() bool { return m.unissuedAtomicTxs.Len() > 0 }

func (m *mempool) HasProposalTx() bool { return m.unissuedProposalTxs.Len() > 0 }

func (m *mempool) RemoveDecisionTxs(txs []*Tx) {
	for _, tx := range txs {
		txID := tx.ID()
		m.unissuedDecisionTxs.Remove(txID)
		m.deregister(tx)
	}
}

func (m *mempool) RemoveAtomicTx(tx *Tx) {
	txID := tx.ID()
	m.unissuedAtomicTxs.Remove(txID)
	m.deregister(tx)
}

func (m *mempool) RemoveProposalTx(tx *Tx) {
	txID := tx.ID()
	m.unissuedProposalTxs.Remove(txID)
	m.deregister(tx)
}

func (m *mempool) PopDecisionTxs(numTxs int) []*Tx {
	if maxLen := m.unissuedDecisionTxs.Len(); numTxs > maxLen {
		numTxs = maxLen
	}

	txs := make([]*Tx, numTxs)
	for i := range txs {
		tx := m.unissuedDecisionTxs.RemoveTop()
		m.deregister(tx)
		txs[i] = tx
	}
	return txs
}

func (m *mempool) PopAtomicTx() *Tx {
	tx := m.unissuedAtomicTxs.RemoveTop()
	m.deregister(tx)
	return tx
}

func (m *mempool) PopProposalTx() *Tx {
	tx := m.unissuedProposalTxs.RemoveTop()
	m.deregister(tx)
	return tx
}

func (m *mempool) MarkDropped(txID ids.ID) {
	m.droppedTxIDs.Put(txID, struct{}{})
}

func (m *mempool) WasDropped(txID ids.ID) bool {
	_, exist := m.droppedTxIDs.Get(txID)
	return exist
}

func (m *mempool) register(tx *Tx) {
	txBytes := tx.Bytes()
	m.totalBytesSize += len(txBytes)
}

func (m *mempool) deregister(tx *Tx) {
	txBytes := tx.Bytes()
	m.totalBytesSize -= len(txBytes)
}
