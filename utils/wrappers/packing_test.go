// Copyright (C) 2019-2026, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package wrappers

import (
	"testing"

	"github.com/stretchr/testify/require"
)

const (
	ByteSentinel  = 0
	ShortSentinel = 0
	IntSentinel   = 0
	LongSentinel  = 0
	BoolSentinel  = false
)

func TestPackerCheckSpace(t *testing.T) {
	require := require.New(t)

	p := Packer{Offset: -1}
	p.checkSpace(1)
	require.True(p.Errored())
	require.ErrorIs(p.Err, errNegativeOffset)

	p = Packer{}
	p.checkSpace(-1)
	require.True(p.Errored())
	require.ErrorIs(p.Err, errInvalidInput)

	p = Packer{Bytes: []byte{0x01}, Offset: 1}
	p.checkSpace(1)
	require.True(p.Errored())
	require.ErrorIs(p.Err, ErrInsufficientLength)

	p = Packer{Bytes: []byte{0x01}, Offset: 2}
	p.checkSpace(0)
	require.True(p.Errored())
	require.ErrorIs(p.Err, ErrInsufficientLength)
}

func TestPackerExpand(t *testing.T) {
	require := require.New(t)

	p := Packer{Bytes: []byte{0x01}, Offset: 2}
	p.expand(1)
	require.True(p.Errored())
	require.ErrorIs(p.Err, ErrInsufficientLength)

	p = Packer{Bytes: []byte{0x01, 0x02, 0x03}, Offset: 0}
	p.expand(1)
	require.False(p.Errored())
	require.NoError(p.Err)
	require.Equal([]byte{0x01, 0x02, 0x03}, p.Bytes)
}

func TestPackerPackByte(t *testing.T) {
	require := require.New(t)

	p := Packer{MaxSize: 1}
	p.PackByte(0x01)
	require.False(p.Errored())
	require.NoError(p.Err)
	require.Equal([]byte{0x01}, p.Bytes)

	p.PackByte(0x02)
	require.True(p.Errored())
	require.ErrorIs(p.Err, ErrInsufficientLength)
}

func TestPackerUnpackByte(t *testing.T) {
	require := require.New(t)

	p := Packer{Bytes: []byte{0x01}, Offset: 0}
	require.Equal(uint8(1), p.UnpackByte())
	require.False(p.Errored())
	require.NoError(p.Err)
	require.Equal(ByteLen, p.Offset)

	require.Equal(uint8(ByteSentinel), p.UnpackByte())
	require.True(p.Errored())
	require.ErrorIs(p.Err, ErrInsufficientLength)
}

func TestPackerPackShort(t *testing.T) {
	require := require.New(t)

	p := Packer{MaxSize: 2}
	p.PackShort(0x0102)
	require.False(p.Errored())
	require.NoError(p.Err)
	require.Equal([]byte{0x01, 0x02}, p.Bytes)
}

func TestPackerUnpackShort(t *testing.T) {
	require := require.New(t)

	p := Packer{Bytes: []byte{0x01, 0x02}, Offset: 0}
	require.Equal(uint16(0x0102), p.UnpackShort())
	require.False(p.Errored())
	require.NoError(p.Err)
	require.Equal(ShortLen, p.Offset)

	require.Equal(uint16(ShortSentinel), p.UnpackShort())
	require.True(p.Errored())
	require.ErrorIs(p.Err, ErrInsufficientLength)
}

func TestPackerPackInt(t *testing.T) {
	require := require.New(t)

	p := Packer{MaxSize: 4}
	p.PackInt(0x01020304)
	require.False(p.Errored())
	require.NoError(p.Err)
	require.Equal([]byte{0x01, 0x02, 0x03, 0x04}, p.Bytes)

	p.PackInt(0x05060708)
	require.True(p.Errored())
	require.ErrorIs(p.Err, ErrInsufficientLength)
}

func TestPackerUnpackInt(t *testing.T) {
	require := require.New(t)

	p := Packer{Bytes: []byte{0x01, 0x02, 0x03, 0x04}, Offset: 0}
	require.Equal(uint32(0x01020304), p.UnpackInt())
	require.False(p.Errored())
	require.NoError(p.Err)
	require.Equal(IntLen, p.Offset)

	require.Equal(uint32(IntSentinel), p.UnpackInt())
	require.True(p.Errored())
	require.ErrorIs(p.Err, ErrInsufficientLength)
}

func TestPackerPackLong(t *testing.T) {
	require := require.New(t)

	p := Packer{MaxSize: 8}
	p.PackLong(0x0102030405060708)
	require.False(p.Errored())
	require.NoError(p.Err)
	require.Equal([]byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08}, p.Bytes)

	p.PackLong(0x090a0b0c0d0e0f00)
	require.True(p.Errored())
	require.ErrorIs(p.Err, ErrInsufficientLength)
}

func TestPackerUnpackLong(t *testing.T) {
	require := require.New(t)

	p := Packer{Bytes: []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08}, Offset: 0}
	require.Equal(uint64(0x0102030405060708), p.UnpackLong())
	require.False(p.Errored())
	require.NoError(p.Err)
	require.Equal(LongLen, p.Offset)

	require.Equal(uint64(LongSentinel), p.UnpackLong())
	require.True(p.Errored())
	require.ErrorIs(p.Err, ErrInsufficientLength)
}

func TestPackerPackFixedBytes(t *testing.T) {
	require := require.New(t)

	p := Packer{MaxSize: 4}
	p.PackFixedBytes([]byte("Avax"))
	require.False(p.Errored())
	require.NoError(p.Err)
	require.Equal([]byte("Avax"), p.Bytes)

	p.PackFixedBytes([]byte("Avax"))
	require.True(p.Errored())
	require.ErrorIs(p.Err, ErrInsufficientLength)
}

func TestPackerUnpackFixedBytes(t *testing.T) {
	require := require.New(t)

	p := Packer{Bytes: []byte("Avax")}
	require.Equal([]byte("Avax"), p.UnpackFixedBytes(4))
	require.False(p.Errored())
	require.NoError(p.Err)
	require.Equal(4, p.Offset)

	require.Nil(p.UnpackFixedBytes(4))
	require.True(p.Errored())
	require.ErrorIs(p.Err, ErrInsufficientLength)
}

func TestPackerPackBytes(t *testing.T) {
	require := require.New(t)

	p := Packer{MaxSize: 8}
	p.PackBytes([]byte("Avax"))
	require.False(p.Errored())
	require.NoError(p.Err)
	require.Equal([]byte("\x00\x00\x00\x04Avax"), p.Bytes)

	p.PackBytes([]byte("Avax"))
	require.True(p.Errored())
	require.ErrorIs(p.Err, ErrInsufficientLength)
}

func TestPackerUnpackBytes(t *testing.T) {
	require := require.New(t)

	p := Packer{Bytes: []byte("\x00\x00\x00\x04Avax")}
	require.Equal([]byte("Avax"), p.UnpackBytes())
	require.False(p.Errored())
	require.NoError(p.Err)
	require.Equal(8, p.Offset)

	require.Nil(p.UnpackBytes())
	require.True(p.Errored())
	require.ErrorIs(p.Err, ErrInsufficientLength)
}

func TestPackerUnpackLimitedBytes(t *testing.T) {
	require := require.New(t)

	p := Packer{Bytes: []byte("\x00\x00\x00\x04Avax")}
	require.Equal([]byte("Avax"), p.UnpackLimitedBytes(10))
	require.False(p.Errored())
	require.NoError(p.Err)
	require.Equal(8, p.Offset)

	require.Nil(p.UnpackLimitedBytes(10))
	require.True(p.Errored())
	require.ErrorIs(p.Err, ErrInsufficientLength)

	// Reset and don't allow enough bytes
	p = Packer{Bytes: p.Bytes}
	require.Nil(p.UnpackLimitedBytes(2))
	require.True(p.Errored())
	require.ErrorIs(p.Err, errOversized)
}

func TestPackerString(t *testing.T) {
	require := require.New(t)

	p := Packer{MaxSize: 6}

	p.PackStr("Avax")
	require.False(p.Errored())
	require.NoError(p.Err)
	require.Equal([]byte{0x00, 0x04, 0x41, 0x76, 0x61, 0x78}, p.Bytes)
}

func TestPackerUnpackString(t *testing.T) {
	require := require.New(t)

	p := Packer{Bytes: []byte("\x00\x04Avax")}

	require.Equal("Avax", p.UnpackStr())
	require.False(p.Errored())
	require.NoError(p.Err)
	require.Equal(6, p.Offset)

	require.Empty(p.UnpackStr())
	require.True(p.Errored())
	require.ErrorIs(p.Err, ErrInsufficientLength)
}

func TestPackerUnpackLimitedString(t *testing.T) {
	require := require.New(t)

	p := Packer{Bytes: []byte("\x00\x04Avax")}
	require.Equal("Avax", p.UnpackLimitedStr(10))
	require.False(p.Errored())
	require.NoError(p.Err)
	require.Equal(6, p.Offset)

	require.Empty(p.UnpackLimitedStr(10))
	require.True(p.Errored())
	require.ErrorIs(p.Err, ErrInsufficientLength)

	// Reset and don't allow enough bytes
	p = Packer{Bytes: p.Bytes}
	require.Empty(p.UnpackLimitedStr(2))
	require.True(p.Errored())
	require.ErrorIs(p.Err, errOversized)
}

func TestPacker(t *testing.T) {
	require := require.New(t)

	p := Packer{MaxSize: 3}

	require.False(p.Errored())
	require.NoError(p.Err)

	p.PackShort(17)
	require.False(p.Errored())
	require.NoError(p.Err)
	require.Equal([]byte{0x0, 0x11}, p.Bytes)

	p.PackShort(1)
	require.True(p.Errored())
	require.ErrorIs(p.Err, ErrInsufficientLength)

	p = Packer{Bytes: p.Bytes}
	require.Equal(uint16(17), p.UnpackShort())
	require.False(p.Errored())
	require.NoError(p.Err)
}

func TestPackBool(t *testing.T) {
	require := require.New(t)

	p := Packer{MaxSize: 3}
	p.PackBool(false)
	p.PackBool(true)
	p.PackBool(false)
	require.False(p.Errored())
	require.NoError(p.Err)

	p = Packer{Bytes: p.Bytes}
	bool1, bool2, bool3 := p.UnpackBool(), p.UnpackBool(), p.UnpackBool()
	require.False(p.Errored())
	require.NoError(p.Err)
	require.False(bool1)
	require.True(bool2)
	require.False(bool3)
}

func TestPackerPackBool(t *testing.T) {
	require := require.New(t)

	p := Packer{MaxSize: 1}

	p.PackBool(true)
	require.False(p.Errored())
	require.NoError(p.Err)
	require.Equal([]byte{0x01}, p.Bytes)

	p.PackBool(false)
	require.True(p.Errored())
	require.ErrorIs(p.Err, ErrInsufficientLength)
}

func TestPackerUnpackBool(t *testing.T) {
	require := require.New(t)

	p := Packer{Bytes: []byte{0x01}, Offset: 0}

	require.True(p.UnpackBool())
	require.False(p.Errored())
	require.NoError(p.Err)
	require.Equal(BoolLen, p.Offset)

	require.Equal(BoolSentinel, p.UnpackBool())
	require.True(p.Errored())
	require.ErrorIs(p.Err, ErrInsufficientLength)

	p = Packer{Bytes: []byte{0x42}, Offset: 0}
	require.False(p.UnpackBool())
	require.True(p.Errored())
	require.ErrorIs(p.Err, errBadBool)
}
