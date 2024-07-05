// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package merkledb

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
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

type SerializableID interface {
	fmt.Stringer
	ID() ids.ID
	Encode(writer *codecWriter)
	EncodeBytes() []byte
	Len() int
}

type serializeable struct {
	id ids.ID
}

func (_ *serializeable) Len() int                   { return ids.IDLen }
func (s *serializeable) ID() ids.ID                 { return s.id }
func (s *serializeable) Encode(writer *codecWriter) { writer.ID(s.id) }
func (s *serializeable) EncodeBytes() []byte        { return s.id[:] }
func (s *serializeable) String() string             { return s.id.String() }

type Hasher interface {
	// Returns the canonical hash of the non-nil [node].
	HashNode(node *node) SerializableID
	// Returns the canonical hash of [value].
	HashValue(value []byte) ids.ID
	// Returns the ID of the serialized object
	Decode(reader *codecReader) (SerializableID, error)
	DecodeBytes(b []byte) (SerializableID, error)
	// Returns the serializeable ID of the empty object
	Empty() SerializableID
}

type sha256Hasher struct{}

func (*sha256Hasher) Decode(reader *codecReader) (SerializableID, error) {
	id, err := reader.ID()
	if err != nil {
		return nil, err
	}
	return &serializeable{id}, nil
}

func (*sha256Hasher) DecodeBytes(b []byte) (SerializableID, error) {
	id, err := ids.ToID(b)
	if err != nil {
		return nil, err
	}
	return &serializeable{id}, nil
}

func (*sha256Hasher) Empty() SerializableID {
	return &serializeable{ids.Empty}
}

// This method is performance critical. It is not expected to perform any memory
// allocations.
func (*sha256Hasher) HashNode(n *node) SerializableID {
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
			id := entry.id.ID()
			_, _ = sha.Write(id[:])
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
	return &serializeable{hash}
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
