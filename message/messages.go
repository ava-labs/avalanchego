// (c) 2019-2020, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import "github.com/ava-labs/avalanchego/ids"

var (
	_ InboundMessage  = &inboundMessage{}
	_ OutboundMessage = &outboundMessage{}
)

// InboundMessage represents a set of fields for an inbound message that can be serialized into a byte stream
type InboundMessage interface {
	BytesSavedCompression() int
	Op() Op
	Get(Field) interface{}
	NodeID() ids.ShortID
	OnFinishedHandling()
}

type inboundMessage struct {
	op                    Op
	bytesSavedCompression int
	fields                map[Field]interface{}
	nodeID                ids.ShortID
	onFinishedHandling    func()
}

// OutboundMessage represents a set of fields for an outbound message that can be serialized into a byte stream
type OutboundMessage interface {
	BytesSavedCompression() int
	Bytes() []byte
	Op() Op

	Clone() OutboundMessage
	ReturnBytes()
}

type outboundMessage struct {
	bytes                 []byte
	bytesSavedCompression int
	op                    Op
	ReturnBytesF          func([]byte)
}

// Op returns the value of the specified operation in this message
func (inMsg *inboundMessage) Op() Op { return inMsg.op }

// BytesSavedCompression returns the number of bytes this message saved due to
// compression. That is, the number of bytes we did not receive over the
// network due to the message being compressed. 0 for messages that were not
// compressed.
func (inMsg *inboundMessage) BytesSavedCompression() int { return inMsg.bytesSavedCompression }

// Field returns the value of the specified field in this message
func (inMsg *inboundMessage) Get(field Field) interface{} { return inMsg.fields[field] }

// NodeID returns the node from which the msg was received
func (inMsg *inboundMessage) NodeID() ids.ShortID { return inMsg.nodeID }

// OnFinishedHandling is the function to be called once inboundMessage is complete
func (inMsg *inboundMessage) OnFinishedHandling() { inMsg.onFinishedHandling() }

// Op returns the value of the specified operation in this message
func (outMsg *outboundMessage) Op() Op { return outMsg.op }

// Bytes returns this message in bytes
func (outMsg *outboundMessage) Bytes() []byte { return outMsg.bytes }

// BytesSavedCompression returns the number of bytes this message saved due to
// compression. That is, the number of bytes we did not send over the
// network due to the message being compressed. 0 for messages that were not
// compressed.
func (outMsg *outboundMessage) BytesSavedCompression() int { return outMsg.bytesSavedCompression }

func (outMsg *outboundMessage) Clone() OutboundMessage {
	copiedBytes := make([]byte, len(outMsg.bytes))
	copy(copiedBytes, outMsg.bytes)

	return &outboundMessage{
		bytes:                 copiedBytes,
		bytesSavedCompression: outMsg.bytesSavedCompression,
		op:                    outMsg.op,
		ReturnBytesF:          outMsg.ReturnBytesF,
	}
}

// release msg bytes to a memory pool for subsequent reuse
// msg cannot be used anymore once ReturnBytes is called
func (outMsg *outboundMessage) ReturnBytes() {
	outMsg.ReturnBytesF(outMsg.bytes)
}
