// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package peer

import (
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/ava-labs/avalanchego/utils/wrappers"
)

var (
	errInvalidMaxMessageLength  = errors.New("invalid maximum message length")
	errInvalidMessageLength     = errors.New("invalid message length")
	errMaxMessageLengthExceeded = errors.New("maximum message length exceeded")
)

// Used to mask the most significant bit to indicate that the message format
// uses protocol buffers.
const bitmaskCodec = uint32(1 << 31)

// Assumes the specified [msgLen] will never >= 1<<31.
func writeMsgLen(msgLen uint32, maxMsgLen uint32) ([wrappers.IntLen]byte, error) {
	if maxMsgLen >= bitmaskCodec {
		return [wrappers.IntLen]byte{}, fmt.Errorf(
			"%w; maximum message length must be <%d to be able to embed codec information at most significant bit",
			errInvalidMaxMessageLength,
			bitmaskCodec,
		)
	}
	if msgLen > maxMsgLen {
		return [wrappers.IntLen]byte{}, fmt.Errorf("%w; the message length %d exceeds the specified limit %d", errMaxMessageLengthExceeded, msgLen, maxMsgLen)
	}

	x := msgLen

	// Mask the most significant bit to denote it's using proto. This bit isn't
	// read anymore, because all the messages use proto. However, it is set for
	// backwards compatibility.
	// TODO: Once the v1.10 is activated, this mask should be removed.
	x |= bitmaskCodec

	b := [wrappers.IntLen]byte{}
	binary.BigEndian.PutUint32(b[:], x)

	return b, nil
}

// Assumes the read [msgLen] will never >= 1<<31.
func readMsgLen(b []byte, maxMsgLen uint32) (uint32, error) {
	if maxMsgLen >= bitmaskCodec {
		return 0, fmt.Errorf(
			"%w; maximum message length must be <%d to be able to embed codec information at most significant bit",
			errInvalidMaxMessageLength,
			bitmaskCodec,
		)
	}
	if len(b) != wrappers.IntLen {
		return 0, fmt.Errorf(
			"%w; readMsgLen only supports 4 bytes (got %d bytes)",
			errInvalidMessageLength,
			len(b),
		)
	}

	// parse the message length
	msgLen := binary.BigEndian.Uint32(b)

	// Because we always use proto messages, there's no need to check the most
	// significant bit to inspect the message format. So, we just zero the proto
	// flag.
	msgLen &^= bitmaskCodec

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
