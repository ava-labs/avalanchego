package platformvm

import (
	"github.com/ava-labs/avalanchego/cache"
	"github.com/ava-labs/avalanchego/ids"
)

var _ Mempool = &mempoolContent{}

type Mempool interface {
	Has(txID ids.ID) bool
	Get(txID ids.ID) *Tx
}

// Transactions from clients that have not yet been put into blocks and added to
// consensus
type mempoolContent struct {
	unissuedProposalTxs TxHeap
	unissuedDecisionTxs TxHeap
	unissuedAtomicTxs   TxHeap
	totalBytesSize      int

	rejectedTxs *cache.LRU
}

func newMempoolContent() *mempoolContent {
	return &mempoolContent{
		unissuedProposalTxs: NewTxHeapByStartTime(),
		unissuedDecisionTxs: NewTxHeapByAge(),
		unissuedAtomicTxs:   NewTxHeapByAge(),
		rejectedTxs:         &cache.LRU{Size: rejectedTxsCacheSize},
	}
}

func (mc *mempoolContent) Has(txID ids.ID) bool {
	return mc.Get(txID) != nil
}

func (mc *mempoolContent) Get(txID ids.ID) *Tx {
	if tx := mc.unissuedDecisionTxs.Get(txID); tx != nil {
		return tx
	}
	if tx := mc.unissuedAtomicTxs.Get(txID); tx != nil {
		return tx
	}
	return mc.unissuedProposalTxs.Get(txID)
}

func (mc *mempoolContent) register(tx *Tx) {
	txBytes := tx.Bytes()
	mc.totalBytesSize += len(txBytes)
}

func (mc *mempoolContent) deregister(tx *Tx) {
	txBytes := tx.Bytes()
	mc.totalBytesSize -= len(txBytes)
}

func (mc *mempoolContent) hasRoomFor(tx *Tx) bool {
	txBytes := tx.Bytes()
	return mc.totalBytesSize+len(txBytes) <= MaxMempoolByteSize
}

// DecisionTx-specific methods
func (mc *mempoolContent) AddDecisionTx(tx *Tx) {
	mc.unissuedDecisionTxs.Add(tx)
	mc.register(tx)
}

func (mc *mempoolContent) RemoveDecisionTxs(txs []*Tx) {
	for _, tx := range txs {
		txID := tx.ID()
		mc.unissuedDecisionTxs.Remove(txID)
		mc.deregister(tx)
	}
}

// select first numTxs decision tx and remove them from mempool
func (mc *mempoolContent) ExtractNextDecisionTxs(numTxs int) []*Tx {
	if maxLen := mc.unissuedDecisionTxs.Len(); numTxs > maxLen {
		numTxs = maxLen
	}

	txs := make([]*Tx, numTxs)
	for i := range txs {
		tx := mc.unissuedDecisionTxs.RemoveTop()
		mc.deregister(tx)
		txs[i] = tx
	}
	return txs
}

func (mc *mempoolContent) HasDecisionTxs() bool { return mc.unissuedDecisionTxs.Len() > 0 }

// AtomicTx-specific methods
func (mc *mempoolContent) AddAtomicTx(tx *Tx) {
	mc.unissuedAtomicTxs.Add(tx)
	mc.register(tx)
}

func (mc *mempoolContent) RemoveAtomicTx(tx *Tx) {
	txID := tx.ID()
	mc.unissuedAtomicTxs.Remove(txID)
	mc.deregister(tx)
}

func (mc *mempoolContent) ExtractNextAtomicTx() *Tx {
	tx := mc.unissuedAtomicTxs.RemoveTop()
	mc.deregister(tx)
	return tx
}

func (mc *mempoolContent) HasAtomicTxs() bool { return mc.unissuedAtomicTxs.Len() > 0 }

// ProposalTx-specific methods
func (mc *mempoolContent) AddProposalTx(tx *Tx) {
	mc.unissuedProposalTxs.Add(tx)
	mc.register(tx)
}

func (mc *mempoolContent) RemoveProposalTx(tx *Tx) {
	txID := tx.ID()
	mc.unissuedProposalTxs.Remove(txID)
	mc.deregister(tx)
}

func (mc *mempoolContent) PeekProposalTx() *Tx {
	return mc.unissuedProposalTxs.Peek()
}

func (mc *mempoolContent) HasProposalTxs() bool { return mc.unissuedProposalTxs.Len() > 0 }

// RejectionTx-specific methods
func (mc *mempoolContent) markReject(txID ids.ID) {
	mc.rejectedTxs.Put(txID, struct{}{})
}

func (mc *mempoolContent) isAlreadyRejected(txID ids.ID) bool {
	_, exist := mc.rejectedTxs.Get(txID)
	return exist
}
