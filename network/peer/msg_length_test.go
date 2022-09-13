// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package peer

import (
	"errors"
	"math"
	"testing"

	"github.com/ava-labs/avalanchego/utils/constants"
)

func TestMsgLen(t *testing.T) {
	tt := []struct {
		msgLen           uint32
		msgLimit         uint32
		isProto          bool
		expectedWriteErr error
		expectedReadErr  error
	}{
		{
			msgLen:           math.MaxUint32,
			msgLimit:         math.MaxUint32,
			isProto:          false,
			expectedWriteErr: errInvalidMaxMessageLength,
			expectedReadErr:  nil,
		},
		{
			msgLen:           1 << 31,
			msgLimit:         1 << 31,
			isProto:          false,
			expectedWriteErr: errInvalidMaxMessageLength,
			expectedReadErr:  nil,
		},
		{
			msgLen:           constants.DefaultMaxMessageSize,
			msgLimit:         constants.DefaultMaxMessageSize,
			isProto:          false,
			expectedWriteErr: nil,
			expectedReadErr:  nil,
		},
		{
			msgLen:           constants.DefaultMaxMessageSize,
			msgLimit:         constants.DefaultMaxMessageSize,
			isProto:          true,
			expectedWriteErr: nil,
			expectedReadErr:  nil,
		},
		{
			msgLen:           1,
			msgLimit:         constants.DefaultMaxMessageSize,
			isProto:          false,
			expectedWriteErr: nil,
			expectedReadErr:  nil,
		},
		{
			msgLen:           1,
			msgLimit:         constants.DefaultMaxMessageSize,
			isProto:          true,
			expectedWriteErr: nil,
			expectedReadErr:  nil,
		},
	}
	for i, tv := range tt {
		msgLenBytes, werr := writeMsgLen(tv.msgLen, tv.isProto, tv.msgLimit)
		if !errors.Is(werr, tv.expectedWriteErr) {
			t.Fatalf("#%d: unexpected writeMsgLen error %v, expected %v", i, werr, tv.expectedWriteErr)
		}
		if tv.expectedWriteErr != nil {
			continue
		}

		msgLen, isProto, rerr := readMsgLen(msgLenBytes[:], tv.msgLimit)
		if !errors.Is(rerr, tv.expectedReadErr) {
			t.Fatalf("#%d: unexpected readMsgLen error %v, expected %v", i, rerr, tv.expectedReadErr)
		}
		if tv.expectedReadErr != nil {
			continue
		}
		t.Logf("#%d: msgLenBytes for %d (isProto %v): %08b\n", i, tv.msgLen, tv.isProto, msgLenBytes)

		if msgLen != tv.msgLen {
			t.Fatalf("#%d: unexpected msg length %v, expected %v", i, msgLen, tv.msgLen)
		}
		if isProto != tv.isProto {
			t.Fatalf("#%d: unexpected isProto %v, expected %v", i, isProto, tv.isProto)
		}
	}
}
