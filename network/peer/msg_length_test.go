// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package peer

import (
	"math"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ava-labs/avalanchego/utils/constants"
)

func TestWriteMsgLen(t *testing.T) {
	require := require.New(t)

	tt := []struct {
		msgLen      uint32
		msgLimit    uint32
		expectedErr error
	}{
		{
			msgLen:      math.MaxUint32,
			msgLimit:    math.MaxUint32,
			expectedErr: errInvalidMaxMessageLength,
		},
		{
			msgLen:      bitmaskCodec,
			msgLimit:    bitmaskCodec,
			expectedErr: errInvalidMaxMessageLength,
		},
		{
			msgLen:      bitmaskCodec - 1,
			msgLimit:    bitmaskCodec - 1,
			expectedErr: nil,
		},
		{
			msgLen:      constants.DefaultMaxMessageSize,
			msgLimit:    constants.DefaultMaxMessageSize,
			expectedErr: nil,
		},
		{
			msgLen:      1,
			msgLimit:    constants.DefaultMaxMessageSize,
			expectedErr: nil,
		},
		{
			msgLen:      constants.DefaultMaxMessageSize,
			msgLimit:    1,
			expectedErr: errMaxMessageLengthExceeded,
		},
	}
	for _, tv := range tt {
		msgLenBytes, err := writeMsgLen(tv.msgLen, tv.msgLimit)
		require.ErrorIs(err, tv.expectedErr)
		if tv.expectedErr != nil {
			continue
		}

		msgLen, err := readMsgLen(msgLenBytes[:], tv.msgLimit)
		require.NoError(err)
		require.Equal(tv.msgLen, msgLen)
	}
}

func TestReadMsgLen(t *testing.T) {
	require := require.New(t)

	tt := []struct {
		msgLenBytes    []byte
		msgLimit       uint32
		expectedErr    error
		expectedMsgLen uint32
	}{
		{
			msgLenBytes:    []byte{0xFF, 0xFF, 0xFF, 0xFF},
			msgLimit:       math.MaxUint32,
			expectedErr:    errInvalidMaxMessageLength,
			expectedMsgLen: 0,
		},
		{
			msgLenBytes:    []byte{0b11111111, 0xFF},
			msgLimit:       math.MaxInt32,
			expectedErr:    errInvalidMessageLength,
			expectedMsgLen: 0,
		},
		{
			msgLenBytes:    []byte{0b11111111, 0xFF, 0xFF, 0xFF},
			msgLimit:       constants.DefaultMaxMessageSize,
			expectedErr:    errMaxMessageLengthExceeded,
			expectedMsgLen: 0,
		},
		{
			msgLenBytes:    []byte{0b11111111, 0xFF, 0xFF, 0xFF},
			msgLimit:       math.MaxInt32,
			expectedErr:    nil,
			expectedMsgLen: math.MaxInt32,
		},
		{
			msgLenBytes:    []byte{0b10000000, 0x00, 0x00, 0x01},
			msgLimit:       math.MaxInt32,
			expectedErr:    nil,
			expectedMsgLen: 1,
		},
		{
			msgLenBytes:    []byte{0b10000000, 0x00, 0x00, 0x01},
			msgLimit:       1,
			expectedErr:    nil,
			expectedMsgLen: 1,
		},
	}
	for _, tv := range tt {
		msgLen, err := readMsgLen(tv.msgLenBytes, tv.msgLimit)
		require.ErrorIs(err, tv.expectedErr)
		if tv.expectedErr != nil {
			continue
		}
		require.Equal(tv.expectedMsgLen, msgLen)

		msgLenBytes, err := writeMsgLen(msgLen, tv.msgLimit)
		require.NoError(err)
		require.Equal(tv.msgLenBytes, msgLenBytes[:])
	}
}

func TestBackwardsCompatibleReadMsgLen(t *testing.T) {
	require := require.New(t)

	tt := []struct {
		msgLenBytes    []byte
		msgLimit       uint32
		expectedMsgLen uint32
	}{
		{
			msgLenBytes:    []byte{0b01111111, 0xFF, 0xFF, 0xFF},
			msgLimit:       math.MaxInt32,
			expectedMsgLen: math.MaxInt32,
		},
		{
			msgLenBytes:    []byte{0b00000000, 0x00, 0x00, 0x01},
			msgLimit:       math.MaxInt32,
			expectedMsgLen: 1,
		},
		{
			msgLenBytes:    []byte{0b00000000, 0x00, 0x00, 0x01},
			msgLimit:       1,
			expectedMsgLen: 1,
		},
	}
	for _, tv := range tt {
		msgLen, err := readMsgLen(tv.msgLenBytes, tv.msgLimit)
		require.NoError(err)
		require.Equal(tv.expectedMsgLen, msgLen)
	}
}
