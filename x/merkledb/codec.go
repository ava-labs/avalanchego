// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
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
	"github.com/ava-labs/avalanchego/utils/hashing"
)

const (
	codecVersion         = 0
	trueByte             = 1
	falseByte            = 0
	minVarIntLen         = 1
	boolLen              = 1
	idLen                = hashing.HashLen
	minSerializedPathLen = minVarIntLen
	minByteSliceLen      = minVarIntLen
	minDeletedKeyLen     = minByteSliceLen
	minMaybeByteSliceLen = boolLen
	minProofPathLen      = minVarIntLen
	minKeyValueLen       = 2 * minByteSliceLen
	minProofNodeLen      = minSerializedPathLen + minMaybeByteSliceLen + minVarIntLen
	minProofLen          = minProofPathLen + minByteSliceLen
	minChangeProofLen    = boolLen + 2*minProofPathLen + 2*minVarIntLen
	minRangeProofLen     = 2*minProofPathLen + minVarIntLen
	minDBNodeLen         = minMaybeByteSliceLen + minVarIntLen
	minHashValuesLen     = minVarIntLen + minMaybeByteSliceLen + minSerializedPathLen
	minProofNodeChildLen = minVarIntLen + idLen
	minChildLen          = minVarIntLen + minSerializedPathLen + idLen
)

var (
	_ EncoderDecoder = (*codecImpl)(nil)

	trueBytes  = []byte{trueByte}
	falseBytes = []byte{falseByte}

	errUnknownVersion         = errors.New("unknown codec version")
	errEncodeNil              = errors.New("can't encode nil pointer or interface")
	errDecodeNil              = errors.New("can't decode nil")
	errNegativeProofPathNodes = errors.New("negative proof path length")
	errNegativeNumChildren    = errors.New("number of children is negative")
	errTooManyChildren        = fmt.Errorf("length of children list is larger than branching factor of %d", NodeBranchFactor)
	errChildIndexTooLarge     = fmt.Errorf("invalid child index. Must be less than branching factor of %d", NodeBranchFactor)
	errNegativeNibbleLength   = errors.New("nibble length is negative")
	errNegativeNumKeyValues   = errors.New("negative number of key values")
	errIntTooLarge            = errors.New("integer too large to be decoded")
	errLeadingZeroes          = errors.New("varint has leading zeroes")
	errInvalidBool            = errors.New("decoded bool is neither true nor false")
	errNonZeroNibblePadding   = errors.New("nibbles should be padded with 0s")
	errExtraSpace             = errors.New("trailing buffer space")
	errNegativeSliceLength    = errors.New("negative slice length")
)

// EncoderDecoder defines the interface needed by merkleDB to marshal
// and unmarshal relevant types.
type EncoderDecoder interface {
	Encoder
	Decoder
}

// TODO actually encode the version and remove version from the interface
type Encoder interface {
	EncodeProof(version uint16, p *Proof) ([]byte, error)
	EncodeChangeProof(version uint16, p *ChangeProof) ([]byte, error)
	EncodeRangeProof(version uint16, p *RangeProof) ([]byte, error)

	encodeDBNode(version uint16, n *dbNode) ([]byte, error)
	encodeHashValues(version uint16, hv *hashValues) ([]byte, error)
}

type Decoder interface {
	DecodeProof(bytes []byte, p *Proof) (uint16, error)
	DecodeChangeProof(bytes []byte, p *ChangeProof) (uint16, error)
	DecodeRangeProof(bytes []byte, p *RangeProof) (uint16, error)

	decodeDBNode(bytes []byte, n *dbNode) (uint16, error)
}

func newCodec() (EncoderDecoder, uint16) {
	return &codecImpl{
		varIntPool: sync.Pool{
			New: func() interface{} {
				return make([]byte, binary.MaxVarintLen64)
			},
		},
	}, codecVersion
}

type codecImpl struct {
	varIntPool sync.Pool
}

func (c *codecImpl) EncodeProof(version uint16, proof *Proof) ([]byte, error) {
	if proof == nil {
		return nil, errEncodeNil
	}

	if version != codecVersion {
		return nil, errUnknownVersion
	}

	buf := &bytes.Buffer{}
	if err := c.encodeProofPath(buf, proof.Path); err != nil {
		return nil, err
	}
	if err := c.encodeByteSlice(buf, proof.Key); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func (c *codecImpl) EncodeChangeProof(version uint16, proof *ChangeProof) ([]byte, error) {
	if proof == nil {
		return nil, errEncodeNil
	}

	if version != codecVersion {
		return nil, errUnknownVersion
	}

	buf := &bytes.Buffer{}
	if err := c.encodeBool(buf, proof.HadRootsInHistory); err != nil {
		return nil, err
	}

	if err := c.encodeProofPath(buf, proof.StartProof); err != nil {
		return nil, err
	}
	if err := c.encodeProofPath(buf, proof.EndProof); err != nil {
		return nil, err
	}
	if err := c.encodeInt(buf, len(proof.KeyValues)); err != nil {
		return nil, err
	}
	for _, kv := range proof.KeyValues {
		if err := c.encodeKeyValue(kv, buf); err != nil {
			return nil, err
		}
	}

	if err := c.encodeInt(buf, len(proof.DeletedKeys)); err != nil {
		return nil, err
	}
	for _, key := range proof.DeletedKeys {
		if err := c.encodeByteSlice(buf, key); err != nil {
			return nil, err
		}
	}
	return buf.Bytes(), nil
}

func (c *codecImpl) EncodeRangeProof(version uint16, proof *RangeProof) ([]byte, error) {
	if proof == nil {
		return nil, errEncodeNil
	}

	if version != codecVersion {
		return nil, errUnknownVersion
	}

	buf := &bytes.Buffer{}
	if err := c.encodeProofPath(buf, proof.StartProof); err != nil {
		return nil, err
	}
	if err := c.encodeProofPath(buf, proof.EndProof); err != nil {
		return nil, err
	}
	if err := c.encodeInt(buf, len(proof.KeyValues)); err != nil {
		return nil, err
	}
	for _, kv := range proof.KeyValues {
		if err := c.encodeKeyValue(kv, buf); err != nil {
			return nil, err
		}
	}

	return buf.Bytes(), nil
}

func (c *codecImpl) encodeDBNode(version uint16, n *dbNode) ([]byte, error) {
	if n == nil {
		return nil, errEncodeNil
	}

	if version != codecVersion {
		return nil, errUnknownVersion
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

func (c *codecImpl) encodeHashValues(version uint16, hv *hashValues) ([]byte, error) {
	if hv == nil {
		return nil, errEncodeNil
	}

	if version != codecVersion {
		return nil, errUnknownVersion
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

func (c *codecImpl) DecodeProof(b []byte, proof *Proof) (uint16, error) {
	if proof == nil {
		return 0, errDecodeNil
	}
	if minProofLen > len(b) {
		return 0, io.ErrUnexpectedEOF
	}

	var (
		err error
		src = bytes.NewReader(b)
	)

	if proof.Path, err = c.decodeProofPath(src); err != nil {
		return 0, err
	}
	if proof.Key, err = c.decodeByteSlice(src); err != nil {
		return 0, err
	}
	if src.Len() != 0 {
		return 0, errExtraSpace
	}
	return codecVersion, nil
}

func (c *codecImpl) DecodeChangeProof(b []byte, proof *ChangeProof) (uint16, error) {
	if proof == nil {
		return 0, errDecodeNil
	}
	if minChangeProofLen > len(b) {
		return 0, io.ErrUnexpectedEOF
	}

	var (
		src = bytes.NewReader(b)
		err error
	)

	if proof.HadRootsInHistory, err = c.decodeBool(src); err != nil {
		return 0, err
	}
	if proof.StartProof, err = c.decodeProofPath(src); err != nil {
		return 0, err
	}
	if proof.EndProof, err = c.decodeProofPath(src); err != nil {
		return 0, err
	}

	numKeyValues, err := c.decodeInt(src)
	if err != nil {
		return 0, err
	}
	if numKeyValues < 0 {
		return 0, errNegativeNumKeyValues
	}
	if numKeyValues > src.Len()/minKeyValueLen {
		return 0, io.ErrUnexpectedEOF
	}
	proof.KeyValues = make([]KeyValue, numKeyValues)
	for i := range proof.KeyValues {
		if proof.KeyValues[i], err = c.decodeKeyValue(src); err != nil {
			return 0, err
		}
	}

	numDeletedKeys, err := c.decodeInt(src)
	if err != nil {
		return 0, err
	}
	if numDeletedKeys < 0 {
		return 0, errNegativeNumKeyValues
	}
	if numDeletedKeys > src.Len()/minDeletedKeyLen {
		return 0, io.ErrUnexpectedEOF
	}
	proof.DeletedKeys = make([][]byte, numDeletedKeys)
	for i := range proof.DeletedKeys {
		if proof.DeletedKeys[i], err = c.decodeByteSlice(src); err != nil {
			return 0, err
		}
	}
	if src.Len() != 0 {
		return 0, errExtraSpace
	}
	return codecVersion, nil
}

func (c *codecImpl) DecodeRangeProof(b []byte, proof *RangeProof) (uint16, error) {
	if proof == nil {
		return 0, errDecodeNil
	}
	if minRangeProofLen > len(b) {
		return 0, io.ErrUnexpectedEOF
	}

	var (
		src = bytes.NewReader(b)
		err error
	)

	if proof.StartProof, err = c.decodeProofPath(src); err != nil {
		return 0, err
	}
	if proof.EndProof, err = c.decodeProofPath(src); err != nil {
		return 0, err
	}

	numKeyValues, err := c.decodeInt(src)
	if err != nil {
		return 0, err
	}
	if numKeyValues < 0 {
		return 0, errNegativeNumKeyValues
	}
	if numKeyValues > src.Len()/minKeyValueLen {
		return 0, io.ErrUnexpectedEOF
	}
	proof.KeyValues = make([]KeyValue, numKeyValues)
	for i := range proof.KeyValues {
		if proof.KeyValues[i], err = c.decodeKeyValue(src); err != nil {
			return 0, err
		}
	}
	if src.Len() != 0 {
		return 0, errExtraSpace
	}
	return codecVersion, nil
}

func (c *codecImpl) decodeDBNode(b []byte, n *dbNode) (uint16, error) {
	if n == nil {
		return 0, errDecodeNil
	}
	if minDBNodeLen > len(b) {
		return 0, io.ErrUnexpectedEOF
	}

	var (
		src = bytes.NewReader(b)
		err error
	)

	if n.value, err = c.decodeMaybeByteSlice(src); err != nil {
		return 0, err
	}

	numChildren, err := c.decodeInt(src)
	if err != nil {
		return 0, err
	}
	switch {
	case numChildren < 0:
		return 0, errNegativeNumChildren
	case numChildren > NodeBranchFactor:
		return 0, errTooManyChildren
	case numChildren > src.Len()/minChildLen:
		return 0, io.ErrUnexpectedEOF
	}

	n.children = make(map[byte]child, NodeBranchFactor)
	previousChild := -1
	for i := 0; i < numChildren; i++ {
		var index int
		if index, err = c.decodeInt(src); err != nil {
			return 0, err
		}
		if index <= previousChild || index > NodeBranchFactor-1 {
			return 0, errChildIndexTooLarge
		}
		previousChild = index

		var compressedPath SerializedPath
		if compressedPath, err = c.decodeSerializedPath(src); err != nil {
			return 0, err
		}
		var childID ids.ID
		if childID, err = c.decodeID(src); err != nil {
			return 0, err
		}
		n.children[byte(index)] = child{
			compressedPath: compressedPath.deserialize(),
			id:             childID,
		}
	}
	if src.Len() != 0 {
		return 0, errExtraSpace
	}
	return codecVersion, err
}

func (c *codecImpl) decodeKeyValue(src *bytes.Reader) (KeyValue, error) {
	if minKeyValueLen > src.Len() {
		return KeyValue{}, io.ErrUnexpectedEOF
	}

	var (
		result KeyValue
		err    error
	)
	if result.Key, err = c.decodeByteSlice(src); err != nil {
		return result, err
	}
	if result.Value, err = c.decodeByteSlice(src); err != nil {
		return result, err
	}
	return result, nil
}

func (c *codecImpl) encodeKeyValue(kv KeyValue, dst io.Writer) error {
	if err := c.encodeByteSlice(dst, kv.Key); err != nil {
		return err
	}
	if err := c.encodeByteSlice(dst, kv.Value); err != nil {
		return err
	}
	return nil
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
	if idLen > src.Len() {
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

// Assumes a proof path has > 0 nodes.
func (c *codecImpl) decodeProofPath(src *bytes.Reader) ([]ProofNode, error) {
	if minProofPathLen > src.Len() {
		return nil, io.ErrUnexpectedEOF
	}

	numProofNodes, err := c.decodeInt(src)
	if err != nil {
		return nil, err
	}
	if numProofNodes < 0 {
		return nil, errNegativeProofPathNodes
	}
	if numProofNodes > src.Len()/minProofNodeLen {
		return nil, io.ErrUnexpectedEOF
	}
	result := make([]ProofNode, numProofNodes)
	for i := 0; i < numProofNodes; i++ {
		if result[i], err = c.decodeProofNode(src); err != nil {
			return nil, err
		}
	}
	return result, nil
}

// Invariant: len(path) > 0.
func (c *codecImpl) encodeProofPath(dst io.Writer, path []ProofNode) error {
	if err := c.encodeInt(dst, len(path)); err != nil {
		return err
	}
	for _, proofNode := range path {
		if err := c.encodeProofNode(proofNode, dst); err != nil {
			return err
		}
	}
	return nil
}

func (c *codecImpl) decodeProofNode(src *bytes.Reader) (ProofNode, error) {
	if minProofNodeLen > src.Len() {
		return ProofNode{}, io.ErrUnexpectedEOF
	}

	var (
		result ProofNode
		err    error
	)
	if result.KeyPath, err = c.decodeSerializedPath(src); err != nil {
		return result, err
	}
	if result.Value, err = c.decodeMaybeByteSlice(src); err != nil {
		return result, err
	}
	numChildren, err := c.decodeInt(src)
	if err != nil {
		return result, err
	}
	switch {
	case numChildren < 0:
		return result, errNegativeNumChildren
	case numChildren > NodeBranchFactor:
		return result, errTooManyChildren
	case numChildren > src.Len()/minProofNodeChildLen:
		return result, io.ErrUnexpectedEOF
	}

	result.Children = make(map[byte]ids.ID, numChildren)
	previousChild := -1
	for addedEntries := 0; addedEntries < numChildren; addedEntries++ {
		index, err := c.decodeInt(src)
		if err != nil {
			return result, err
		}
		if index <= previousChild || index >= NodeBranchFactor {
			return result, errChildIndexTooLarge
		}
		previousChild = index

		childID, err := c.decodeID(src)
		if err != nil {
			return result, err
		}
		result.Children[byte(index)] = childID
	}
	return result, nil
}

func (c *codecImpl) encodeProofNode(pn ProofNode, dst io.Writer) error {
	if err := c.encodeSerializedPath(pn.KeyPath, dst); err != nil {
		return err
	}
	if err := c.encodeMaybeByteSlice(dst, pn.Value); err != nil {
		return err
	}
	if err := c.encodeInt(dst, len(pn.Children)); err != nil {
		return err
	}
	// ensure this is in order
	childrenCount := 0
	for index := byte(0); index < NodeBranchFactor; index++ {
		childID, ok := pn.Children[index]
		if !ok {
			continue
		}
		childrenCount++
		if err := c.encodeInt(dst, int(index)); err != nil {
			return err
		}
		if _, err := dst.Write(childID[:]); err != nil {
			return err
		}
	}
	// there are children present with index >= NodeBranchFactor
	if childrenCount != len(pn.Children) {
		return errChildIndexTooLarge
	}
	return nil
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
