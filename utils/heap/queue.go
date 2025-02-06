// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package heap

import (
	"container/heap"
	"iter"

	"github.com/ava-labs/avalanchego/utils"
)

var _ heap.Interface = (*queue[int])(nil)

// NewQueue returns an empty heap. See QueueOf for more.
func NewQueue[T any](less func(a, b T) bool) Queue[T] {
	return QueueOf(less)
}

// QueueOf returns a heap containing entries ordered by less.
func QueueOf[T any](less func(a, b T) bool, entries ...T) Queue[T] {
	q := Queue[T]{
		queue: &queue[T]{
			entries: make([]T, len(entries)),
			less:    less,
		},
	}

	copy(q.queue.entries, entries)
	heap.Init(q.queue)
	return q
}

type Queue[T any] struct {
	queue *queue[T]
}

func (q *Queue[T]) Len() int {
	return len(q.queue.entries)
}

func (q *Queue[T]) Push(t T) {
	heap.Push(q.queue, t)
}

func (q *Queue[T]) Pop() (T, bool) {
	if q.Len() == 0 {
		return utils.Zero[T](), false
	}

	return heap.Pop(q.queue).(T), true
}

func (q *Queue[T]) Peek() (T, bool) {
	if q.Len() == 0 {
		return utils.Zero[T](), false
	}

	return q.queue.entries[0], true
}

func (q *Queue[T]) Fix(i int) {
	heap.Fix(q.queue, i)
}

type queue[T any] struct {
	entries []T
	less    func(a, b T) bool
}

func (q *queue[T]) Len() int {
	return len(q.entries)
}

func (q *queue[T]) Less(i, j int) bool {
	return q.less(q.entries[i], q.entries[j])
}

func (q *queue[T]) Swap(i, j int) {
	q.entries[i], q.entries[j] = q.entries[j], q.entries[i]
}

func (q *queue[T]) Push(e any) {
	q.entries = append(q.entries, e.(T))
}

func (q *queue[T]) Pop() any {
	end := len(q.entries) - 1

	popped := q.entries[end]
	q.entries[end] = utils.Zero[T]()
	q.entries = q.entries[:end]

	return popped
}

func (q *queue[T]) Iterator() iter.Seq[T] {
	return func(yield func(T) bool) {
		qCopy := queue[T]{
			less: q.less,
		}

		qCopy.entries = make([]T, len(q.entries))
		copy(qCopy.entries, q.entries)

		for qCopy.Len() > 0 {
			element := heap.Pop(&qCopy).(T)
			if !yield(element) {
				return
			}
		}
	}
}
