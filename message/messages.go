// Copyright (C) 2019-2021, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
)

var (
	_ InboundMessage  = &inboundMessage{}
	_ OutboundMessage = &outboundMessage{}
)

// InboundMessage represents a set of fields for an inbound message that can be serialized into a byte stream
type InboundMessage interface {
	fmt.Stringer

	BytesSavedCompression() int
	Op() Op
	Get(Field) interface{}
	NodeID() ids.ShortID
	ExpirationTime() time.Time
	OnFinishedHandling()
}

type inboundMessage struct {
	op                    Op
	bytesSavedCompression int
	fields                map[Field]interface{}
	nodeID                ids.ShortID
	expirationTime        time.Time
	onFinishedHandling    func()
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

// NodeID returns the node that the msg was sent by.
func (inMsg *inboundMessage) NodeID() ids.ShortID { return inMsg.nodeID }

// ExpirationTime returns the time this message doesn't need to be responded to.
// A zero time means message does not expire.
func (inMsg *inboundMessage) ExpirationTime() time.Time { return inMsg.expirationTime }

// OnFinishedHandling is the function to be called once inboundMessage is
// complete.
func (inMsg *inboundMessage) OnFinishedHandling() {
	if inMsg.onFinishedHandling != nil {
		inMsg.onFinishedHandling()
	}
}

func (inMsg *inboundMessage) String() string {
	sb := strings.Builder{}
	sb.WriteString(fmt.Sprintf("(Op: %s, NodeID: %s%s", inMsg.op, constants.NodeIDPrefix, inMsg.nodeID))
	if requestIDIntf, exists := inMsg.fields[RequestID]; exists {
		sb.WriteString(fmt.Sprintf(", RequestID: %d", requestIDIntf.(uint32)))
	}
	if !inMsg.expirationTime.IsZero() {
		sb.WriteString(fmt.Sprintf(", Deadline: %d", inMsg.expirationTime.Unix()))
	}
	switch inMsg.op {
	case GetAccepted, Accepted, Chits, AcceptedFrontier:
		sb.WriteString(fmt.Sprintf(", NumContainerIDs: %d)", len(inMsg.fields[ContainerIDs].([][]byte))))
	case Get, GetAncestors, Put, PushQuery, PullQuery:
		sb.WriteString(fmt.Sprintf(", ContainerID: 0x%x)", inMsg.fields[ContainerID].([]byte)))
	case Ancestors:
		sb.WriteString(fmt.Sprintf(", NumContainers: %d)", len(inMsg.fields[MultiContainerBytes].([][]byte))))
	case Notify:
		sb.WriteString(fmt.Sprintf(", Notification: %d)", inMsg.fields[VMMessage].(uint32)))
	case AppRequest, AppResponse, AppGossip:
		sb.WriteString(fmt.Sprintf(", len(AppMsg): %d)", inMsg.fields[AppBytes].([]byte)))
	default:
		sb.WriteString(")")
	}

	return sb.String()
}

// OutboundMessage represents a set of fields for an outbound message that can be serialized into a byte stream
type OutboundMessage interface {
	BytesSavedCompression() int
	Bytes() []byte
	Op() Op

	AddRef()
	DecRef()
}

type outboundMessage struct {
	bytes                 []byte
	bytesSavedCompression int
	op                    Op

	refLock sync.Mutex
	refs    int
	c       *codec
}

// Op returns the value of the specified operation in this message
func (outMsg *outboundMessage) Op() Op { return outMsg.op }

// Bytes returns this message in bytes
func (outMsg *outboundMessage) Bytes() []byte { return outMsg.bytes }

// BytesSavedCompression returns the number of bytes this message saved due to
// compression. That is, the number of bytes we did not send over the
// network due to the message being compressed. 0 for messages that were not
// compressed.
func (outMsg *outboundMessage) BytesSavedCompression() int { return outMsg.bytesSavedCompression }

func (outMsg *outboundMessage) AddRef() {
	outMsg.refLock.Lock()
	defer outMsg.refLock.Unlock()

	outMsg.refs++
}

// Once the reference count of this message goes to 0, the byte slice should not
// be inspected.
func (outMsg *outboundMessage) DecRef() {
	outMsg.refLock.Lock()
	defer outMsg.refLock.Unlock()

	outMsg.refs--
	if outMsg.refs == 0 {
		outMsg.c.byteSlicePool.Put(outMsg.bytes)
	}
}
