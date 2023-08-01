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
)

const (
	trueByte             = 1
	falseByte            = 0
	minVarIntLen         = 1
	minMaybeByteSliceLen = 1
	minSerializedPathLen = minVarIntLen
	minByteSliceLen      = minVarIntLen
	minDBNodeLen         = minMaybeByteSliceLen + minVarIntLen
	minChildLen          = minVarIntLen + minSerializedPathLen + ids.IDLen
)

var (
	_ encoderDecoder = (*codecImpl)(nil)

	trueBytes  = []byte{trueByte}
	falseBytes = []byte{falseByte}

	errEncodeNil            = errors.New("can't encode nil pointer or interface")
	errDecodeNil            = errors.New("can't decode nil")
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
	encodeDBNode(n *dbNode) ([]byte, error)
	encodeHashValues(hv *hashValues) ([]byte, error)
}

type decoder interface {
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

type codecImpl struct {
	varIntPool sync.Pool
}

func (c *codecImpl) encodeDBNode(n *dbNode) ([]byte, error) {
	if n == nil {
		return nil, errEncodeNil
	}

	buf := &bytes.Buffer{}
	if err := c.encodeMaybeByteSlice(buf, n.value); err != nil {
		return nil, err
	}
	childrenLength := len(n.children)
	if err := c.encodeInt(buf, childrenLength); err != nil {
		return nil, err
	}
	for index := byte(0); index < NodeBranchFactor; index++ {
		if entry, ok := n.children[index]; ok {
			if err := c.encodeInt(buf, int(index)); err != nil {
				return nil, err
			}
			path := entry.compressedPath.Serialize()
			if err := c.encodeSerializedPath(path, buf); err != nil {
				return nil, err
			}
			if _, err := buf.Write(entry.id[:]); err != nil {
				return nil, err
			}
		}
	}
	return buf.Bytes(), nil
}

func (c *codecImpl) encodeHashValues(hv *hashValues) ([]byte, error) {
	if hv == nil {
		return nil, errEncodeNil
	}

	buf := &bytes.Buffer{}

	length := len(hv.Children)
	if err := c.encodeInt(buf, length); err != nil {
		return nil, err
	}

	// ensure that the order of entries is consistent
	for index := byte(0); index < NodeBranchFactor; index++ {
		if entry, ok := hv.Children[index]; ok {
			if err := c.encodeInt(buf, int(index)); err != nil {
				return nil, err
			}
			if _, err := buf.Write(entry.id[:]); err != nil {
				return nil, err
			}
		}
	}
	if err := c.encodeMaybeByteSlice(buf, hv.Value); err != nil {
		return nil, err
	}
	if err := c.encodeSerializedPath(hv.Key, buf); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

func (c *codecImpl) decodeDBNode(b []byte, n *dbNode) error {
	if n == nil {
		return errDecodeNil
	}
	if minDBNodeLen > len(b) {
		return io.ErrUnexpectedEOF
	}

	var (
		src = bytes.NewReader(b)
		err error
	)

	if n.value, err = c.decodeMaybeByteSlice(src); err != nil {
		return err
	}

	numChildren, err := c.decodeInt(src)
	if err != nil {
		return err
	}
	switch {
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
		var index int
		if index, err = c.decodeInt(src); err != nil {
			return err
		}
		if index <= previousChild || index > NodeBranchFactor-1 {
			return errChildIndexTooLarge
		}
		previousChild = index

		var compressedPath SerializedPath
		if compressedPath, err = c.decodeSerializedPath(src); err != nil {
			return err
		}
		var childID ids.ID
		if childID, err = c.decodeID(src); err != nil {
			return err
		}
		n.children[byte(index)] = child{
			compressedPath: compressedPath.deserialize(),
			id:             childID,
		}
	}
	if src.Len() != 0 {
		return errExtraSpace
	}
	return err
}

func (*codecImpl) encodeBool(dst io.Writer, value bool) error {
	bytesValue := falseBytes
	if value {
		bytesValue = trueBytes
	}
	_, err := dst.Write(bytesValue)
	return err
}

func (*codecImpl) decodeBool(src *bytes.Reader) (bool, error) {
	boolByte, err := src.ReadByte()
	if err == io.EOF {
		return false, io.ErrUnexpectedEOF
	}
	if err != nil {
		return false, err
	}
	switch boolByte {
	case trueByte:
		return true, nil
	case falseByte:
		return false, nil
	default:
		return false, errInvalidBool
	}
}

func (c *codecImpl) encodeInt(dst io.Writer, value int) error {
	return c.encodeInt64(dst, int64(value))
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

func (c *codecImpl) encodeInt64(dst io.Writer, value int64) error {
	buf := c.varIntPool.Get().([]byte)
	size := binary.PutVarint(buf, value)
	_, err := dst.Write(buf[:size])
	c.varIntPool.Put(buf)
	return err
}

func (c *codecImpl) encodeMaybeByteSlice(dst io.Writer, maybeValue Maybe[[]byte]) error {
	if err := c.encodeBool(dst, !maybeValue.IsNothing()); err != nil {
		return err
	}
	if maybeValue.IsNothing() {
		return nil
	}
	return c.encodeByteSlice(dst, maybeValue.Value())
}

func (c *codecImpl) decodeMaybeByteSlice(src *bytes.Reader) (Maybe[[]byte], error) {
	if minMaybeByteSliceLen > src.Len() {
		return Nothing[[]byte](), io.ErrUnexpectedEOF
	}

	if hasValue, err := c.decodeBool(src); err != nil || !hasValue {
		return Nothing[[]byte](), err
	}

	bytes, err := c.decodeByteSlice(src)
	if err != nil {
		return Nothing[[]byte](), err
	}

	return Some(bytes), nil
}

func (c *codecImpl) decodeByteSlice(src *bytes.Reader) ([]byte, error) {
	if minByteSliceLen > src.Len() {
		return nil, io.ErrUnexpectedEOF
	}

	var (
		length int
		err    error
		result []byte
	)
	if length, err = c.decodeInt(src); err != nil {
		if err == io.EOF {
			return nil, io.ErrUnexpectedEOF
		}
		return nil, err
	}

	switch {
	case length < 0:
		return nil, errNegativeSliceLength
	case length == 0:
		return nil, nil
	case length > src.Len():
		return nil, io.ErrUnexpectedEOF
	}

	result = make([]byte, length)
	if _, err := io.ReadFull(src, result); err != nil {
		if err == io.EOF {
			err = io.ErrUnexpectedEOF
		}
		return nil, err
	}
	return result, nil
}

func (c *codecImpl) encodeByteSlice(dst io.Writer, value []byte) error {
	if err := c.encodeInt(dst, len(value)); err != nil {
		return err
	}
	if value != nil {
		if _, err := dst.Write(value); err != nil {
			return err
		}
	}
	return nil
}

func (*codecImpl) decodeID(src *bytes.Reader) (ids.ID, error) {
	if ids.IDLen > src.Len() {
		return ids.ID{}, io.ErrUnexpectedEOF
	}

	var id ids.ID
	if _, err := io.ReadFull(src, id[:]); err != nil {
		if err == io.EOF {
			err = io.ErrUnexpectedEOF
		}
		return id, err
	}
	return id, nil
}

func (c *codecImpl) encodeSerializedPath(s SerializedPath, dst io.Writer) error {
	if err := c.encodeInt(dst, s.NibbleLength); err != nil {
		return err
	}
	_, err := dst.Write(s.Value)
	return err
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
		return result, err
	}
	if result.NibbleLength < 0 {
		return result, errNegativeNibbleLength
	}
	pathBytesLen := result.NibbleLength >> 1
	hasOddLen := result.hasOddLength()
	if hasOddLen {
		pathBytesLen++
	}
	if pathBytesLen > src.Len() {
		return result, io.ErrUnexpectedEOF
	}
	result.Value = make([]byte, pathBytesLen)
	if _, err := io.ReadFull(src, result.Value); err != nil {
		if err == io.EOF {
			err = io.ErrUnexpectedEOF
		}
		return result, err
	}
	if hasOddLen {
		paddedNibble := result.Value[pathBytesLen-1] & 0x0F
		if paddedNibble != 0 {
			return result, errNonZeroNibblePadding
		}
	}
	return result, nil
}
