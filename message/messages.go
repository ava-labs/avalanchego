// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"google.golang.org/protobuf/proto"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/compression"

	p2ppb "github.com/ava-labs/avalanchego/proto/pb/p2p"
)

var (
	_ InboundMessage  = &inboundMessageWithPacker{}
	_ OutboundMessage = &outboundMessageWithPacker{}
	_ OutboundMessage = &outboundMessageWithProto{}
)

// InboundMessage represents a set of fields for an inbound message that can be serialized into a byte stream
type InboundMessage interface {
	fmt.Stringer

	BytesSavedCompression() int
	Op() Op
	Get(Field) (interface{}, error)
	NodeID() ids.NodeID
	ExpirationTime() time.Time
	OnFinishedHandling()
}

type inboundMessage struct {
	op                    Op
	bytesSavedCompression int
	nodeID                ids.NodeID
	expirationTime        time.Time
	onFinishedHandling    func()
}

// Op returns the value of the specified operation in this message
func (inMsg *inboundMessage) Op() Op { return inMsg.op }

// BytesSavedCompression returns the number of bytes this message saved due to
// compression. That is, the number of bytes we did not receive over the
// network due to the message being compressed. 0 for messages that were not
// compressed.
func (inMsg *inboundMessage) BytesSavedCompression() int {
	return inMsg.bytesSavedCompression
}

// NodeID returns the node that the msg was sent by.
func (inMsg *inboundMessage) NodeID() ids.NodeID { return inMsg.nodeID }

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

type inboundMessageWithPacker struct {
	inboundMessage

	fields map[Field]interface{}
}

// Field returns the value of the specified field in this message
func (inMsg *inboundMessageWithPacker) Get(field Field) (interface{}, error) {
	value, ok := inMsg.fields[field]
	if !ok {
		return nil, fmt.Errorf("%w: %s", errMissingField, field)
	}
	return value, nil
}

func (inMsg *inboundMessageWithPacker) String() string {
	sb := strings.Builder{}
	sb.WriteString(fmt.Sprintf("(Op: %s, NodeID: %s", inMsg.op, inMsg.nodeID))
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
		sb.WriteString(fmt.Sprintf(", len(AppMsg): %d)", len(inMsg.fields[AppBytes].([]byte))))
	default:
		sb.WriteString(")")
	}

	return sb.String()
}

// OutboundMessage represents a set of fields for an outbound message that can
// be serialized into a byte stream
type OutboundMessage interface {
	BytesSavedCompression() int
	Bytes() []byte
	Op() Op
	BypassThrottling() bool

	AddRef()
	DecRef()
}

type outboundMessage struct {
	op                    Op
	bytes                 []byte
	bytesSavedCompression int
	bypassThrottling      bool
}

// Op returns the value of the specified operation in this message
func (outMsg *outboundMessage) Op() Op { return outMsg.op }

// Bytes returns this message in bytes
func (outMsg *outboundMessage) Bytes() []byte { return outMsg.bytes }

// BytesSavedCompression returns the number of bytes this message saved due to
// compression. That is, the number of bytes we did not send over the
// network due to the message being compressed. 0 for messages that were not
// compressed.
func (outMsg *outboundMessage) BytesSavedCompression() int {
	return outMsg.bytesSavedCompression
}

// BypassThrottling when attempting to send this message
func (outMsg *outboundMessage) BypassThrottling() bool { return outMsg.bypassThrottling }

type outboundMessageWithPacker struct {
	outboundMessage

	refLock sync.Mutex
	refs    int
	c       *codec
}

func (outMsg *outboundMessageWithPacker) AddRef() {
	outMsg.refLock.Lock()
	defer outMsg.refLock.Unlock()

	outMsg.refs++
}

// Once the reference count of this message goes to 0, the byte slice should not
// be inspected.
func (outMsg *outboundMessageWithPacker) DecRef() {
	outMsg.refLock.Lock()
	defer outMsg.refLock.Unlock()

	outMsg.refs--
	if outMsg.refs == 0 {
		outMsg.c.byteSlicePool.Put(outMsg.bytes)
	}
}

// TODO: add other compression algorithms with extended interface
type msgBuilderProtobuf struct {
	gzipCompressor compression.Compressor
}

func newMsgBuilderProtobuf(maxCompressSize int64) (*msgBuilderProtobuf, error) {
	cpr, err := compression.NewGzipCompressor(maxCompressSize)
	return &msgBuilderProtobuf{gzipCompressor: cpr}, err
}

// TODO: semantically verify ids.Id fields, etc.
// e.g., ancestors chain Id should be ids.ID format

// NOTE THAT the passed message must be verified beforehand.
// NOTE THAT the passed message will be modified if compression is enabled.
// TODO: find a way to not in-place modify the message
// TODO: implement parsing tests for inbound messages
func (mc *msgBuilderProtobuf) marshal(m *p2ppb.Message, gzipCompress bool) ([]byte, int, error) {
	b, err := proto.Marshal(m)
	if err != nil {
		return nil, 0, err
	}

	before := len(b)
	if gzipCompress {
		// If compression is enabled, we marshal twice:
		// 1. the original message
		// 2. the message with compressed bytes
		//
		// This recursive packing allows us to avoid an extra compression on/off
		// field in the message.
		compressed, err := mc.gzipCompressor.Compress(b)
		if err != nil {
			return nil, 0, err
		}

		// Original message can be discarded for the compressed message.
		m.Message = &p2ppb.Message_CompressedGzip{
			CompressedGzip: compressed,
		}
		b, err = proto.Marshal(m)
		if err != nil {
			return nil, 0, err
		}
	}

	after := len(b)
	bytesSaved := before - after
	return b, bytesSaved, err
}

// NOTE THAT the passed message will be updated if compression is enabled.
// TODO: find a way to not in-place modify the message
func (mc *msgBuilderProtobuf) createOutbound(op Op, msg *p2ppb.Message, gzipCompress bool, bypassThrottling bool) (*outboundMessageWithProto, error) {
	b, saved, err := mc.marshal(msg, gzipCompress)
	if err != nil {
		return nil, err
	}
	return &outboundMessageWithProto{
		outboundMessage: outboundMessage{
			op:                    op,
			bytes:                 b,
			bytesSavedCompression: saved,
			bypassThrottling:      bypassThrottling,
		},
		msg: msg,
	}, nil
}

type outboundMessageWithProto struct {
	outboundMessage

	msg *p2ppb.Message
}

func (outMsg *outboundMessageWithProto) AddRef() {}
func (outMsg *outboundMessageWithProto) DecRef() {}
