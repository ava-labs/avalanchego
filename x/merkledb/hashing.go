// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
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

const MaxHashLen = 33

type HashDataT [MaxHashLen]byte

type SerializableID struct {
	data HashDataT
	idImpl[HashDataT]
}

func (id SerializableID) ID() ids.ID                 { return id.idImpl.ID(id.data) }
func (id SerializableID) Encode(writer *codecWriter) { id.idImpl.Encode(id.data, writer) }
func (id SerializableID) EncodeBytes() []byte        { return id.idImpl.EncodeBytes(id.data) }
func (id SerializableID) Len() int                   { return id.idImpl.Len(id.data) }
func (id SerializableID) String() string             { return id.idImpl.String(id.data) }

type idImpl[T any] interface {
	String(T) string
	ID(T) ids.ID
	Encode(T, *codecWriter)
	EncodeBytes(T) []byte
	Len(T) int
}

type sIDImpl struct{}

var _ idImpl[HashDataT] = sIDImpl{}

func (sIDImpl) Len(HashDataT) int                          { return ids.IDLen }
func (sIDImpl) ID(data HashDataT) ids.ID                   { return ids.ID(data[:HashLength]) }
func (sIDImpl) Encode(data HashDataT, writer *codecWriter) { writer.ID(ids.ID(data[:HashLength])) }
func (sIDImpl) EncodeBytes(data HashDataT) []byte          { return data[:HashLength] }
func (s sIDImpl) String(data HashDataT) string             { return s.ID(data).String() }

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

func serializeable(id ids.ID) SerializableID {
	s := SerializableID{
		idImpl: sIDImpl{},
	}
	copy(s.data[:], id[:])
	return s
}

type sha256Hasher struct{}

func (*sha256Hasher) Decode(reader *codecReader) (SerializableID, error) {
	id, err := reader.ID()
	if err != nil {
		return SerializableID{}, err
	}
	return serializeable(id), nil
}

func (*sha256Hasher) DecodeBytes(b []byte) (SerializableID, error) {
	id, err := ids.ToID(b)
	if err != nil {
		return SerializableID{}, err
	}
	return serializeable(id), nil
}

func (*sha256Hasher) Empty() SerializableID {
	return serializeable(ids.Empty)
}

// This method is performance critical. It is not expected to perform any memory
// allocations.
func (*sha256Hasher) HashNode(n *node) SerializableID {
	var (
		// sha.Write always returns nil, so we ignore its return values.
		sha  = sha256.New()
		hash = serializeable(ids.Empty)
		// The hash length is larger than the maximum Uvarint length. This
		// ensures binary.AppendUvarint doesn't perform any memory allocations.
		emptyHashBuffer = hash.data[:0]
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
