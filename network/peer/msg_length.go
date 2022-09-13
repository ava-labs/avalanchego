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
	errInvalidMaxMessageLength   = errors.New("invalid maximum message length")
	errInvalidMessageLengthBytes = errors.New("invalid message length bytes")
	errMaxMessageLengthExceeded  = errors.New("maximum message length exceeded")
)

const bitmaskCodec = uint32(1 << 31)

// Assumes the specified [msgLen] will never >= 1<<31.
func writeMsgLen(msgLen uint32, isProto bool, maxMsgLen uint32) ([wrappers.IntLen]byte, error) {
	if maxMsgLen >= bitmaskCodec {
		return [wrappers.IntLen]byte{}, fmt.Errorf("%w; maximum message length must be <%d to be able to embed codec information at most significant bit", errInvalidMaxMessageLength, bitmaskCodec)
	}
	if msgLen > maxMsgLen {
		return [wrappers.IntLen]byte{}, fmt.Errorf("%w; the message length %d exceeds the specified limit %d", errMaxMessageLengthExceeded, msgLen, maxMsgLen)
	}

	x := msgLen
	if isProto {
		// mask most significant bit to denote it's using proto
		x |= bitmaskCodec
	}

	b := [wrappers.IntLen]byte{}
	binary.BigEndian.PutUint32(b[:], x)

	return b, nil
}

// Assumes the read [msgLen] will never >= 1<<31.
func readMsgLen(b []byte, maxMsgLen uint32) (uint32, bool, error) {
	if len(b) != wrappers.IntLen {
		return 0, false, fmt.Errorf("%w; readMsgLen only supports 4-byte (got %d bytes)", errInvalidMessageLengthBytes, len(b))
	}

	// parse the message length
	msgLen := binary.BigEndian.Uint32(b)

	// handle proto by reading most significant bit
	isProto := msgLen&bitmaskCodec != 0

	// equivalent to "^= iff isProto=true"
	msgLen &^= bitmaskCodec

	if msgLen > maxMsgLen {
		return 0, false, fmt.Errorf("%w; the message length %d exceeds the specified limit %d", errMaxMessageLengthExceeded, msgLen, maxMsgLen)
	}

	return msgLen, isProto, nil
}
