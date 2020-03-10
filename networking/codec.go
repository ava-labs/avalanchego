// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package networking

import (
	"errors"
	"math"

	"github.com/ava-labs/salticidae-go"

	"github.com/ava-labs/gecko/utils/wrappers"
)

var (
	errBadLength    = errors.New("stream has unexpected length")
	errMissingField = errors.New("message missing field")
	errBadOp        = errors.New("input field has invalid operation")
)

// Codec defines the serialization and deserialization of network messages
type Codec struct{}

// Pack attempts to pack a map of fields into a message.
func (Codec) Pack(op salticidae.Opcode, fields map[Field]interface{}) (Msg, error) {
	message, ok := Messages[op]
	if !ok {
		return nil, errBadOp
	}

	p := wrappers.Packer{MaxSize: math.MaxInt32}
	for _, field := range message {
		data, ok := fields[field]
		if !ok {
			return nil, errMissingField
		}
		field.Packer()(&p, data)
	}

	if p.Errored() { // Prevent the datastream from leaking
		return nil, p.Err
	}

	return &msg{
		op:     op,
		ds:     salticidae.NewDataStreamFromBytes(p.Bytes, false),
		fields: fields,
	}, nil
}

// Parse attempts to convert a byte stream into a message.
func (Codec) Parse(op salticidae.Opcode, ds salticidae.DataStream) (Msg, error) {
	message, ok := Messages[op]
	if !ok {
		return nil, errBadOp
	}

	// TODO: make this work without copy
	size := ds.Size()
	p := wrappers.Packer{Bytes: make([]byte, size)}

	byteHandle := ds.GetDataInPlace(size)
	defer byteHandle.Release()

	copy(p.Bytes, byteHandle.Get())

	fields := make(map[Field]interface{}, len(message))
	for _, field := range message {
		fields[field] = field.Unpacker()(&p)
	}

	if p.Offset != size {
		return nil, errBadLength
	}

	return &msg{
		op:     op,
		ds:     ds,
		fields: fields,
	}, p.Err
}
