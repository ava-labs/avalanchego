// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"bytes"
	"container/heap"
	"time"

	"github.com/ava-labs/gecko/ids"
)

// TimedTx ...
type TimedTx interface {
	ID() ids.ID
	StartTime() time.Time
	EndTime() time.Time
	Bytes() []byte
}

// EventHeap is a collection of timedTxs where elements are ordered by either
// their startTime or their endTime. If SortByStartTime == true, the first
// element of [Txs] is the tx in the heap with the earliest startTime. Otherwise
// the first element is the tx with earliest endTime. The default value of this
// struct will order transactions by endTime. This struct implements the heap
// interface.
// Transactions must be syntactically verified before adding to EventHeap to
// ensure that EventHeap can always by marshalled.
type EventHeap struct {
	SortByStartTime bool  `serialize:"true"`
	Txs             []*Tx `serialize:"true"`
}

func (h *EventHeap) Len() int { return len(h.Txs) }
func (h *EventHeap) Less(i, j int) bool {
	iTx := h.Txs[i].UnsignedTx.(TimedTx)
	jTx := h.Txs[j].UnsignedTx.(TimedTx)

	iTime := iTx.EndTime()
	jTime := jTx.EndTime()
	if h.SortByStartTime {
		iTime = iTx.StartTime()
		jTime = jTx.StartTime()
	}

	switch {
	case iTime.Unix() < jTime.Unix():
		return true
	case iTime == jTime:
		_, iOk := iTx.(*UnsignedAddValidatorTx)
		_, jOk := jTx.(*UnsignedAddValidatorTx)

		if iOk != jOk {
			return iOk == h.SortByStartTime
		}
		return bytes.Compare(iTx.ID().Bytes(), jTx.ID().Bytes()) == -1
	default:
		return false
	}
}
func (h *EventHeap) Swap(i, j int) { h.Txs[i], h.Txs[j] = h.Txs[j], h.Txs[i] }

// Timestamp returns the timestamp on the top transaction on the heap
func (h *EventHeap) Timestamp() time.Time {
	tx := h.Txs[0].UnsignedTx.(TimedTx)
	if h.SortByStartTime {
		return tx.StartTime()
	}
	return tx.EndTime()
}

// Add ...
func (h *EventHeap) Add(tx *Tx) { heap.Push(h, tx) }

// Peek ...
func (h *EventHeap) Peek() *Tx { return h.Txs[0] }

// Remove ...
func (h *EventHeap) Remove() *Tx { return heap.Pop(h).(*Tx) }

// Push implements the heap interface
func (h *EventHeap) Push(x interface{}) { h.Txs = append(h.Txs, x.(*Tx)) }

// Pop implements the heap interface
func (h *EventHeap) Pop() interface{} {
	newLen := len(h.Txs) - 1
	val := h.Txs[newLen]
	h.Txs = h.Txs[:newLen]
	return val
}

// Bytes returns the byte representation of this heap
func (h *EventHeap) Bytes() ([]byte, error) {
	return Codec.Marshal(h)
}
