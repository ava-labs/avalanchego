// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package heap

import (
	"container/heap"
	"iter"

	"github.com/ava-labs/avalanchego/utils"
)

var _ heap.Interface = (*indexedQueue[int, int])(nil)

func MapValues[K comparable, V any](m Map[K, V]) []V {
	result := make([]V, 0, m.Len())
	for _, e := range m.queue.entries {
		result = append(result, e.v)
	}
	return result
}

// NewMap returns a heap without duplicates ordered by its values
func NewMap[K comparable, V any](less func(a, b V) bool) Map[K, V] {
	return Map[K, V]{
		queue: &indexedQueue[K, V]{
			queue: queue[entry[K, V]]{
				less: func(a, b entry[K, V]) bool {
					return less(a.v, b.v)
				},
			},
			index: make(map[K]int),
		},
	}
}

type Map[K comparable, V any] struct {
	queue *indexedQueue[K, V]
}

// Push returns the evicted previous value if present
func (m *Map[K, V]) Push(k K, v V) (V, bool) {
	if i, ok := m.queue.index[k]; ok {
		prev := m.queue.entries[i]
		m.queue.entries[i].v = v
		heap.Fix(m.queue, i)
		return prev.v, true
	}

	heap.Push(m.queue, entry[K, V]{k: k, v: v})
	return utils.Zero[V](), false
}

func (m *Map[K, V]) Pop() (K, V, bool) {
	if m.Len() == 0 {
		return utils.Zero[K](), utils.Zero[V](), false
	}

	popped := heap.Pop(m.queue).(entry[K, V])
	return popped.k, popped.v, true
}

func (m *Map[K, V]) Peek() (K, V, bool) {
	if m.Len() == 0 {
		return utils.Zero[K](), utils.Zero[V](), false
	}

	entry := m.queue.entries[0]
	return entry.k, entry.v, true
}

func (m *Map[K, V]) Len() int {
	return m.queue.Len()
}

func (m *Map[K, V]) Remove(k K) (V, bool) {
	if i, ok := m.queue.index[k]; ok {
		removed := heap.Remove(m.queue, i).(entry[K, V])
		return removed.v, true
	}
	return utils.Zero[V](), false
}

func (m *Map[K, V]) Contains(k K) bool {
	_, ok := m.queue.index[k]
	return ok
}

func (m *Map[K, V]) Get(k K) (V, bool) {
	if i, ok := m.queue.index[k]; ok {
		got := m.queue.entries[i]
		return got.v, true
	}
	return utils.Zero[V](), false
}

func (m *Map[K, V]) Fix(k K) {
	if i, ok := m.queue.index[k]; ok {
		heap.Fix(m.queue, i)
	}
}

func (m *Map[K, V]) Iterator() iter.Seq2[K, V] {
	return m.queue.Iterator()
}

type indexedQueue[K comparable, V any] struct {
	queue[entry[K, V]]
	index map[K]int
}

func (h *indexedQueue[K, V]) Swap(i, j int) {
	h.entries[i], h.entries[j] = h.entries[j], h.entries[i]
	h.index[h.entries[i].k], h.index[h.entries[j].k] = i, j
}

func (h *indexedQueue[K, V]) Push(x any) {
	entry := x.(entry[K, V])
	h.entries = append(h.entries, entry)
	h.index[entry.k] = len(h.index)
}

func (h *indexedQueue[K, V]) Pop() any {
	end := len(h.entries) - 1

	popped := h.entries[end]
	h.entries[end] = entry[K, V]{}
	h.entries = h.entries[:end]

	delete(h.index, popped.k)
	return popped
}

func (h *indexedQueue[K, V]) Iterator() iter.Seq2[K, V] {
	return func(yield func(K, V) bool) {
		for e := range h.queue.Iterator() {
			if !yield(e.k, e.v) {
				return
			}
		}
	}
}

type entry[K any, V any] struct {
	k K
	v V
}
