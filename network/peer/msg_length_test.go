// Copyright (C) 2019, Ava Labs, Inc. All rights reserved.
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
			msgLen:      constants.DefaultMaxMessageSize,
			msgLimit:    1,
			expectedErr: errMaxMessageLengthExceeded,
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
			msgLenBytes:    []byte{0b11111111, 0xFF},
			msgLimit:       math.MaxInt32,
			expectedErr:    errInvalidMessageLength,
			expectedMsgLen: 0,
		},
		{
			msgLenBytes:    []byte{0xFF, 0xFF, 0xFF, 0xFF},
			msgLimit:       constants.DefaultMaxMessageSize,
			expectedErr:    errMaxMessageLengthExceeded,
			expectedMsgLen: 0,
		},
		{
			msgLenBytes:    []byte{0xFF, 0xFF, 0xFF, 0xFF},
			msgLimit:       math.MaxUint32,
			expectedErr:    nil,
			expectedMsgLen: math.MaxUint32,
		},
		{
			msgLenBytes:    []byte{0x00, 0x00, 0x00, 0x01},
			msgLimit:       10,
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

		msgLenAfterWrite, err := readMsgLen(msgLenBytes[:], tv.msgLimit)
		require.NoError(err)
		require.Equal(tv.expectedMsgLen, msgLenAfterWrite)
	}
}
