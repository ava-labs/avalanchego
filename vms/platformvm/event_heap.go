// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package platformvm

import (
	"bytes"
	"container/heap"
	"errors"
	"time"

	"github.com/ava-labs/gecko/ids"
	"github.com/ava-labs/gecko/snow/validators"
)

// TimedTx ...
type TimedTx interface {
	ProposalTx

	Vdr() validators.Validator

	ID() ids.ID
	StartTime() time.Time
	EndTime() time.Time
}

// EventHeap is a collection of timedTxs where elements are ordered by either
// their startTime or their endTime. If SortByStartTime == true, the first
// element of [Txs] is the tx in the heap with the earliest startTime. Otherwise
// the first element is the tx with earliest endTime. The default value of this
// struct will order transactions by endTime. This struct implements the heap
// interface.
type EventHeap struct {
	SortByStartTime bool      `serialize:"true"`
	Txs             []TimedTx `serialize:"true"`
}

func (h *EventHeap) Len() int { return len(h.Txs) }
func (h *EventHeap) Less(i, j int) bool {
	iTx := h.Txs[i]
	jTx := h.Txs[j]

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
		_, iOk := iTx.(*addDefaultSubnetValidatorTx)
		_, jOk := jTx.(*addDefaultSubnetValidatorTx)

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
	if h.SortByStartTime {
		return h.Txs[0].StartTime()
	}
	return h.Txs[0].EndTime()
}

// Add ...
func (h *EventHeap) Add(tx TimedTx) { heap.Push(h, tx) }

// Peek ...
func (h *EventHeap) Peek() TimedTx { return h.Txs[0] }

// Remove ...
func (h *EventHeap) Remove() TimedTx { return heap.Pop(h).(TimedTx) }

// Push implements the heap interface
func (h *EventHeap) Push(x interface{}) { h.Txs = append(h.Txs, x.(TimedTx)) }

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

// getDefaultSubnetStaker ...
func (h *EventHeap) getDefaultSubnetStaker(id ids.ShortID) (*addDefaultSubnetValidatorTx, error) {
	for _, txIntf := range h.Txs {
		tx, ok := txIntf.(*addDefaultSubnetValidatorTx)
		if !ok {
			continue
		}
		if id.Equals(tx.NodeID) {
			return tx, nil
		}
	}
	return nil, errors.New("couldn't find validator in the default subnet")
}
