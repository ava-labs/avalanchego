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
		expectedWriteErr error
		expectedReadErr  error
	}{
		{
			msgLen:           math.MaxUint32,
			msgLimit:         math.MaxUint32,
			expectedWriteErr: errInvalidMaxMessageLength,
			expectedReadErr:  nil,
		},
		{
			msgLen:           1 << 31,
			msgLimit:         1 << 31,
			expectedWriteErr: errInvalidMaxMessageLength,
			expectedReadErr:  nil,
		},
		{
			msgLen:           constants.DefaultMaxMessageSize,
			msgLimit:         constants.DefaultMaxMessageSize,
			expectedWriteErr: nil,
			expectedReadErr:  nil,
		},
		{
			msgLen:           1,
			msgLimit:         constants.DefaultMaxMessageSize,
			expectedWriteErr: nil,
			expectedReadErr:  nil,
		},
	}
	for i, tv := range tt {
		msgLenBytes, werr := writeMsgLen(tv.msgLen, tv.msgLimit)
		if !errors.Is(werr, tv.expectedWriteErr) {
			t.Fatalf("#%d: unexpected writeMsgLen error %v, expected %v", i, werr, tv.expectedWriteErr)
		}
		if tv.expectedWriteErr != nil {
			continue
		}

		msgLen, rerr := readMsgLen(msgLenBytes[:], tv.msgLimit)
		if !errors.Is(rerr, tv.expectedReadErr) {
			t.Fatalf("#%d: unexpected readMsgLen error %v, expected %v", i, rerr, tv.expectedReadErr)
		}
		if tv.expectedReadErr != nil {
			continue
		}
		t.Logf("#%d: msgLenBytes for %d: %08b\n", i, tv.msgLen, msgLenBytes)

		if msgLen != tv.msgLen {
			t.Fatalf("#%d: unexpected msg length %v, expected %v", i, msgLen, tv.msgLen)
		}
	}
}
