// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package state

import (
	"sync"

	"github.com/google/btree"
)

const defaultTreeDegree = 2

var _ StakerIterator = (*treeIterator)(nil)

type treeIterator struct {
	current     *Staker
	next        chan *Staker
	releaseOnce sync.Once
	release     chan struct{}
	wg          sync.WaitGroup
}

// NewTreeIterator returns a new iterator of the stakers in [tree] in ascending
// order. Note that it isn't safe to modify [tree] while iterating over it.
func NewTreeIterator(tree *btree.BTreeG[*Staker]) StakerIterator {
	if tree == nil {
		return EmptyIterator
	}
	it := &treeIterator{
		next:    make(chan *Staker),
		release: make(chan struct{}),
	}
	it.wg.Add(1)
	go func() {
		defer it.wg.Done()
		tree.Ascend(func(i *Staker) bool {
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

func (i *treeIterator) Next() bool {
	next, ok := <-i.next
	i.current = next
	return ok
}

func (i *treeIterator) Value() *Staker {
	return i.current
}

func (i *treeIterator) Release() {
	i.releaseOnce.Do(func() {
		close(i.release)
	})
	i.wg.Wait()
}
