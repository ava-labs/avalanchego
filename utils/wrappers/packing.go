// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package wrappers

import (
	"encoding/binary"
	"errors"
	"math"
)

const (
	MaxStringLen = math.MaxUint16

	// ByteLen is the number of bytes per byte...
	ByteLen = 1
	// ShortLen is the number of bytes per short
	ShortLen = 2
	// IntLen is the number of bytes per int
	IntLen = 4
	// LongLen is the number of bytes per long
	LongLen = 8
	// BoolLen is the number of bytes per bool
	BoolLen = 1
)

func StringLen(str string) int {
	// note: there is a max length for string ([MaxStringLen])
	// we defer to PackString checking whether str is within limits
	return ShortLen + len(str)
}

var (
	ErrInsufficientLength = errors.New("packer has insufficient length for input")
	errNegativeOffset     = errors.New("negative offset")
	errInvalidInput       = errors.New("input does not match expected format")
	errBadBool            = errors.New("unexpected value when unpacking bool")
	errOversized          = errors.New("size is larger than limit")
)

// Packer packs and unpacks a byte array from/to standard values
type Packer struct {
	Errs

	// The largest allowed size of expanding the byte array
	MaxSize int
	// The current byte array
	Bytes []byte
	// The offset that is being written to in the byte array
	Offset int
}

// PackByte append a byte to the byte array
func (p *Packer) PackByte(val byte) {
	p.expand(ByteLen)
	if p.Errored() {
		return
	}

	p.Bytes[p.Offset] = val
	p.Offset++
}

// UnpackByte unpack a byte from the byte array
func (p *Packer) UnpackByte() byte {
	p.checkSpace(ByteLen)
	if p.Errored() {
		return 0
	}

	val := p.Bytes[p.Offset]
	p.Offset += ByteLen
	return val
}

// PackShort append a short to the byte array
func (p *Packer) PackShort(val uint16) {
	p.expand(ShortLen)
	if p.Errored() {
		return
	}

	binary.BigEndian.PutUint16(p.Bytes[p.Offset:], val)
	p.Offset += ShortLen
}

// UnpackShort unpack a short from the byte array
func (p *Packer) UnpackShort() uint16 {
	p.checkSpace(ShortLen)
	if p.Errored() {
		return 0
	}

	val := binary.BigEndian.Uint16(p.Bytes[p.Offset:])
	p.Offset += ShortLen
	return val
}

// PackInt append an int to the byte array
func (p *Packer) PackInt(val uint32) {
	p.expand(IntLen)
	if p.Errored() {
		return
	}

	binary.BigEndian.PutUint32(p.Bytes[p.Offset:], val)
	p.Offset += IntLen
}

// UnpackInt unpack an int from the byte array
func (p *Packer) UnpackInt() uint32 {
	p.checkSpace(IntLen)
	if p.Errored() {
		return 0
	}

	val := binary.BigEndian.Uint32(p.Bytes[p.Offset:])
	p.Offset += IntLen
	return val
}

// PackLong append a long to the byte array
func (p *Packer) PackLong(val uint64) {
	p.expand(LongLen)
	if p.Errored() {
		return
	}

	binary.BigEndian.PutUint64(p.Bytes[p.Offset:], val)
	p.Offset += LongLen
}

// UnpackLong unpack a long from the byte array
func (p *Packer) UnpackLong() uint64 {
	p.checkSpace(LongLen)
	if p.Errored() {
		return 0
	}

	val := binary.BigEndian.Uint64(p.Bytes[p.Offset:])
	p.Offset += LongLen
	return val
}

// PackBool packs a bool into the byte array
func (p *Packer) PackBool(b bool) {
	if b {
		p.PackByte(1)
	} else {
		p.PackByte(0)
	}
}

// UnpackBool unpacks a bool from the byte array
func (p *Packer) UnpackBool() bool {
	b := p.UnpackByte()
	switch b {
	case 0:
		return false
	case 1:
		return true
	default:
		p.Add(errBadBool)
		return false
	}
}

// PackFixedBytes append a byte slice, with no length descriptor to the byte
// array
func (p *Packer) PackFixedBytes(bytes []byte) {
	p.expand(len(bytes))
	if p.Errored() {
		return
	}

	copy(p.Bytes[p.Offset:], bytes)
	p.Offset += len(bytes)
}

// UnpackFixedBytes unpack a byte slice, with no length descriptor from the byte
// array
func (p *Packer) UnpackFixedBytes(size int) []byte {
	p.checkSpace(size)
	if p.Errored() {
		return nil
	}

	bytes := p.Bytes[p.Offset : p.Offset+size]
	p.Offset += size
	return bytes
}

// PackBytes append a byte slice to the byte array
func (p *Packer) PackBytes(bytes []byte) {
	p.PackInt(uint32(len(bytes)))
	p.PackFixedBytes(bytes)
}

// UnpackBytes unpack a byte slice from the byte array
func (p *Packer) UnpackBytes() []byte {
	size := p.UnpackInt()
	return p.UnpackFixedBytes(int(size))
}

// UnpackLimitedBytes unpacks a byte slice. If the size of the slice is greater
// than [limit], adds [errOversized] to the packer and returns nil.
func (p *Packer) UnpackLimitedBytes(limit uint32) []byte {
	size := p.UnpackInt()
	if size > limit {
		p.Add(errOversized)
		return nil
	}
	return p.UnpackFixedBytes(int(size))
}

// PackStr append a string to the byte array
func (p *Packer) PackStr(str string) {
	strSize := len(str)
	if strSize > MaxStringLen {
		p.Add(errInvalidInput)
		return
	}
	p.PackShort(uint16(strSize))
	p.PackFixedBytes([]byte(str))
}

// UnpackStr unpacks a string from the byte array
func (p *Packer) UnpackStr() string {
	strSize := p.UnpackShort()
	return string(p.UnpackFixedBytes(int(strSize)))
}

// UnpackLimitedStr unpacks a string. If the size of the string is greater than
// [limit], adds [errOversized] to the packer and returns the empty string.
func (p *Packer) UnpackLimitedStr(limit uint16) string {
	strSize := p.UnpackShort()
	if strSize > limit {
		p.Add(errOversized)
		return ""
	}
	return string(p.UnpackFixedBytes(int(strSize)))
}

// checkSpace requires that there is at least [bytes] of write space left in the
// byte array. If this is not true, an error is added to the packer
func (p *Packer) checkSpace(bytes int) {
	switch {
	case p.Offset < 0:
		p.Add(errNegativeOffset)
	case bytes < 0:
		p.Add(errInvalidInput)
	case len(p.Bytes)-p.Offset < bytes:
		p.Add(ErrInsufficientLength)
	}
}

// expand ensures that there is [bytes] bytes left of space in the byte slice.
// If this is not allowed due to the maximum size, an error is added to the packer
// In order to understand this code, its important to understand the difference
// between a slice's length and its capacity.
func (p *Packer) expand(bytes int) {
	neededSize := bytes + p.Offset // Need byte slice's length to be at least [neededSize]
	switch {
	case neededSize <= len(p.Bytes): // Byte slice has sufficient length already
		return
	case neededSize > p.MaxSize: // Lengthening the byte slice would cause it to grow too large
		p.Err = ErrInsufficientLength
		return
	case neededSize <= cap(p.Bytes): // Byte slice has sufficient capacity to lengthen it without mem alloc
		p.Bytes = p.Bytes[:neededSize]
		return
	default: // Add capacity/length to byte slice
		p.Bytes = append(p.Bytes[:cap(p.Bytes)], make([]byte, neededSize-cap(p.Bytes))...)
	}
}
