// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package merkledb

import (
	"crypto/sha256"
	"encoding/binary"
	"slices"

	"github.com/ava-labs/avalanchego/ids"
)

// TODO: Support configurable hash lengths
const HashLength = 32

var (
	SHA256Hasher Hasher = &sha256Hasher{}

	// If a Hasher isn't specified, this package defaults to using the
	// [SHA256Hasher].
	DefaultHasher = SHA256Hasher
)

type Hasher interface {
	// Returns the canonical hash of the non-nil [node].
	HashNode(node *node) ids.ID
	// Returns the canonical hash of [value].
	HashValue(value []byte) ids.ID
}

type sha256Hasher struct{}

// This method is performance critical. It is not expected to perform any memory
// allocations.
func (*sha256Hasher) HashNode(n *node) ids.ID {
	var (
		// sha.Write always returns nil, so we ignore its return values.
		sha  = sha256.New()
		hash ids.ID
		// The hash length is larger than the maximum Uvarint length. This
		// ensures binary.AppendUvarint doesn't perform any memory allocations.
		emptyHashBuffer = hash[:0]
	)

	// By directly calling sha.Write rather than passing sha around as an
	// io.Writer, the compiler can perform sufficient escape analysis to avoid
	// allocating buffers on the heap.
	numChildren := len(n.children)
	_, _ = sha.Write(binary.AppendUvarint(emptyHashBuffer, uint64(numChildren)))

	// Avoid allocating keys entirely if the node doesn't have any children.
	if numChildren != 0 {
		// By allocating BranchFactorLargest rather than [numChildren], this
		// slice is allocated on the stack rather than the heap.
		// BranchFactorLargest is at least [numChildren] which avoids memory
		// allocations.
		keys := make([]byte, numChildren, BranchFactorLargest)
		i := 0
		for k := range n.children {
			keys[i] = k
			i++
		}

		// Ensure that the order of entries is correct.
		slices.Sort(keys)
		for _, index := range keys {
			entry := n.children[index]
			_, _ = sha.Write(binary.AppendUvarint(emptyHashBuffer, uint64(index)))
			_, _ = sha.Write(entry.id[:])
		}
	}

	if n.valueDigest.HasValue() {
		_, _ = sha.Write(trueBytes)
		value := n.valueDigest.Value()
		_, _ = sha.Write(binary.AppendUvarint(emptyHashBuffer, uint64(len(value))))
		_, _ = sha.Write(value)
	} else {
		_, _ = sha.Write(falseBytes)
	}

	_, _ = sha.Write(binary.AppendUvarint(emptyHashBuffer, uint64(n.key.length)))
	_, _ = sha.Write(n.key.Bytes())
	sha.Sum(emptyHashBuffer)
	return hash
}

// This method is performance critical. It is not expected to perform any memory
// allocations.
func (*sha256Hasher) HashValue(value []byte) ids.ID {
	sha := sha256.New()
	// sha.Write always returns nil, so we ignore its return values.
	_, _ = sha.Write(value)

	var hash ids.ID
	sha.Sum(hash[:0])
	return hash
}
