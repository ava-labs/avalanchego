package platformvm

import (
	"bytes"
	"sort"

	"github.com/ava-labs/avalanchego/cache"
	"github.com/ava-labs/avalanchego/ids"
)

// Txs should be retrieveable by their ID. Also we need to retrieve N of them
// ordered by their insertion

type poolOrderedTx struct {
	tx            *Tx
	entryPosition int
}
type poolOrderedTxs []poolOrderedTx

func (p poolOrderedTxs) Len() int           { return len(p) }
func (p poolOrderedTxs) Less(i, j int) bool { return p[i].entryPosition < p[j].entryPosition }
func (p poolOrderedTxs) Swap(i, j int)      { p[i], p[j] = p[j], p[i] }

// TODO: remove code duplication with EventHeap
type timeOrderedTx struct {
	tx *Tx
}
type timeOrderedTxs []timeOrderedTx

func (t timeOrderedTxs) Len() int { return len(t) }
func (t timeOrderedTxs) Less(i, j int) bool {
	iTx := t[i].tx.UnsignedTx.(TimedTx)
	jTx := t[j].tx.UnsignedTx.(TimedTx)

	iTime := iTx.StartTime()
	jTime := jTx.StartTime()

	switch {
	case iTime.Unix() < jTime.Unix():
		return true
	case iTime == jTime:
		_, iOk := iTx.(*UnsignedAddValidatorTx)
		_, jOk := jTx.(*UnsignedAddValidatorTx)

		if iOk != jOk {
			return iOk
		}
		id1 := iTx.ID()
		id2 := jTx.ID()
		return bytes.Compare(id1[:], id2[:]) == -1
	default:
		return false
	}
}
func (t timeOrderedTxs) Swap(i, j int) { t[i], t[j] = t[j], t[i] }

// Transactions from clients that have not yet been put into blocks and
// added to consensus
type mempoolContent struct {
	unissuedProposalTxs map[ids.ID]timeOrderedTx
	unissuedDecisionTxs map[ids.ID]poolOrderedTx
	unissuedAtomicTxs   map[ids.ID]poolOrderedTx
	unissuedTxs         map[ids.ID]struct{}
	totalBytesSize      int

	rejectedProposalTxs *cache.LRU
	rejectedDecisionTxs *cache.LRU
	rejectedAtomicTxs   *cache.LRU
}

func newMempoolContent() *mempoolContent {
	return &mempoolContent{
		unissuedProposalTxs: make(map[ids.ID]timeOrderedTx),
		unissuedDecisionTxs: make(map[ids.ID]poolOrderedTx),
		unissuedAtomicTxs:   make(map[ids.ID]poolOrderedTx),
		unissuedTxs:         make(map[ids.ID]struct{}),
		rejectedProposalTxs: &cache.LRU{Size: rejectedTxsCacheSize},
		rejectedDecisionTxs: &cache.LRU{Size: rejectedTxsCacheSize},
		rejectedAtomicTxs:   &cache.LRU{Size: rejectedTxsCacheSize},
	}
}

func (mc *mempoolContent) register(tx *Tx) {
	mc.unissuedTxs[tx.ID()] = struct{}{}
	mc.totalBytesSize += len(tx.Bytes())
}

func (mc *mempoolContent) deregister(tx *Tx) {
	delete(mc.unissuedTxs, tx.ID())
	mc.totalBytesSize -= len(tx.Bytes())
}

func (mc *mempoolContent) has(txID ids.ID) bool {
	_, ok := mc.unissuedTxs[txID]
	return ok
}

func (mc *mempoolContent) get(txID ids.ID) *Tx {
	if _, ok := mc.unissuedTxs[txID]; !ok {
		return nil
	}
	if res, ok := mc.unissuedDecisionTxs[txID]; ok {
		return res.tx
	}
	if res, ok := mc.unissuedDecisionTxs[txID]; ok {
		return res.tx
	}
	if res, ok := mc.unissuedAtomicTxs[txID]; ok {
		return res.tx
	}
	return nil
}

func (mc *mempoolContent) hasRoomFor(tx *Tx) bool {
	return mc.totalBytesSize+len(tx.Bytes()) <= MaxMempoolByteSize
}

// DecisionTx-specific methods
func (mc *mempoolContent) AddDecisionTx(tx *Tx) error {
	mc.unissuedDecisionTxs[tx.ID()] = poolOrderedTx{
		tx:            tx,
		entryPosition: len(mc.unissuedDecisionTxs),
	}
	mc.register(tx)
	return nil
}

func (mc *mempoolContent) RemoveDecisionTxs(toDrop []*Tx) {
	for _, tx := range toDrop {
		delete(mc.unissuedDecisionTxs, tx.ID())
		mc.deregister(tx)
	}
}

// select first numTxs decision tx and remove them from mempool
func (mc *mempoolContent) ExtractNextDecisionTxs(numTxs int) []*Tx {
	if numTxs > len(mc.unissuedDecisionTxs) {
		numTxs = len(mc.unissuedDecisionTxs)
	}

	// pick the first numTxs txs by entryPosition
	orderedTxs := make(poolOrderedTxs, len(mc.unissuedDecisionTxs))
	i := 0
	for _, v := range mc.unissuedDecisionTxs {
		orderedTxs[i] = poolOrderedTx{
			tx:            v.tx,
			entryPosition: v.entryPosition,
		}
		i++
	}
	sort.Sort(orderedTxs)
	orderedTxs = orderedTxs[:numTxs]

	// return them
	res := make([]*Tx, numTxs)
	i = 0
	for _, v := range orderedTxs {
		res[i] = v.tx
		delete(mc.unissuedDecisionTxs, v.tx.ID())
		mc.deregister(v.tx)
	}

	return res
}

func (mc *mempoolContent) HasDecisionTxs() bool { return len(mc.unissuedDecisionTxs) > 0 }

// AtomicTx-specific methods
func (mc *mempoolContent) AddAtomicTx(tx *Tx) error {
	mc.unissuedAtomicTxs[tx.ID()] = poolOrderedTx{
		tx:            tx,
		entryPosition: len(mc.unissuedAtomicTxs),
	}
	mc.register(tx)
	return nil
}

func (mc *mempoolContent) RemoveAtomicTx(toDrop *Tx) {
	delete(mc.unissuedAtomicTxs, toDrop.ID())
	mc.deregister(toDrop)
}

func (mc *mempoolContent) ExtractNextAtomicTx() *Tx {
	orderedTxs := make(poolOrderedTxs, len(mc.unissuedAtomicTxs))
	i := 0
	for _, v := range mc.unissuedAtomicTxs {
		orderedTxs[i] = poolOrderedTx{
			tx:            v.tx,
			entryPosition: v.entryPosition,
		}
		i++
	}
	sort.Sort(orderedTxs)
	delete(mc.unissuedAtomicTxs, orderedTxs[0].tx.ID())
	mc.deregister(orderedTxs[0].tx)
	return orderedTxs[0].tx
}

func (mc *mempoolContent) HasAtomicTxs() bool { return len(mc.unissuedAtomicTxs) > 0 }

// ProposalTx-specific methods
func (mc *mempoolContent) AddProposalTx(tx *Tx) error {
	mc.unissuedProposalTxs[tx.ID()] = timeOrderedTx{
		tx: tx,
	}
	mc.register(tx)
	return nil
}

func (mc *mempoolContent) RemoveProposalTx(toDrop *Tx) {
	delete(mc.unissuedProposalTxs, toDrop.ID())
	mc.deregister(toDrop)
}

func (mc *mempoolContent) PeekProposalTx() *Tx {
	// get next tx by time without remove it
	timedTxs := make(timeOrderedTxs, len(mc.unissuedProposalTxs))
	i := 0
	for _, v := range mc.unissuedProposalTxs {
		timedTxs[i] = timeOrderedTx{
			tx: v.tx,
		}
		i++
	}
	sort.Sort(timedTxs)
	return timedTxs[0].tx
}

func (mc *mempoolContent) HasProposalTxs() bool { return len(mc.unissuedProposalTxs) > 0 }

// RejectionTx-specific methods
func (mc *mempoolContent) markReject(tx *Tx) error {
	switch tx.UnsignedTx.(type) {
	case TimedTx:
		mc.rejectedProposalTxs.Put(tx.ID(), struct{}{})
	case UnsignedDecisionTx:
		mc.rejectedDecisionTxs.Put(tx.ID(), struct{}{})
	case UnsignedAtomicTx:
		mc.rejectedAtomicTxs.Put(tx.ID(), struct{}{})
	default:
		return errUnknownTxType
	}
	return nil
}

func (mc *mempoolContent) isAlreadyRejected(txID ids.ID) bool {
	res := false
	if _, exist := mc.rejectedProposalTxs.Get(txID); exist {
		res = true
	} else if _, exist := mc.rejectedDecisionTxs.Get(txID); exist {
		res = true
	} else if _, exist := mc.rejectedAtomicTxs.Get(txID); exist {
		res = true
	}

	return res
}
