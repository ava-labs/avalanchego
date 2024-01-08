// Copyright (C) 2019-2024, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package merkledb

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"math"
	"math/bits"
	"sync"

	"golang.org/x/exp/maps"
	"golang.org/x/exp/slices"

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

	estimatedKeyLen   = 64
	estimatedValueLen = 64
	// Child index, child ID
	hashValuesChildLen = minVarIntLen + ids.IDLen
)

var (
	_ encoderDecoder = (*codecImpl)(nil)

	trueBytes  = []byte{trueByte}
	falseBytes = []byte{falseByte}

	errChildIndexTooLarge = errors.New("invalid child index. Must be less than branching factor")
	errLeadingZeroes      = errors.New("varint has leading zeroes")
	errInvalidBool        = errors.New("decoded bool is neither true nor false")
	errNonZeroKeyPadding  = errors.New("key partial byte should be padded with 0s")
	errExtraSpace         = errors.New("trailing buffer space")
	errIntOverflow        = errors.New("value overflows int")
)

// encoderDecoder defines the interface needed by merkleDB to marshal
// and unmarshal relevant types.
type encoderDecoder interface {
	encoder
	decoder
}

type encoder interface {
	// Assumes [n] is non-nil.
	encodeDBNode(n *dbNode) []byte
	encodedDBNodeSize(n *dbNode) int

	// Returns the bytes that will be hashed to generate [n]'s ID.
	// Assumes [n] is non-nil.
	encodeHashValues(n *node) []byte
	encodeKey(key Key) []byte
}

type decoder interface {
	// Assumes [n] is non-nil.
	decodeDBNode(bytes []byte, n *dbNode) error
	decodeKey(bytes []byte) (Key, error)
}

func newCodec() encoderDecoder {
	return &codecImpl{
		varIntPool: sync.Pool{
			New: func() interface{} {
				return make([]byte, binary.MaxVarintLen64)
			},
		},
	}
}

// Note that bytes.Buffer.Write always returns nil, so we
// can ignore its return values in [codecImpl] methods.
type codecImpl struct {
	// Invariant: Every byte slice returned by [varIntPool] has
	// length [binary.MaxVarintLen64].
	varIntPool sync.Pool
}

func (c *codecImpl) childSize(index byte, childEntry *child) int {
	// * index
	// * child ID
	// * child key
	// * bool indicating whether the child has a value
	return c.uintSize(uint64(index)) + ids.IDLen + c.keySize(childEntry.compressedKey) + boolLen
}

// based on the current implementation of codecImpl.encodeUint which uses binary.PutUvarint
func (*codecImpl) uintSize(value uint64) int {
	if value == 0 {
		return 1
	}
	return (bits.Len64(value) + 6) / 7
}

func (c *codecImpl) keySize(p Key) int {
	return c.uintSize(uint64(p.length)) + bytesNeeded(p.length)
}

func (c *codecImpl) encodedDBNodeSize(n *dbNode) int {
	// * number of children
	// * bool indicating whether [n] has a value
	// * the value (optional)
	// * children
	size := c.uintSize(uint64(len(n.children))) + boolLen
	if n.value.HasValue() {
		valueLen := len(n.value.Value())
		size += c.uintSize(uint64(valueLen)) + valueLen
	}
	// for each non-nil entry, we add the additional size of the child entry
	for index, entry := range n.children {
		size += c.childSize(index, entry)
	}
	return size
}

func (c *codecImpl) encodeDBNode(n *dbNode) []byte {
	buf := bytes.NewBuffer(make([]byte, 0, c.encodedDBNodeSize(n)))
	c.encodeMaybeByteSlice(buf, n.value)
	c.encodeUint(buf, uint64(len(n.children)))
	// Note we insert children in order of increasing index
	// for determinism.
	keys := maps.Keys(n.children)
	slices.Sort(keys)
	for _, index := range keys {
		entry := n.children[index]
		c.encodeUint(buf, uint64(index))
		c.encodeKeyToBuffer(buf, entry.compressedKey)
		_, _ = buf.Write(entry.id[:])
		c.encodeBool(buf, entry.hasValue)
	}
	return buf.Bytes()
}

func (c *codecImpl) encodeHashValues(n *node) []byte {
	var (
		numChildren = len(n.children)
		// Estimate size [hv] to prevent memory allocations
		estimatedLen = minVarIntLen + numChildren*hashValuesChildLen + estimatedValueLen + estimatedKeyLen
		buf          = bytes.NewBuffer(make([]byte, 0, estimatedLen))
	)

	c.encodeUint(buf, uint64(numChildren))

	// ensure that the order of entries is consistent
	keys := maps.Keys(n.children)
	slices.Sort(keys)
	for _, index := range keys {
		entry := n.children[index]
		c.encodeUint(buf, uint64(index))
		_, _ = buf.Write(entry.id[:])
	}
	c.encodeMaybeByteSlice(buf, n.valueDigest)
	c.encodeKeyToBuffer(buf, n.key)

	return buf.Bytes()
}

func (c *codecImpl) decodeDBNode(b []byte, n *dbNode) error {
	if minDBNodeLen > len(b) {
		return io.ErrUnexpectedEOF
	}

	src := bytes.NewReader(b)

	value, err := c.decodeMaybeByteSlice(src)
	if err != nil {
		return err
	}
	n.value = value

	numChildren, err := c.decodeUint(src)
	switch {
	case err != nil:
		return err
	case numChildren > uint64(src.Len()/minChildLen):
		return io.ErrUnexpectedEOF
	}

	n.children = make(map[byte]*child, numChildren)
	var previousChild uint64
	for i := uint64(0); i < numChildren; i++ {
		index, err := c.decodeUint(src)
		if err != nil {
			return err
		}
		if (i != 0 && index <= previousChild) || index > math.MaxUint8 {
			return errChildIndexTooLarge
		}
		previousChild = index

		compressedKey, err := c.decodeKeyFromReader(src)
		if err != nil {
			return err
		}
		childID, err := c.decodeID(src)
		if err != nil {
			return err
		}
		hasValue, err := c.decodeBool(src)
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

func (*codecImpl) encodeBool(dst *bytes.Buffer, value bool) {
	bytesValue := falseBytes
	if value {
		bytesValue = trueBytes
	}
	_, _ = dst.Write(bytesValue)
}

func (*codecImpl) decodeBool(src *bytes.Reader) (bool, error) {
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

func (*codecImpl) decodeUint(src *bytes.Reader) (uint64, error) {
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

func (c *codecImpl) encodeUint(dst *bytes.Buffer, value uint64) {
	buf := c.varIntPool.Get().([]byte)
	size := binary.PutUvarint(buf, value)
	_, _ = dst.Write(buf[:size])
	c.varIntPool.Put(buf)
}

func (c *codecImpl) encodeMaybeByteSlice(dst *bytes.Buffer, maybeValue maybe.Maybe[[]byte]) {
	hasValue := maybeValue.HasValue()
	c.encodeBool(dst, hasValue)
	if hasValue {
		c.encodeByteSlice(dst, maybeValue.Value())
	}
}

func (c *codecImpl) decodeMaybeByteSlice(src *bytes.Reader) (maybe.Maybe[[]byte], error) {
	if minMaybeByteSliceLen > src.Len() {
		return maybe.Nothing[[]byte](), io.ErrUnexpectedEOF
	}

	if hasValue, err := c.decodeBool(src); err != nil || !hasValue {
		return maybe.Nothing[[]byte](), err
	}

	rawBytes, err := c.decodeByteSlice(src)
	if err != nil {
		return maybe.Nothing[[]byte](), err
	}

	return maybe.Some(rawBytes), nil
}

func (c *codecImpl) decodeByteSlice(src *bytes.Reader) ([]byte, error) {
	if minByteSliceLen > src.Len() {
		return nil, io.ErrUnexpectedEOF
	}

	length, err := c.decodeUint(src)
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

func (c *codecImpl) encodeByteSlice(dst *bytes.Buffer, value []byte) {
	c.encodeUint(dst, uint64(len(value)))
	if value != nil {
		_, _ = dst.Write(value)
	}
}

func (*codecImpl) decodeID(src *bytes.Reader) (ids.ID, error) {
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

func (c *codecImpl) encodeKey(key Key) []byte {
	estimatedLen := binary.MaxVarintLen64 + len(key.Bytes())
	dst := bytes.NewBuffer(make([]byte, 0, estimatedLen))
	c.encodeKeyToBuffer(dst, key)
	return dst.Bytes()
}

func (c *codecImpl) encodeKeyToBuffer(dst *bytes.Buffer, key Key) {
	c.encodeUint(dst, uint64(key.length))
	_, _ = dst.Write(key.Bytes())
}

func (c *codecImpl) decodeKey(b []byte) (Key, error) {
	src := bytes.NewReader(b)
	key, err := c.decodeKeyFromReader(src)
	if err != nil {
		return Key{}, err
	}
	if src.Len() != 0 {
		return Key{}, errExtraSpace
	}
	return key, err
}

func (c *codecImpl) decodeKeyFromReader(src *bytes.Reader) (Key, error) {
	if minKeyLen > src.Len() {
		return Key{}, io.ErrUnexpectedEOF
	}

	length, err := c.decodeUint(src)
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
