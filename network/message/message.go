// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

var _ InboundMessage = &inboundMessage{}
var _ OutboundMessage = &outboundMessage{}

// InboundMessage represents a set of fields for an inbound message that can be serialized into a byte stream
type InboundMessage interface {
	BytesSavedCompression() []byte
	Op() Op
	Bytes() []byte
}

// OutboundMessage represents a set of fields for an outbound message that can be serialized into a byte stream
type OutboundMessage interface {
	BytesSavedCompression() []byte
	Bytes() []byte
}

type inboundMessage struct {
	op                    Op
	bytes                 []byte
	bytesSavedCompression []byte
}

type outboundMessage struct {
	bytes                 []byte
	bytesSavedCompression []byte
}

// Op returns the value of the specified operation in this message
func (inMsg *inboundMessage) Op() Op { return inMsg.op }

// Bytes returns this message in bytes
func (inMsg *inboundMessage) Bytes() []byte { return inMsg.bytes }

// BytesSavedCompression returns the number of bytes this message saved due to
// compression. That is, the number of bytes we did not receive over the
// network due to the message being compressed. Empty slice for messages that were not
// compressed.
func (inMsg *inboundMessage) BytesSavedCompression() []byte { return inMsg.bytesSavedCompression }

// Bytes returns this message in bytes
func (outMsg *outboundMessage) Bytes() []byte { return outMsg.bytes }

// BytesSavedCompression returns the number of bytes this message saved due to
// compression. That is, the number of bytes we did not send over the
// network due to the message being compressed. Empty slice for messages that were not
// compressed.
func (outMsg *outboundMessage) BytesSavedCompression() []byte { return outMsg.bytesSavedCompression }
