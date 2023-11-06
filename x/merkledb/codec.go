// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package merkledb

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"math"
	"sync"

	"golang.org/x/exp/maps"
	"golang.org/x/exp/slices"

	"github.com/ava-labs/avalanchego/ids"
	genericMath "github.com/ava-labs/avalanchego/utils/math"
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
	minChildLen          = minVarIntLen + minKeyLen + ids.IDLen

	estimatedKeyLen           = 64
	estimatedValueLen         = 64
	estimatedCompressedKeyLen = 8
	// Child index, child compressed key, child ID, child has value
	estimatedNodeChildLen = minVarIntLen + estimatedCompressedKeyLen + ids.IDLen
	// Child index, child ID
	hashValuesChildLen = minVarIntLen + ids.IDLen
)

var (
	_ encoderDecoder = (*codecImpl)(nil)

	trueBytes  = []byte{trueByte}
	falseBytes = []byte{falseByte}

	ErrChildIndexTooLarge = errors.New("invalid child index. Must be less than branching factor")
	ErrLeadingZeroes      = errors.New("varint has leading zeroes")
	ErrInvalidBool        = errors.New("decoded bool is neither true nor false")
	ErrNonZeroKeyPadding  = errors.New("key partial byte should be padded with 0s")
	ErrExtraSpace         = errors.New("trailing buffer space")
	ErrIntOverflow        = errors.New("value overflows int")
)

// encoderDecoder defines the interface needed by merkleDB to marshal
// and unmarshal relevant types.
type encoderDecoder interface {
	encoder
	decoder
}

type encoder interface {
	// Assumes [n] is non-nil.
	encodeNode(n *node) []byte
	encodedNodeSize(n *node) int

	// Returns the bytes that will be hashed to generate [n]'s ID.
	// Assumes [n] is non-nil.
	encodeHashValues(key Key, n nodeChildren, value maybe.Maybe[[]byte]) []byte
}

type decoder interface {
	// Assumes [n] is non-nil.
	decodeNode(bytes []byte) (*node, error)
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

// Note that bytes.Buffer.Write always returns nil so we
// can ignore its return values in [codecImpl] methods.
type codecImpl struct {
	// Invariant: Every byte slice returned by [varIntPool] has
	// length [binary.MaxVarintLen64].
	varIntPool sync.Pool
}

func (c *codecImpl) encodedHashValuesSize(key Key, n nodeChildren, value maybe.Maybe[[]byte]) int {
	// total is storing the node's value digest + the factor number of children pointers + the child entries for n.childCount children
	total := valueOrDigestSize(value) + uintSize(uint64(len(n))) + keySize(key)
	// for each non-nil entry, we add the additional size of the child entry
	for index, _ := range n {
		total += uintSize(uint64(index)) + len(ids.Empty)
	}
	return total
}

func (c *codecImpl) encodedNodeSize(n *node) int {
	if n == nil {
		return 0
	}
	// total the number of children pointers + bool indicating if it has a value + the child entries for n.children
	total := uintSize(uint64(len(n.children))) + boolSize()
	// for each non-nil entry, we add the additional size of the child entry
	for index, entry := range n.children {
		total += childSize(index, entry)
	}
	return total
}

func childSize(index byte, childEntry *child) int {
	return uintSize(uint64(index)) + len(ids.Empty) + keySize(childEntry.compressedKey) + boolSize()
}

func valueOrDigestSize(maybeValue maybe.Maybe[[]byte]) int {
	if maybeValue.HasValue() {
		valueOrDigestLength := genericMath.Min(len(maybeValue.Value()), HashLength)
		return boolSize() + valueOrDigestLength + uintSize(uint64(valueOrDigestLength))
	}
	return boolSize()
}

func boolSize() int {
	return 1
}

var log128 = math.Log(128)

func uintSize(value uint64) int {
	if value == 0 {
		return 1
	}
	return 1 + int(math.Log(float64(value))/log128)
}

func keySize(p Key) int {
	return uintSize(uint64(p.length)) + bytesNeeded(p.length)
}

func (c *codecImpl) encodeNode(n *node) []byte {
	buf := bytes.NewBuffer(make([]byte, 0, c.encodedNodeSize(n)))
	c.encodeBool(buf, n.hasValue)
	c.encodeChildren(buf, n.children)
	return buf.Bytes()
}

func (c *codecImpl) encodeChildren(dst *bytes.Buffer, n nodeChildren) {
	c.encodeUint(dst, uint64(len(n)))
	// Note we insert children in order of increasing index
	// for determinism.
	keys := maps.Keys(n)
	slices.Sort(keys)
	for _, index := range keys {
		entry := n[index]
		c.encodeUint(dst, uint64(index))
		c.encodeKey(dst, entry.compressedKey)
		_, _ = dst.Write(entry.id[:])
	}
}

func (c *codecImpl) encodeHashValues(key Key, n nodeChildren, value maybe.Maybe[[]byte]) []byte {
	buf := bytes.NewBuffer(make([]byte, 0, c.encodedHashValuesSize(key, n, value)))

	c.encodeUint(buf, uint64(len(n)))

	// ensure that the order of entries is consistent
	keys := maps.Keys(n)
	slices.Sort(keys)
	for _, index := range keys {
		entry := n[index]
		c.encodeUint(buf, uint64(index))
		_, _ = buf.Write(entry.id[:])
	}
	c.encodeMaybeByteSlice(buf, getValueDigest(value))
	c.encodeKey(buf, key)

	return buf.Bytes()
}

func (c *codecImpl) decodeNode(b []byte) (*node, error) {
	src := bytes.NewReader(b)

	hasValue, err := c.decodeBool(src)
	numChildren, err := c.decodeUint(src)
	switch {
	case err != nil:
		return nil, err
	case numChildren > uint64(src.Len()/minChildLen):
		return nil, io.ErrUnexpectedEOF
	}
	n := newNode(int(numChildren))
	n.hasValue = hasValue
	var previousChild uint64
	for i := uint64(0); i < numChildren; i++ {
		index, err := c.decodeUint(src)
		if err != nil {
			return nil, err
		}
		if i != 0 && index <= previousChild || index > math.MaxUint8 {
			return nil, ErrChildIndexTooLarge
		}
		previousChild = index

		compressedKey, err := c.decodeKey(src)
		if err != nil {
			return nil, err
		}
		childID, err := c.decodeID(src)
		if err != nil {
			return nil, err
		}
		n.children[byte(index)] = &child{
			compressedKey: compressedKey,
			id:            childID,
		}
	}
	if src.Len() != 0 {
		return nil, ErrExtraSpace
	}
	return n, nil
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
		return false, ErrInvalidBool
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
			return 0, ErrLeadingZeroes
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

	bytes, err := c.decodeByteSlice(src)
	if err != nil {
		return maybe.Nothing[[]byte](), err
	}

	return maybe.Some(bytes), nil
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

func (c *codecImpl) encodeKey(dst *bytes.Buffer, key Key) {
	c.encodeUint(dst, uint64(key.length))
	_, _ = dst.Write(key.Bytes())
}

func (c *codecImpl) decodeKey(src *bytes.Reader) (Key, error) {
	if minKeyLen > src.Len() {
		return Key{}, io.ErrUnexpectedEOF
	}

	length, err := c.decodeUint(src)
	if err != nil {
		return Key{}, err
	}
	if length > math.MaxInt {
		return Key{}, ErrIntOverflow
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
		// Generate a mask where the left [remainderBitCount] bits are 0.
		paddingMask := byte(0xFF >> result.remainderBitCount())
		if buffer[keyBytesLen-1]&paddingMask != 0 {
			return Key{}, ErrNonZeroKeyPadding
		}
	}
	result.value = string(buffer)
	return result, nil
}
