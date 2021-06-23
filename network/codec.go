// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"errors"
	"fmt"
	"math"

	"github.com/ava-labs/avalanchego/utils/compression"
	"github.com/ava-labs/avalanchego/utils/wrappers"
)

var (
	errMissingField = errors.New("message missing field")
	errBadOp        = errors.New("input field has invalid operation")
)

// Codec defines the serialization and deserialization of network messages
type Codec struct {
	// compressor must not be nil
	compressor compression.Compressor
}

// Pack attempts to pack a map of fields into a message.
// The first byte of the message is the opcode of the message.
// Uses [buffer] to hold the message's byte repr.
// [buffer]'s contents may be overwritten by this method.
// [buffer] may be nil.
// If [includeIsCompressedFlag], include a flag that marks whether the payload
// is compressed or not.
// If [compress] and [includeIsCompressedFlag], compress the payload.
// TODO remove [includeIsCompressedFlag] after network upgrade.
func (c Codec) Pack(
	buffer []byte,
	op Op,
	fieldValues map[Field]interface{},
	includeIsCompressedFlag bool,
	compress bool,
) (Msg, error) {
	msgFields, ok := Messages[op]
	if !ok {
		return nil, errBadOp
	}

	p := wrappers.Packer{
		MaxSize: math.MaxInt32,
		Bytes:   buffer[:0],
	}
	// Pack the op code (message type)
	p.PackByte(byte(op))

	// If messages of this type may be compressed, pack whether the payload is compressed
	if includeIsCompressedFlag {
		p.PackBool(compress)
	}

	// Pack the payload
	for _, field := range msgFields {
		data, ok := fieldValues[field]
		if !ok {
			return nil, errMissingField
		}
		field.Packer()(&p, data)
	}
	if p.Err != nil {
		return nil, p.Err
	}
	msg := &msg{
		op:     op,
		fields: fieldValues,
		bytes:  p.Bytes,
	}
	if !compress || !includeIsCompressedFlag {
		return msg, nil
	}

	// If [compress], compress the payload (not the op code, not isCompressed).
	// The slice below is guaranteed to be in-bounds because [p.Err] == nil
	// implies that len(msg.bytes) >= 2
	payloadBytes := msg.bytes[wrappers.BoolLen+wrappers.ByteLen:]
	compressedPayloadBytes, err := c.compressor.Compress(payloadBytes)
	if err != nil {
		return nil, fmt.Errorf("couldn't compress payload of %s message: %s", op, err)
	}
	// Remove the payload (keep just the message type and isCompressed)
	msg.bytes = msg.bytes[:wrappers.BoolLen+wrappers.ByteLen]
	// Attach the compressed payload
	msg.bytes = append(msg.bytes, compressedPayloadBytes...)
	return msg, nil
}

// Parse attempts to convert bytes into a message.
// The first byte of the message is the opcode of the message.
func (c Codec) Parse(b []byte, mayBeCompressed bool) (Msg, error) {
	p := wrappers.Packer{Bytes: b}

	// Unpack the op code (message type)
	op := Op(p.UnpackByte())

	msgFields, ok := Messages[op]
	if !ok { // Unknown message type
		return nil, errBadOp
	}

	// See if messages of this type may be compressed
	compressed := false
	if mayBeCompressed {
		compressed = p.UnpackBool()
	}
	if p.Err != nil {
		return nil, p.Err
	}

	// If the payload is compressed, decompress it
	if compressed {
		// The slice below is guaranteed to be in-bounds because [p.Err] == nil
		compressedPayloadBytes := p.Bytes[wrappers.ByteLen+wrappers.BoolLen:]
		payloadBytes, err := c.compressor.Decompress(compressedPayloadBytes)
		if err != nil {
			return nil, fmt.Errorf("couldn't decompress payload of %s message: %s", op, err)
		}
		// Replace the compressed payload with the decompressed payload.
		// Remove the compressed payload (keep just the message type and isCompressed)
		p.Bytes = p.Bytes[:wrappers.ByteLen+wrappers.BoolLen]
		// Attach the decompressed payload.
		p.Bytes = append(p.Bytes, payloadBytes...)
	}

	// Parse each field of the payload
	fieldValues := make(map[Field]interface{}, len(msgFields))
	for _, field := range msgFields {
		fieldValues[field] = field.Unpacker()(&p)
	}

	if p.Offset != len(p.Bytes) {
		return nil, fmt.Errorf("expected length %d got %d", len(p.Bytes), p.Offset)
	}

	return &msg{
		op:     op,
		fields: fieldValues,
		bytes:  b,
	}, p.Err
}
