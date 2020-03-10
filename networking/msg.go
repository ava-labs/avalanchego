// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package networking

import (
	"github.com/ava-labs/salticidae-go"
)

// Msg represents a set of fields that can be serialized into a byte stream
type Msg interface {
	Op() salticidae.Opcode
	Get(Field) interface{}
	DataStream() salticidae.DataStream
}

type msg struct {
	op     salticidae.Opcode
	ds     salticidae.DataStream
	fields map[Field]interface{}
}

// Field returns the value of the specified field in this message
func (msg *msg) Op() salticidae.Opcode { return msg.op }

// Field returns the value of the specified field in this message
func (msg *msg) Get(field Field) interface{} { return msg.fields[field] }

// Bytes returns this message in bytes
func (msg *msg) DataStream() salticidae.DataStream { return msg.ds }
