// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package firewood

import (
	"runtime"

	"github.com/ava-labs/firewood-go-ethhash/ffi"
	"github.com/ava-labs/libevm/common"
	"github.com/ava-labs/libevm/log"
	"github.com/ava-labs/libevm/trie"
)

var _ trie.NodeIterator = (*accountIterator)(nil)

type accountIterator struct {
	it *ffi.Iterator
}

func newAccountIterator(rev *ffi.Revision, start []byte) (*accountIterator, error) {
	it, err := rev.Iter(start)
	if err != nil {
		return nil, err
	}
	a := &accountIterator{
		it: it,
	}

	// Because the user manages the lifetime of the iterator, we must guarantee
	// that the underlying resources are freed when the iterator is garbage
	// collected.
	runtime.AddCleanup(a, func(it *ffi.Iterator) {
		if err := it.Drop(); err != nil {
			log.Warn("failed to drop iterator: %w, Firewood may leak resources", err)
		}
	}, it)
	return a, nil
}

// Next advances the iterator to the next account.
func (a *accountIterator) Next(bool) bool {
	next := a.it.Next()
	for ; next; next = a.it.Next() {
		if len(a.it.Key()) == common.HashLength {
			return true
		}
	}
	return false
}

// Error returns any error that occurred during iteration.
func (a *accountIterator) Error() error {
	return a.it.Err()
}

// Leaf always returns true since Firewood only iterates over leaf nodes.
func (*accountIterator) Leaf() bool {
	return true
}

// LeafKey returns the key of the account.
func (a *accountIterator) LeafKey() []byte {
	return a.it.Key()
}

// LeafBlob returns the RLP encoded account data.
func (a *accountIterator) LeafBlob() []byte {
	return a.it.Value()
}

// LeafProof returns nil since Firewood does not support proofs.
func (*accountIterator) LeafProof() [][]byte {
	return nil
}

// Hash is unused since Firewood does not expose internal hashes.
func (*accountIterator) Hash() common.Hash {
	return common.Hash{}
}

// NodeBlob is unused since Firewood does not expose internal nodes.
func (*accountIterator) NodeBlob() []byte {
	return nil
}

func (a *accountIterator) Path() []byte {
	return a.it.Key()
}

func (*accountIterator) AddResolver(trie.NodeResolver) {}

func (*accountIterator) Parent() common.Hash {
	return common.Hash{}
}
