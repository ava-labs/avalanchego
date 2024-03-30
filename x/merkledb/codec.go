// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package merkledb

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"io"
	"math"
	"math/bits"
	"slices"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/maybe"
)

const (
	boolLen              = 1
	trueByte             = 1
	falseByte            = 0
	minVarIntLen         = 1
	minMaybeByteSliceLen = boolLen
	minKeyLen            = minVarIntLen
	minByteSliceLen      = minVarIntLen
	minDBNodeLen         = minMaybeByteSliceLen + minVarIntLen
	minChildLen          = minVarIntLen + minKeyLen + ids.IDLen + boolLen
)

var (
	trueBytes  = []byte{trueByte}
	falseBytes = []byte{falseByte}

	errChildIndexTooLarge = errors.New("invalid child index. Must be less than branching factor")
	errLeadingZeroes      = errors.New("varint has leading zeroes")
	errInvalidBool        = errors.New("decoded bool is neither true nor false")
	errNonZeroKeyPadding  = errors.New("key partial byte should be padded with 0s")
	errExtraSpace         = errors.New("trailing buffer space")
	errIntOverflow        = errors.New("value overflows int")
)

// Note that sha256.Write and bytes.Buffer.Write always returns nil, so we
// ignore their return values.

func childSize(index byte, childEntry *child) int {
	// * index
	// * child ID
	// * child key
	// * bool indicating whether the child has a value
	return uintSize(uint64(index)) + ids.IDLen + keySize(childEntry.compressedKey) + boolLen
}

// based on the implementation of encodeUint which uses binary.PutUvarint
func uintSize(value uint64) int {
	if value == 0 {
		return 1
	}
	return (bits.Len64(value) + 6) / 7
}

func keySize(p Key) int {
	return uintSize(uint64(p.length)) + bytesNeeded(p.length)
}

// Assumes [n] is non-nil.
func encodedDBNodeSize(n *dbNode) int {
	// * number of children
	// * bool indicating whether [n] has a value
	// * the value (optional)
	// * children
	size := uintSize(uint64(len(n.children))) + boolLen
	if n.value.HasValue() {
		valueLen := len(n.value.Value())
		size += uintSize(uint64(valueLen)) + valueLen
	}
	// for each non-nil entry, we add the additional size of the child entry
	for index, entry := range n.children {
		size += childSize(index, entry)
	}
	return size
}

// Returns the canonical hash of [n].
//
// Assumes [n] is non-nil.
// This method is performance critical. It is not expected to perform any memory
// allocations.
func hashNode(n *node) ids.ID {
	var (
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
		// By allocating BranchFactorLargest rather than len(n.children), this
		// slice is allocated on the stack rather than the heap.
		// BranchFactorLargest is at least len(n.children) which avoids memory
		// allocations.
		keys := make([]byte, 0, BranchFactorLargest)
		for k := range n.children {
			keys = append(keys, k)
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

// Assumes [n] is non-nil.
func encodeDBNode(n *dbNode) []byte {
	buf := bytes.NewBuffer(make([]byte, 0, encodedDBNodeSize(n)))
	encodeMaybeByteSlice(buf, n.value)

	numChildren := len(n.children)
	encodeUint(buf, uint64(numChildren))

	// Avoid allocating keys entirely if the node doesn't have any children.
	if numChildren == 0 {
		return buf.Bytes()
	}

	// By allocating BranchFactorLargest rather than len(n.children), this slice
	// is allocated on the stack rather than the heap. BranchFactorLargest is
	// at least len(n.children) which avoids memory allocations.
	keys := make([]byte, 0, BranchFactorLargest)
	for k := range n.children {
		keys = append(keys, k)
	}

	// Ensure that the order of entries is correct.
	slices.Sort(keys)
	for _, index := range keys {
		entry := n.children[index]
		encodeUint(buf, uint64(index))
		encodeKeyToBuffer(buf, entry.compressedKey)
		_, _ = buf.Write(entry.id[:])
		encodeBool(buf, entry.hasValue)
	}
	return buf.Bytes()
}

// Assumes [n] is non-nil.
func decodeDBNode(b []byte, n *dbNode) error {
	if minDBNodeLen > len(b) {
		return io.ErrUnexpectedEOF
	}

	src := bytes.NewReader(b)

	value, err := decodeMaybeByteSlice(src)
	if err != nil {
		return err
	}
	n.value = value

	numChildren, err := decodeUint(src)
	switch {
	case err != nil:
		return err
	case numChildren > uint64(src.Len()/minChildLen):
		return io.ErrUnexpectedEOF
	}

	n.children = make(map[byte]*child, numChildren)
	var previousChild uint64
	for i := uint64(0); i < numChildren; i++ {
		index, err := decodeUint(src)
		if err != nil {
			return err
		}
		if (i != 0 && index <= previousChild) || index > math.MaxUint8 {
			return errChildIndexTooLarge
		}
		previousChild = index

		compressedKey, err := decodeKeyFromReader(src)
		if err != nil {
			return err
		}
		childID, err := decodeID(src)
		if err != nil {
			return err
		}
		hasValue, err := decodeBool(src)
		if err != nil {
			return err
		}
		n.children[byte(index)] = &child{
			compressedKey: compressedKey,
			id:            childID,
			hasValue:      hasValue,
		}
	}
	if src.Len() != 0 {
		return errExtraSpace
	}
	return nil
}

func encodeBool(dst *bytes.Buffer, value bool) {
	bytesValue := falseBytes
	if value {
		bytesValue = trueBytes
	}
	_, _ = dst.Write(bytesValue)
}

func decodeBool(src *bytes.Reader) (bool, error) {
	boolByte, err := src.ReadByte()
	switch {
	case err == io.EOF:
		return false, io.ErrUnexpectedEOF
	case err != nil:
		return false, err
	case boolByte == trueByte:
		return true, nil
	case boolByte == falseByte:
		return false, nil
	default:
		return false, errInvalidBool
	}
}

func encodeUint(dst *bytes.Buffer, value uint64) {
	var buf [binary.MaxVarintLen64]byte
	size := binary.PutUvarint(buf[:], value)
	_, _ = dst.Write(buf[:size])
}

func decodeUint(src *bytes.Reader) (uint64, error) {
	// To ensure encoding/decoding is canonical, we need to check for leading
	// zeroes in the varint.
	// The last byte of the varint we read is the most significant byte.
	// If it's 0, then it's a leading zero, which is considered invalid in the
	// canonical encoding.
	startLen := src.Len()
	val64, err := binary.ReadUvarint(src)
	if err != nil {
		if err == io.EOF {
			return 0, io.ErrUnexpectedEOF
		}
		return 0, err
	}
	endLen := src.Len()

	// Just 0x00 is a valid value so don't check if the varint is 1 byte
	if startLen-endLen > 1 {
		if err := src.UnreadByte(); err != nil {
			return 0, err
		}
		lastByte, err := src.ReadByte()
		if err != nil {
			return 0, err
		}
		if lastByte == 0x00 {
			return 0, errLeadingZeroes
		}
	}

	return val64, nil
}

func encodeMaybeByteSlice(dst *bytes.Buffer, maybeValue maybe.Maybe[[]byte]) {
	hasValue := maybeValue.HasValue()
	encodeBool(dst, hasValue)
	if hasValue {
		encodeByteSlice(dst, maybeValue.Value())
	}
}

func decodeMaybeByteSlice(src *bytes.Reader) (maybe.Maybe[[]byte], error) {
	if minMaybeByteSliceLen > src.Len() {
		return maybe.Nothing[[]byte](), io.ErrUnexpectedEOF
	}

	if hasValue, err := decodeBool(src); err != nil || !hasValue {
		return maybe.Nothing[[]byte](), err
	}

	rawBytes, err := decodeByteSlice(src)
	if err != nil {
		return maybe.Nothing[[]byte](), err
	}

	return maybe.Some(rawBytes), nil
}

func encodeByteSlice(dst *bytes.Buffer, value []byte) {
	encodeUint(dst, uint64(len(value)))
	if value != nil {
		_, _ = dst.Write(value)
	}
}

func decodeByteSlice(src *bytes.Reader) ([]byte, error) {
	if minByteSliceLen > src.Len() {
		return nil, io.ErrUnexpectedEOF
	}

	length, err := decodeUint(src)
	switch {
	case err == io.EOF:
		return nil, io.ErrUnexpectedEOF
	case err != nil:
		return nil, err
	case length == 0:
		return nil, nil
	case length > uint64(src.Len()):
		return nil, io.ErrUnexpectedEOF
	}

	result := make([]byte, length)
	_, err = io.ReadFull(src, result)
	if err == io.EOF {
		err = io.ErrUnexpectedEOF
	}
	return result, err
}

func decodeID(src *bytes.Reader) (ids.ID, error) {
	if ids.IDLen > src.Len() {
		return ids.ID{}, io.ErrUnexpectedEOF
	}

	var id ids.ID
	_, err := io.ReadFull(src, id[:])
	if err == io.EOF {
		err = io.ErrUnexpectedEOF
	}
	return id, err
}

func encodeKey(key Key) []byte {
	keyLen := uintSize(uint64(key.length)) + len(key.Bytes())
	buf := bytes.NewBuffer(make([]byte, 0, keyLen))
	encodeKeyToBuffer(buf, key)
	return buf.Bytes()
}

func encodeKeyToBuffer(dst *bytes.Buffer, key Key) {
	encodeUint(dst, uint64(key.length))
	_, _ = dst.Write(key.Bytes())
}

func decodeKey(b []byte) (Key, error) {
	src := bytes.NewReader(b)
	key, err := decodeKeyFromReader(src)
	if err != nil {
		return Key{}, err
	}
	if src.Len() != 0 {
		return Key{}, errExtraSpace
	}
	return key, err
}

func decodeKeyFromReader(src *bytes.Reader) (Key, error) {
	if minKeyLen > src.Len() {
		return Key{}, io.ErrUnexpectedEOF
	}

	length, err := decodeUint(src)
	if err != nil {
		return Key{}, err
	}
	if length > math.MaxInt {
		return Key{}, errIntOverflow
	}
	result := Key{
		length: int(length),
	}
	keyBytesLen := bytesNeeded(result.length)
	if keyBytesLen > src.Len() {
		return Key{}, io.ErrUnexpectedEOF
	}
	buffer := make([]byte, keyBytesLen)
	if _, err := io.ReadFull(src, buffer); err != nil {
		if err == io.EOF {
			err = io.ErrUnexpectedEOF
		}
		return Key{}, err
	}
	if result.hasPartialByte() {
		// Confirm that the padding bits in the partial byte are 0.
		// We want to only look at the bits to the right of the last token, which is at index length-1.
		// Generate a mask where the (result.length % 8) left bits are 0.
		paddingMask := byte(0xFF >> (result.length % 8))
		if buffer[keyBytesLen-1]&paddingMask != 0 {
			return Key{}, errNonZeroKeyPadding
		}
	}
	result.value = string(buffer)
	return result, nil
}
