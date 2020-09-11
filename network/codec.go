// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

import (
	"errors"
	"fmt"
	"math"

	"github.com/ava-labs/avalanchego/utils/wrappers"
)

var (
	errMissingField = errors.New("message missing field")
	errBadOp        = errors.New("input field has invalid operation")
)

// Codec defines the serialization and deserialization of network messages
type Codec struct{}

// Pack attempts to pack a map of fields into a message.
// The first byte of the message is the opcode of the message.
func (Codec) Pack(op Op, fields map[Field]interface{}) (Msg, error) {
	message, ok := Messages[op]
	if !ok {
		return nil, errBadOp
	}

	p := wrappers.Packer{MaxSize: math.MaxInt32}
	p.PackByte(byte(op))
	for _, field := range message {
		data, ok := fields[field]
		if !ok {
			return nil, errMissingField
		}
		field.Packer()(&p, data)
	}

	return &msg{
		op:     op,
		fields: fields,
		bytes:  p.Bytes,
	}, p.Err
}

// Parse attempts to convert bytes into a message.
// The first byte of the message is the opcode of the message.
func (Codec) Parse(b []byte) (Msg, error) {
	p := wrappers.Packer{Bytes: b}
	op := Op(p.UnpackByte())
	message, ok := Messages[op]
	if !ok {
		return nil, errBadOp
	}

	fields := make(map[Field]interface{}, len(message))
	for _, field := range message {
		fields[field] = field.Unpacker()(&p)
	}

	if p.Offset != len(b) {
		p.Add(fmt.Errorf("expected length %d got %d", len(b), p.Offset))
	}

	return &msg{
		op:     op,
		fields: fields,
		bytes:  b,
	}, p.Err
}
