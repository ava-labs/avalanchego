// Copyright (C) 2019-2025, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package peer

import (
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/utils/wrappers"
)

var (
	errInvalidMessageLength     = errors.New("invalid message length")
	errMaxMessageLengthExceeded = errors.New("maximum message length exceeded")
)

func writeMsgLen(msgLen uint32, maxMsgLen uint32) ([wrappers.IntLen]byte, error) {
	if msgLen > maxMsgLen {
		return [wrappers.IntLen]byte{}, fmt.Errorf(
			"%w; the message length %d exceeds the specified limit %d",
			errMaxMessageLengthExceeded,
			msgLen,
			maxMsgLen,
		)
	}

	b := [wrappers.IntLen]byte{}
	binary.BigEndian.PutUint32(b[:], msgLen)

	return b, nil
}

func readMsgLen(b []byte, maxMsgLen uint32) (uint32, error) {
	if len(b) != wrappers.IntLen {
		return 0, fmt.Errorf(
			"%w; readMsgLen only supports 4 bytes (got %d bytes)",
			errInvalidMessageLength,
			len(b),
		)
	}

	// parse the message length
	msgLen := binary.BigEndian.Uint32(b)
	if msgLen > maxMsgLen {
		return 0, fmt.Errorf(
			"%w; the message length %d exceeds the specified limit %d",
			errMaxMessageLengthExceeded,
			msgLen,
			maxMsgLen,
		)
	}

	return msgLen, nil
}
