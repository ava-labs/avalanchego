// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

var _ Message = &message{}

// Message represents a set of fields that can be serialized into a byte stream
type Message interface {
	Op() Op
	Get(Field) interface{}
	Bytes() []byte
	BytesSavedCompression() int
}

type message struct {
	op                    Op
	fields                map[Field]interface{}
	bytes                 []byte
	bytesSavedCompression int
}

// Field returns the value of the specified field in this message
func (msg *message) Op() Op { return msg.op }

// Field returns the value of the specified field in this message
func (msg *message) Get(field Field) interface{} { return msg.fields[field] }

// Bytes returns this message in bytes
func (msg *message) Bytes() []byte { return msg.bytes }

// BytesSavedCompression returns the number of bytes this message saved due to
// compression. That is, the number of bytes we did not send/receive over the
// network due to the message being compressed. 0 for messages that were not
// compressed.
func (msg *message) BytesSavedCompression() int { return msg.bytesSavedCompression }
