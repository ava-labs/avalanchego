// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package merkledb

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"sync"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/maybe"
)

const (
	boolLen              = 1
	trueByte             = 1
	falseByte            = 0
	minVarIntLen         = 1
	minMaybeByteSliceLen = boolLen
	minSerializedPathLen = minVarIntLen
	minByteSliceLen      = minVarIntLen
	minDBNodeLen         = minMaybeByteSliceLen + minVarIntLen
	minChildLen          = minVarIntLen + minSerializedPathLen + ids.IDLen + boolLen

	estimatedKeyLen            = 64
	estimatedValueLen          = 64
	estimatedCompressedPathLen = 8
	// Child index, child compressed path, child ID, child has value
	estimatedNodeChildLen = minVarIntLen + estimatedCompressedPathLen + ids.IDLen + boolLen
	// Child index, child ID
	hashValuesChildLen = minVarIntLen + ids.IDLen
)

var (
	_ encoderDecoder = (*codecImpl)(nil)

	trueBytes  = []byte{trueByte}
	falseBytes = []byte{falseByte}

	errNegativeNumChildren  = errors.New("number of children is negative")
	errTooManyChildren      = fmt.Errorf("length of children list is larger than branching factor of %d", NodeBranchFactor)
	errChildIndexTooLarge   = fmt.Errorf("invalid child index. Must be less than branching factor of %d", NodeBranchFactor)
	errNegativeNibbleLength = errors.New("nibble length is negative")
	errIntTooLarge          = errors.New("integer too large to be decoded")
	errLeadingZeroes        = errors.New("varint has leading zeroes")
	errInvalidBool          = errors.New("decoded bool is neither true nor false")
	errNonZeroNibblePadding = errors.New("nibbles should be padded with 0s")
	errExtraSpace           = errors.New("trailing buffer space")
	errNegativeSliceLength  = errors.New("negative slice length")
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
	// Assumes [hv] is non-nil.
	encodeHashValues(hv *hashValues) []byte
}

type decoder interface {
	// Assumes [n] is non-nil.
	decodeDBNode(bytes []byte, n *dbNode) error
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
	varIntPool sync.Pool
}

func (c *codecImpl) encodeDBNode(n *dbNode) []byte {
	var (
		numChildren = len(n.children)
		// Estimate size of [n] to prevent memory allocations
		estimatedLen = estimatedValueLen + minVarIntLen + estimatedNodeChildLen*numChildren
		buf          = bytes.NewBuffer(make([]byte, 0, estimatedLen))
	)

	c.encodeMaybeByteSlice(buf, n.value)
	c.encodeInt(buf, numChildren)
	// Note we insert children in order of increasing index
	// for determinism.
	for index := byte(0); index < NodeBranchFactor; index++ {
		if entry, ok := n.children[index]; ok {
			c.encodeInt(buf, int(index))
			path := entry.compressedPath.Serialize()
			c.encodeSerializedPath(buf, path)
			_, _ = buf.Write(entry.id[:])
			c.encodeBool(buf, entry.hasValue)
		}
	}
	return buf.Bytes()
}

func (c *codecImpl) encodeHashValues(hv *hashValues) []byte {
	var (
		numChildren = len(hv.Children)
		// Estimate size [hv] to prevent memory allocations
		estimatedLen = minVarIntLen + numChildren*hashValuesChildLen + estimatedValueLen + estimatedKeyLen
		buf          = bytes.NewBuffer(make([]byte, 0, estimatedLen))
	)

	c.encodeInt(buf, numChildren)

	// ensure that the order of entries is consistent
	for index := byte(0); index < NodeBranchFactor; index++ {
		if entry, ok := hv.Children[index]; ok {
			c.encodeInt(buf, int(index))
			_, _ = buf.Write(entry.id[:])
		}
	}
	c.encodeMaybeByteSlice(buf, hv.Value)
	c.encodeSerializedPath(buf, hv.Key)

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

	numChildren, err := c.decodeInt(src)
	switch {
	case err != nil:
		return err
	case numChildren < 0:
		return errNegativeNumChildren
	case numChildren > NodeBranchFactor:
		return errTooManyChildren
	case numChildren > src.Len()/minChildLen:
		return io.ErrUnexpectedEOF
	}

	n.children = make(map[byte]child, NodeBranchFactor)
	previousChild := -1
	for i := 0; i < numChildren; i++ {
		index, err := c.decodeInt(src)
		if err != nil {
			return err
		}
		if index <= previousChild || index >= NodeBranchFactor {
			return errChildIndexTooLarge
		}
		previousChild = index

		compressedPath, err := c.decodeSerializedPath(src)
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
		n.children[byte(index)] = child{
			compressedPath: compressedPath.deserialize(),
			id:             childID,
			hasValue:       hasValue,
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

func (c *codecImpl) encodeInt(dst *bytes.Buffer, value int) {
	c.encodeInt64(dst, int64(value))
}

func (*codecImpl) decodeInt(src *bytes.Reader) (int, error) {
	// To ensure encoding/decoding is canonical, we need to check for leading
	// zeroes in the varint.
	// The last byte of the varint we read is the most significant byte.
	// If it's 0, then it's a leading zero, which is considered invalid in the
	// canonical encoding.
	startLen := src.Len()
	val64, err := binary.ReadVarint(src)
	switch {
	case err == io.EOF:
		return 0, io.ErrUnexpectedEOF
	case err != nil:
		return 0, err
	case val64 > math.MaxInt:
		return 0, errIntTooLarge
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

	return int(val64), nil
}

func (c *codecImpl) encodeInt64(dst *bytes.Buffer, value int64) {
	buf := c.varIntPool.Get().([]byte)
	size := binary.PutVarint(buf, value)
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

	length, err := c.decodeInt(src)
	switch {
	case err == io.EOF:
		return nil, io.ErrUnexpectedEOF
	case err != nil:
		return nil, err
	case length < 0:
		return nil, errNegativeSliceLength
	case length == 0:
		return nil, nil
	case length > src.Len():
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
	c.encodeInt(dst, len(value))
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

func (c *codecImpl) encodeSerializedPath(dst *bytes.Buffer, s SerializedPath) {
	c.encodeInt(dst, s.NibbleLength)
	_, _ = dst.Write(s.Value)
}

func (c *codecImpl) decodeSerializedPath(src *bytes.Reader) (SerializedPath, error) {
	if minSerializedPathLen > src.Len() {
		return SerializedPath{}, io.ErrUnexpectedEOF
	}

	var (
		result SerializedPath
		err    error
	)
	if result.NibbleLength, err = c.decodeInt(src); err != nil {
		return SerializedPath{}, err
	}
	if result.NibbleLength < 0 {
		return SerializedPath{}, errNegativeNibbleLength
	}
	pathBytesLen := result.NibbleLength >> 1
	hasOddLen := result.hasOddLength()
	if hasOddLen {
		pathBytesLen++
	}
	if pathBytesLen > src.Len() {
		return SerializedPath{}, io.ErrUnexpectedEOF
	}
	result.Value = make([]byte, pathBytesLen)
	if _, err := io.ReadFull(src, result.Value); err != nil {
		if err == io.EOF {
			err = io.ErrUnexpectedEOF
		}
		return SerializedPath{}, err
	}
	if hasOddLen {
		paddedNibble := result.Value[pathBytesLen-1] & 0x0F
		if paddedNibble != 0 {
			return SerializedPath{}, errNonZeroNibblePadding
		}
	}
	return result, nil
}
