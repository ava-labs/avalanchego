// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package validator

import "container/heap"

// EndTimeHeap orders validators by EndTime from earliest to latest.
type EndTimeHeap []*Validator

func (h *EndTimeHeap) Len() int                 { return len(*h) }
func (h *EndTimeHeap) Less(i, j int) bool       { return (*h)[i].EndTime().Before((*h)[j].EndTime()) }
func (h *EndTimeHeap) Swap(i, j int)            { (*h)[i], (*h)[j] = (*h)[j], (*h)[i] }
func (h *EndTimeHeap) Add(validator *Validator) { heap.Push(h, validator) }
func (h *EndTimeHeap) Peek() *Validator         { return (*h)[0] }
func (h *EndTimeHeap) Remove() *Validator       { return heap.Pop(h).(*Validator) }
func (h *EndTimeHeap) Push(x interface{})       { *h = append(*h, x.(*Validator)) }
func (h *EndTimeHeap) Pop() interface{} {
	newLen := len(*h) - 1
	val := (*h)[newLen]
	(*h)[newLen] = nil
	*h = (*h)[:newLen]
	return val
}
