// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package network

var _ Msg = &msg{}

// Msg represents a set of fields that can be serialized into a byte stream
type Msg interface {
	Op() Op
	Get(Field) interface{}
	Bytes() []byte
}

type msg struct {
	op     Op
	fields map[Field]interface{}
	bytes  []byte
}

// Field returns the value of the specified field in this message
func (msg *msg) Op() Op { return msg.op }

// Field returns the value of the specified field in this message
func (msg *msg) Get(field Field) interface{} { return msg.fields[field] }

// Bytes returns this message in bytes
func (msg *msg) Bytes() []byte { return msg.bytes }
