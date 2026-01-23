// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.
package firewood

import (
	"runtime"

	"github.com/ava-labs/firewood-go-ethhash/ffi"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/trie"
)

var _ trie.NodeIterator = (*accountIt)(nil)

type accountIt struct {
	it *ffi.Iterator
}

func newAccountIt(rev *ffi.Revision, start []byte) (*accountIt, error) {
	it, err := rev.Iter(start)
	if err != nil {
		return nil, err
	}
	a := &accountIt{
		it: it,
	}

	// Because the user manages the lifetime of the iterator, we must guarantee
	// that the underlying resources are freed when the iterator is garbage
	// collected.
	runtime.AddCleanup(a, func(it *ffi.Iterator) {
		_ = it.Drop()
	}, it)
	return a, nil
}

// Next advances the iterator to the next account.
func (a *accountIt) Next(bool) bool {
	next := a.it.Next()
	for ; next; next = a.it.Next() {
		if len(a.it.Key()) == common.HashLength {
			return true
		}
	}
	return false
}

// Error returns any error that occurred during iteration.
func (a *accountIt) Error() error {
	return a.it.Err()
}

// Leaf always returns true since Firewood only iterates over leaf nodes.
func (a *accountIt) Leaf() bool {
	return true
}

// LeafKey returns the key of the account.
func (a *accountIt) LeafKey() []byte {
	return a.it.Key()
}

// LeafBlob returns the RLP encoded account data.
func (a *accountIt) LeafBlob() []byte {
	return a.it.Value()
}

// LeafProof returns nil since Firewood does not support proofs.
func (a *accountIt) LeafProof() [][]byte {
	return nil
}

// Hash is unused since Firewood does not expose internal hashes.
func (a *accountIt) Hash() common.Hash {
	return common.Hash{}
}

// NodeBlob is unused since Firewood does not expose internal nodes.
func (a *accountIt) NodeBlob() []byte {
	return nil
}

func (a *accountIt) Path() []byte {
	return a.it.Key()
}

func (a *accountIt) AddResolver(trie.NodeResolver) {}

func (a *accountIt) Parent() common.Hash {
	return common.Hash{}
}
