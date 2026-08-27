// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package hashdb

import (
	"errors"
	"fmt"
	"iter"

	"github.com/ava-labs/libevm/trie"
)

// errInvalidLeafKey is returned when trie iteration reaches a leaf whose key
// is of unexpected length.
var errInvalidLeafKey = errors.New("iterated leaf key is of unexpected length")

// Pair represents a key and value returned by the [LeafIterator].
type Pair struct {
	Key, Value []byte
}

// LeafIterator opens a guarded leaf iterator over t, starting at start.
// keyLength is the expected trie key length in bytes.
//
// start MUST be the same length as any key in the trie.
func LeafIterator(t *trie.Trie, start []byte) iter.Seq2[Pair, error] {
	keyLength := len(start)
	return func(yield func(Pair, error) bool) {
		it, err := t.NodeIterator(start)
		if err != nil {
			yield(Pair{}, err)
			return
		}

		for it.Next(true) {
			if !it.Leaf() {
				continue
			}
			// A leaf's path includes a terminator symbol in addition to the key.
			if path := it.Path(); len(path) != 2*keyLength+1 {
				yield(Pair{}, fmt.Errorf("%w: leaf path length %d", errInvalidLeafKey, len(path)))
				return
			}

			// While [trie.NodeIterator] forbids retaining LeafKey and LeafBlob past
			// Next, the implementation never reuses their memory, so it.Key and
			// it.Value are safe to retain (matching the existing handler behavior).
			if !yield(Pair{it.LeafKey(), it.LeafBlob()}, nil) {
				return
			}
		}

		if err := it.Error(); err != nil {
			yield(Pair{}, err)
		}
	}
}
