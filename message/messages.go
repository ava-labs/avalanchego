// Copyright (C) 2019-2022, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package message

import (
	"fmt"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"google.golang.org/protobuf/proto"

	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/proto/pb/p2p"
	"github.com/ava-labs/avalanchego/utils/compression"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/utils/math"
	"github.com/ava-labs/avalanchego/utils/metric"
	"github.com/ava-labs/avalanchego/utils/timer/mockable"
	"github.com/ava-labs/avalanchego/utils/wrappers"
)

var (
	_ InboundMessage  = (*inboundMessage)(nil)
	_ OutboundMessage = (*outboundMessage)(nil)
)

// InboundMessage represents a set of fields for an inbound message
type InboundMessage interface {
	// NodeID returns the ID of the node that sent this message
	NodeID() ids.NodeID
	// Op returns the op that describes this message type
	Op() Op
	// Message returns the message that was sent
	Message() any
	// Expiration returns the time that the sender will have already timed out
	// this request
	Expiration() time.Time
	// OnFinishedHandling must be called one time when this message has been
	// handled by the message handler
	OnFinishedHandling()
	// BytesSavedCompression returns the number of bytes that this message saved
	// due to being compressed
	BytesSavedCompression() int
}

type inboundMessage struct {
	nodeID                ids.NodeID
	op                    Op
	message               any
	expiration            time.Time
	onFinishedHandling    func()
	bytesSavedCompression int
}

func (m *inboundMessage) NodeID() ids.NodeID {
	return m.nodeID
}

func (m *inboundMessage) Op() Op {
	return m.op
}

func (m *inboundMessage) Message() any {
	return m.message
}

func (m *inboundMessage) Expiration() time.Time {
	return m.expiration
}

func (m *inboundMessage) OnFinishedHandling() {
	if m.onFinishedHandling != nil {
		m.onFinishedHandling()
	}
}

func (m *inboundMessage) BytesSavedCompression() int {
	return m.bytesSavedCompression
}

// OutboundMessage represents a set of fields for an outbound message that can
// be serialized into a byte stream
type OutboundMessage interface {
	// BypassThrottling returns true if we should send this message, regardless
	// of any outbound message throttling
	BypassThrottling() bool
	// Op returns the op that describes this message type
	Op() Op
	// Bytes returns the bytes that will be sent
	Bytes() []byte
	// BytesSavedCompression returns the number of bytes that this message saved
	// due to being compressed
	BytesSavedCompression() int
}

type outboundMessage struct {
	bypassThrottling      bool
	op                    Op
	bytes                 []byte
	bytesSavedCompression int
}

func (m *outboundMessage) BypassThrottling() bool {
	return m.bypassThrottling
}

func (m *outboundMessage) Op() Op {
	return m.op
}

func (m *outboundMessage) Bytes() []byte {
	return m.bytes
}

func (m *outboundMessage) BytesSavedCompression() int {
	return m.bytesSavedCompression
}

// TODO: add other compression algorithms with extended interface
type msgBuilder struct {
	gzipCompressor compression.Compressor

	compressTimeMetrics   map[Op]metric.Averager
	decompressTimeMetrics map[Op]metric.Averager

	maxMessageTimeout time.Duration
}

func newMsgBuilder(
	namespace string,
	metrics prometheus.Registerer,
	maxMessageTimeout time.Duration,
) (*msgBuilder, error) {
	cpr, err := compression.NewGzipCompressor(constants.DefaultMaxMessageSize)
	if err != nil {
		return nil, err
	}

	mb := &msgBuilder{
		gzipCompressor: cpr,

		compressTimeMetrics:   make(map[Op]metric.Averager, len(ExternalOps)),
		decompressTimeMetrics: make(map[Op]metric.Averager, len(ExternalOps)),

		maxMessageTimeout: maxMessageTimeout,
	}

	errs := wrappers.Errs{}
	for _, op := range ExternalOps {
		mb.compressTimeMetrics[op] = metric.NewAveragerWithErrs(
			namespace,
			fmt.Sprintf("%s_compress_time", op),
			fmt.Sprintf("time (in ns) to compress %s messages", op),
			metrics,
			&errs,
		)
		mb.decompressTimeMetrics[op] = metric.NewAveragerWithErrs(
			namespace,
			fmt.Sprintf("%s_decompress_time", op),
			fmt.Sprintf("time (in ns) to decompress %s messages", op),
			metrics,
			&errs,
		)
	}
	return mb, errs.Err
}

func (mb *msgBuilder) marshal(
	uncompressedMsg *p2p.Message,
	gzipCompress bool,
) ([]byte, int, time.Duration, error) {
	uncompressedMsgBytes, err := proto.Marshal(uncompressedMsg)
	if err != nil {
		return nil, 0, 0, err
	}

	if !gzipCompress {
		return uncompressedMsgBytes, 0, 0, nil
	}

	// If compression is enabled, we marshal twice:
	// 1. the original message
	// 2. the message with compressed bytes
	//
	// This recursive packing allows us to avoid an extra compression on/off
	// field in the message.
	startTime := time.Now()
	compressedBytes, err := mb.gzipCompressor.Compress(uncompressedMsgBytes)
	if err != nil {
		return nil, 0, 0, err
	}

	compressedMsg := p2p.Message{
		Message: &p2p.Message_CompressedGzip{
			CompressedGzip: compressedBytes,
		},
	}
	compressedMsgBytes, err := proto.Marshal(&compressedMsg)
	if err != nil {
		return nil, 0, 0, err
	}
	compressTook := time.Since(startTime)

	bytesSaved := len(uncompressedMsgBytes) - len(compressedMsgBytes)
	return compressedMsgBytes, bytesSaved, compressTook, nil
}

func (mb *msgBuilder) unmarshal(b []byte) (*p2p.Message, bool, int, time.Duration, error) {
	m := new(p2p.Message)
	if err := proto.Unmarshal(b, m); err != nil {
		return nil, false, 0, 0, err
	}

	compressed := m.GetCompressedGzip()
	if len(compressed) == 0 {
		// The message wasn't compressed
		return m, false, 0, 0, nil
	}

	startTime := time.Now()
	decompressed, err := mb.gzipCompressor.Decompress(compressed)
	if err != nil {
		return nil, true, 0, 0, err
	}

	if err := proto.Unmarshal(decompressed, m); err != nil {
		return nil, true, 0, 0, err
	}
	decompressTook := time.Since(startTime)

	bytesSavedCompression := len(decompressed) - len(compressed)
	return m, true, bytesSavedCompression, decompressTook, nil
}

func (mb *msgBuilder) createOutbound(m *p2p.Message, gzipCompress bool, bypassThrottling bool) (*outboundMessage, error) {
	b, saved, compressTook, err := mb.marshal(m, gzipCompress)
	if err != nil {
		return nil, err
	}

	op, err := ToOp(m)
	if err != nil {
		return nil, err
	}

	if gzipCompress {
		mb.compressTimeMetrics[op].Observe(float64(compressTook))
	}

	return &outboundMessage{
		bypassThrottling:      bypassThrottling,
		op:                    op,
		bytes:                 b,
		bytesSavedCompression: saved,
	}, nil
}

func (mb *msgBuilder) parseInbound(
	bytes []byte,
	nodeID ids.NodeID,
	onFinishedHandling func(),
) (*inboundMessage, error) {
	m, wasCompressed, bytesSavedCompression, decompressTook, err := mb.unmarshal(bytes)
	if err != nil {
		return nil, err
	}

	op, err := ToOp(m)
	if err != nil {
		return nil, err
	}

	msg, err := Unwrap(m)
	if err != nil {
		return nil, err
	}

	if wasCompressed {
		mb.decompressTimeMetrics[op].Observe(float64(decompressTook))
	}

	expiration := mockable.MaxTime
	if deadline, ok := GetDeadline(msg); ok {
		deadline = math.Min(deadline, mb.maxMessageTimeout)
		expiration = time.Now().Add(deadline)
	}

	return &inboundMessage{
		nodeID:                nodeID,
		op:                    op,
		message:               msg,
		expiration:            expiration,
		onFinishedHandling:    onFinishedHandling,
		bytesSavedCompression: bytesSavedCompression,
	}, nil
}
