// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package iterator

import (
	"sync"

	"github.com/google/btree"
)

var _ Iterator[any] = (*tree[any])(nil)

type tree[T any] struct {
	current     T
	next        chan T
	releaseOnce sync.Once
	release     chan struct{}
	wg          sync.WaitGroup
}

// FromTree returns a new iterator of the stakers in [tree] in ascending order.
// Note that it isn't safe to modify [tree] while iterating over it.
func FromTree[T any](btree *btree.BTreeG[T]) Iterator[T] {
	if btree == nil {
		return Empty[T]{}
	}
	it := &tree[T]{
		next:    make(chan T),
		release: make(chan struct{}),
	}
	it.wg.Add(1)
	go func() {
		defer it.wg.Done()
		btree.Ascend(func(i T) bool {
			select {
			case it.next <- i:
				return true
			case <-it.release:
				return false
			}
		})
		close(it.next)
	}()
	return it
}

func (i *tree[_]) Next() bool {
	next, ok := <-i.next
	i.current = next
	return ok
}

func (i *tree[T]) Value() T {
	return i.current
}

func (i *tree[_]) Release() {
	i.releaseOnce.Do(func() {
		close(i.release)
	})
	i.wg.Wait()
}
